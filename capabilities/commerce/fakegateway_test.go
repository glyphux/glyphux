package commerce_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
)

// fakeGateway is a local HTTP test server replicating a real payment
// gateway's checkout-session-creation + webhook-callback API shape closely
// enough that the REAL github.com/stripe/stripe-go/v81 client
// (capabilities/commerce.StripeGateway) can be pointed at it and exercise a
// genuine HTTP request/response cycle — this ticket's firm, user-confirmed
// decision (see docs/implementation/active/0023-phase3-slice3.3-commerce.md)
// to build a local fake gateway rather than reach any real network
// endpoint or use real credentials.
//
// It implements exactly two things a real gateway does:
//  1. POST /v1/checkout/sessions — parses the real
//     application/x-www-form-urlencoded body stripe-go's own Backend.Call
//     sends, and responds with a JSON stripe.CheckoutSession matching the
//     real API's response shape (id, url, client_reference_id, ...).
//  2. Outbound webhook delivery — CompleteSession/RefundSession, called by
//     a test after the fact to simulate the gateway's own asynchronous
//     notification, POST a real, correctly HMAC-signed
//     (webhook.GenerateTestSignedPayload) event payload to whatever
//     merchant webhook URL the test configured, exactly like Stripe's own
//     webhook delivery.
type fakeGateway struct {
	srv           *httptest.Server
	webhookSecret string

	mu         sync.Mutex
	sessions   map[string]fakeSession
	webhookURL string
	nextID     int
}

type fakeSession struct {
	ClientReferenceID string
}

// newFakeGateway starts the fake gateway server. webhookSecret is the
// signing secret both the gateway (when it delivers a webhook) and the
// merchant's StripeGateway (when it verifies one) share — mirrors the real
// Stripe dashboard-issued signing secret both sides of a real integration
// are configured with.
func newFakeGateway(webhookSecret string) *fakeGateway {
	g := &fakeGateway{webhookSecret: webhookSecret, sessions: map[string]fakeSession{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/checkout/sessions", g.handleCreateSession)
	g.srv = httptest.NewServer(mux)
	return g
}

// URL is this fake gateway's base URL — what StripeGateway's baseURL is
// pointed at in every test in this package.
func (g *fakeGateway) URL() string { return g.srv.URL }

// Close shuts down the underlying httptest server.
func (g *fakeGateway) Close() { g.srv.Close() }

// SetWebhookURL configures where CompleteSession/RefundSession deliver their
// simulated webhook callback — a test's own merchant webhook receiver
// httptest server, mirroring the URL a real Stripe dashboard webhook
// endpoint would be configured with.
func (g *fakeGateway) SetWebhookURL(url string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.webhookURL = url
}

func (g *fakeGateway) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientRef := r.Form.Get("client_reference_id")

	g.mu.Lock()
	g.nextID++
	id := fmt.Sprintf("cs_test_%d", g.nextID)
	g.sessions[id] = fakeSession{ClientReferenceID: clientRef}
	g.mu.Unlock()

	resp := map[string]any{
		"id":                  id,
		"object":              "checkout.session",
		"url":                 g.srv.URL + "/checkout/" + id,
		"client_reference_id": clientRef,
		"payment_status":      "unpaid",
		"status":              "open",
		"mode":                "payment",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// CompleteSession simulates the gateway observing a successful payment for
// sessionID: it POSTs a real, signed checkout.session.completed webhook
// event to the configured webhook URL (via SetWebhookURL), returning the
// merchant endpoint's response status so the test can assert delivery
// succeeded.
func (g *fakeGateway) CompleteSession(sessionID string) (int, error) {
	return g.deliverWebhook(sessionID, string(stripe.EventTypeCheckoutSessionCompleted))
}

// RefundSession simulates the gateway processing a refund for sessionID: it
// POSTs a real, signed charge.refunded webhook event to the configured
// webhook URL. Real Stripe charge objects carry no client_reference_id
// (that's a Checkout Session field); this fake gateway round-trips the same
// key on its synthetic charge.refunded payload anyway, a documented
// simplification StripeGateway.ParseWebhook's doc comment also explains,
// since recovering the order id in a real integration would need a
// PaymentIntent/Session lookup out of scope for this slice.
func (g *fakeGateway) RefundSession(sessionID string) (int, error) {
	return g.deliverWebhook(sessionID, string(stripe.EventTypeChargeRefunded))
}

func (g *fakeGateway) deliverWebhook(sessionID, eventType string) (int, error) {
	g.mu.Lock()
	sess, ok := g.sessions[sessionID]
	webhookURL := g.webhookURL
	g.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("fakeGateway: unknown session %q", sessionID)
	}
	if webhookURL == "" {
		return 0, fmt.Errorf("fakeGateway: no webhook URL configured (call SetWebhookURL first)")
	}

	object := map[string]any{
		"id":                  sessionID,
		"client_reference_id": sess.ClientReferenceID,
	}
	objectJSON, err := json.Marshal(object)
	if err != nil {
		return 0, err
	}
	eventBody := map[string]any{
		"id":          "evt_test_" + sessionID,
		"object":      "event",
		"api_version": stripe.APIVersion,
		"created":     time.Now().Unix(),
		"type":        eventType,
		"data":        map[string]any{"object": json.RawMessage(objectJSON)},
	}
	payload, err := json.Marshal(eventBody)
	if err != nil {
		return 0, err
	}

	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: payload,
		Secret:  g.webhookSecret,
	})

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(signed.Payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Stripe-Signature", signed.Header)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
