package membership_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"

	stripe "github.com/stripe/stripe-go/v81"
)

// fakeRecurringGateway is a local HTTP test server replicating a real
// payment gateway's PaymentIntent-creation API shape closely enough that the
// REAL github.com/stripe/stripe-go/v81 client
// (capabilities/membership.StripeRecurringGateway) can be pointed at it and
// exercise a genuine HTTP request/response cycle — adapted from
// capabilities/commerce/fakegateway_test.go's fakeGateway (same firm,
// user-confirmed decision: a local fake gateway, never a live network
// endpoint or real credentials). Duplicated into this package rather than
// imported from capabilities/commerce (a _test.go file, and a different
// capability's test-only internals) per this ticket's own instruction to
// duplicate/adapt rather than import test-only code across a capability
// boundary.
//
// It implements exactly one thing a real gateway does: POST
// /v1/payment_intents — parses the real application/x-www-form-urlencoded
// body stripe-go's own Backend.Call sends, and responds with a JSON
// stripe.PaymentIntent matching the real API's response shape (id, status,
// amount, currency, ...). The outcome (succeeded vs. declined) for a given
// call is looked up by the "metadata[subscription_id]" form field
// StripeRecurringGateway.ChargeSubscription sets — configured per-test via
// SetOutcome, defaulting to succeeded if never configured.
type fakeRecurringGateway struct {
	srv *httptest.Server

	mu       sync.Mutex
	outcomes map[string]bool
	nextID   int
}

// newFakeRecurringGateway starts the fake gateway server.
func newFakeRecurringGateway() *fakeRecurringGateway {
	g := &fakeRecurringGateway{outcomes: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/payment_intents", g.handleCreatePaymentIntent)
	g.srv = httptest.NewServer(mux)
	return g
}

// URL is this fake gateway's base URL — what StripeRecurringGateway's
// baseURL is pointed at in every test in this package.
func (g *fakeRecurringGateway) URL() string { return g.srv.URL }

// Close shuts down the underlying httptest server.
func (g *fakeRecurringGateway) Close() { g.srv.Close() }

// SetOutcome configures whether a future ChargeSubscription call naming
// subscriptionID (via its PaymentIntent metadata) succeeds or is declined.
func (g *fakeRecurringGateway) SetOutcome(subscriptionID string, succeed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.outcomes[subscriptionID] = succeed
}

func (g *fakeRecurringGateway) handleCreatePaymentIntent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	subscriptionID := r.Form.Get("metadata[subscription_id]")

	g.mu.Lock()
	succeed, configured := g.outcomes[subscriptionID]
	if !configured {
		succeed = true
	}
	g.nextID++
	id := fmt.Sprintf("pi_test_%d", g.nextID)
	g.mu.Unlock()

	status := stripe.PaymentIntentStatusSucceeded
	if !succeed {
		status = stripe.PaymentIntentStatusRequiresPaymentMethod
	}

	amount, _ := strconv.ParseInt(r.Form.Get("amount"), 10, 64)
	resp := map[string]any{
		"id":       id,
		"object":   "payment_intent",
		"amount":   amount,
		"currency": r.Form.Get("currency"),
		"status":   string(status),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
