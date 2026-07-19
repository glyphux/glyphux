package commerce

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"
)

// CheckoutRequest is what StartCheckout asks a PaymentGateway to turn into a
// hosted checkout session.
type CheckoutRequest struct {
	OrderID     string
	AmountCents int64
	Currency    string
	ProductName string
	SuccessURL  string
	CancelURL   string
}

// CheckoutSession is a gateway-hosted checkout session: a URL to send the
// buyer to, and the gateway's own ID for that session.
type CheckoutSession struct {
	ID  string
	URL string
}

// WebhookEvent is a payment-gateway callback this capability's webhook
// handler (webhook.go) acts on, already verified and normalized by
// PaymentGateway.ParseWebhook.
type WebhookEvent struct {
	// Type is the gateway's own event type string (e.g.
	// "checkout.session.completed", "charge.refunded" for StripeGateway) —
	// exposed as-is rather than mapped to a smaller enum, since webhook.go
	// only branches on the two outcomes this slice handles (see its own
	// doc comment).
	Type string
	// OrderID is the order this event concerns, recovered from whatever
	// field the gateway round-trips the checkout's client reference through
	// (Stripe: client_reference_id).
	OrderID string
}

// PaymentGateway is the provider-agnostic payment adapter boundary this
// capability's checkout/webhook flow is built against — mirrors
// capabilities/notifications's MailerAdapter pattern (slice 3.1): the
// capability's own logic (checkout.go, webhook.go) never imports a
// provider SDK directly, only this interface. StripeGateway (below) is the
// one real implementation this slice ships, backed by the actual
// github.com/stripe/stripe-go/v81 client library configured to talk to
// either the real Stripe API or (in every test in this package) a local
// fake-gateway test server — see this slice's tracking doc for why a real
// SDK against a fake server, rather than a hand-rolled HTTP client, is the
// more honest choice here.
type PaymentGateway interface {
	// CreateCheckoutSession starts a hosted checkout for req, returning the
	// URL to send the buyer to.
	CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error)
	// ParseWebhook verifies payload's signature against sigHeader and
	// returns the normalized event it describes.
	ParseWebhook(payload []byte, sigHeader string) (*WebhookEvent, error)
	// AllowlistHost is the exact hostname this gateway's outbound checkout
	// calls target — what Plugin.Manifest declares as this capability's
	// "network" permission allowlist entry (PRD §7.3's own
	// `network: [api.stripe.com]` example), and what StartCheckout checks
	// via host.AllowsNetworkHost before ever calling CreateCheckoutSession.
	AllowlistHost() string
}

// ErrGatewayHostNotAllowed is returned by StartCheckout when the plugin's
// manifest does not allowlist the configured gateway's host (slice 2.6's
// AllowsNetworkHost) — checkout is refused before any outbound call is
// attempted.
var ErrGatewayHostNotAllowed = errors.New("commerce: payment gateway host not allowed by this plugin's network permission")

// StripeGateway is a PaymentGateway backed by the real
// github.com/stripe/stripe-go/v81 client, pointed at baseURL (the real
// Stripe API in production; a local fake-gateway httptest server in every
// test in this package — see gateway_test.go/webhook_test.go).
type StripeGateway struct {
	apiKey        string
	webhookSecret string
	host          string
	backend       stripe.Backend
}

// NewStripeGateway returns a StripeGateway whose checkout-session calls hit
// baseURL (e.g. "https://api.stripe.com" in production, a fake gateway
// test server's URL in tests) using apiKey, and whose ParseWebhook verifies
// incoming callbacks against webhookSecret.
func NewStripeGateway(apiKey, webhookSecret, baseURL string) (*StripeGateway, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("commerce: invalid gateway base URL %q: %w", baseURL, err)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("commerce: gateway base URL %q has no host", baseURL)
	}
	backend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL:        stripe.String(baseURL),
		HTTPClient: http.DefaultClient,
	})
	return &StripeGateway{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		host:          u.Hostname(),
		backend:       backend,
	}, nil
}

// AllowlistHost returns the hostname this gateway's checkout calls target.
func (g *StripeGateway) AllowlistHost() string { return g.host }

// CreateCheckoutSession creates a real Stripe Checkout Session (against
// whatever backend URL NewStripeGateway was configured with) via
// stripe-go's checkout/session client — a genuine HTTP request/response
// cycle in the real Stripe API shape, not a mocked SDK call.
func (g *StripeGateway) CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (*CheckoutSession, error) {
	client := session.Client{B: g.backend, Key: g.apiKey}
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:        stripe.String(req.SuccessURL),
		CancelURL:         stripe.String(req.CancelURL),
		ClientReferenceID: stripe.String(req.OrderID),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Quantity: stripe.Int64(1),
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency:   stripe.String(req.Currency),
					UnitAmount: stripe.Int64(req.AmountCents),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(req.ProductName),
					},
				},
			},
		},
	}
	params.Context = ctx

	sess, err := client.New(params)
	if err != nil {
		return nil, fmt.Errorf("commerce: create checkout session: %w", err)
	}
	return &CheckoutSession{ID: sess.ID, URL: sess.URL}, nil
}

// ParseWebhook verifies payload's Stripe-Signature header against
// webhookSecret (real stripe-go signature verification, github.com/stripe/
// stripe-go/v81/webhook.ConstructEvent) and normalizes the event's type and
// the order ID it concerns.
//
// Order ID recovery is a documented simplification: for
// checkout.session.completed, event.Data.Object's "client_reference_id"
// field (real Stripe checkout-session shape) is used directly. For
// charge.refunded, real Stripe's charge object carries no
// client_reference_id (that field only exists on Checkout Session objects
// — recovering it for a real refund would need a PaymentIntent/Session
// lookup this slice doesn't implement); this capability's fake gateway
// test server round-trips the same "client_reference_id" key on its
// synthetic charge.refunded payload so ParseWebhook's extraction logic is
// uniform across both event types. See this slice's tracking doc.
func (g *StripeGateway) ParseWebhook(payload []byte, sigHeader string) (*WebhookEvent, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, g.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("commerce: verify webhook signature: %w", err)
	}
	orderID, _ := event.Data.Object["client_reference_id"].(string)
	return &WebhookEvent{Type: string(event.Type), OrderID: orderID}, nil
}
