package membership

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/paymentintent"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// ChargeRequest is what ProcessRenewal asks a RecurringGateway to attempt
// for one subscription's recurring charge cycle.
type ChargeRequest struct {
	SubscriptionID string
	AmountCents    int64
	Currency       string
}

// ChargeResult is a gateway's outcome for one recurring-charge attempt.
// Succeeded distinguishes "the gateway processed the request and the charge
// was accepted" from "the charge was declined" — both are a nil error from
// RecurringGateway.ChargeSubscription (the request itself succeeded as an
// HTTP/API call); only a genuine transport/API failure returns a non-nil
// error.
type ChargeResult struct {
	ID        string
	Succeeded bool
}

// RecurringGateway is the provider-agnostic recurring-billing adapter
// boundary this capability's renewal logic (ProcessRenewal) is built
// against — mirrors capabilities/commerce's PaymentGateway pattern (slice
// 3.3) and capabilities/notifications's MailerAdapter pattern (slice 3.1):
// this capability's own logic never imports a provider SDK directly, only
// this interface. StripeRecurringGateway (below) is the one real
// implementation this slice ships.
//
// This is a documented SIMPLIFICATION of real recurring billing, not a
// reimplementation of Stripe's actual Subscriptions/Billing API surface
// (real Stripe Subscriptions manage their own invoice/proration/retry
// lifecycle server-side). ChargeSubscription models one isolated "attempt a
// charge for this renewal" call via a Stripe PaymentIntent — proving "a
// subscription period elapses, a recurring charge attempt is made against
// the gateway, success extends the period / failure marks it expired" as a
// first vertical slice. A real Stripe Billing integration (subscriptions,
// invoices, dunning/retry schedules, proration) is a natural, larger
// follow-up; see this slice's tracking doc.
type RecurringGateway interface {
	// ChargeSubscription attempts one recurring charge for req.
	ChargeSubscription(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
	// AllowlistHost is the exact hostname this gateway's outbound calls
	// target — what Plugin.Manifest declares as this capability's "network"
	// permission allowlist entry, and what ProcessRenewal checks via
	// host.AllowsNetworkHost before ever calling ChargeSubscription.
	AllowlistHost() string
}

// ErrGatewayHostNotAllowed is returned by ProcessRenewal when the plugin's
// manifest does not allowlist the configured gateway's host (slice 2.6's
// AllowsNetworkHost) — mirrors capabilities/commerce's identical gate for
// its own gateway calls.
var ErrGatewayHostNotAllowed = errors.New("membership: recurring-billing gateway host not allowed by this plugin's network permission")

// StripeRecurringGateway is a RecurringGateway backed by the real
// github.com/stripe/stripe-go/v81 client's paymentintent package, pointed at
// baseURL (the real Stripe API in production; a local fake-gateway
// httptest server in every test in this package — see fakegateway_test.go),
// mirroring capabilities/commerce.StripeGateway's identical "real SDK
// against a fake server" convention (see that type's doc comment for the
// full rationale).
type StripeRecurringGateway struct {
	apiKey  string
	host    string
	backend stripe.Backend
}

// NewStripeRecurringGateway returns a StripeRecurringGateway whose
// ChargeSubscription calls hit baseURL using apiKey.
func NewStripeRecurringGateway(apiKey, baseURL string) (*StripeRecurringGateway, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("membership: invalid gateway base URL %q: %w", baseURL, err)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("membership: gateway base URL %q has no host", baseURL)
	}
	backend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL:        stripe.String(baseURL),
		HTTPClient: http.DefaultClient,
	})
	return &StripeRecurringGateway{apiKey: apiKey, host: u.Hostname(), backend: backend}, nil
}

// AllowlistHost returns the hostname this gateway's charge calls target.
func (g *StripeRecurringGateway) AllowlistHost() string { return g.host }

// ChargeSubscription creates a real Stripe PaymentIntent (against whatever
// backend URL NewStripeRecurringGateway was configured with) via stripe-go's
// paymentintent client, confirmed immediately (off-session, as a real
// recurring/merchant-initiated charge against a previously-saved payment
// method would be) — a genuine HTTP request/response cycle in the real
// Stripe API shape. req.SubscriptionID travels as PaymentIntent metadata so
// a fake gateway test server can look up its configured per-subscription
// outcome (see fakegateway_test.go); a real gateway would ignore or log this
// metadata like any other.
func (g *StripeRecurringGateway) ChargeSubscription(ctx context.Context, req ChargeRequest) (*ChargeResult, error) {
	client := paymentintent.Client{B: g.backend, Key: g.apiKey}
	params := &stripe.PaymentIntentParams{
		Amount:        stripe.Int64(req.AmountCents),
		Currency:      stripe.String(req.Currency),
		Confirm:       stripe.Bool(true),
		PaymentMethod: stripe.String("pm_card_visa"),
	}
	params.AddMetadata("subscription_id", req.SubscriptionID)
	params.Context = ctx

	pi, err := client.New(params)
	if err != nil {
		return nil, fmt.Errorf("membership: charge subscription: %w", err)
	}
	return &ChargeResult{ID: pi.ID, Succeeded: pi.Status == stripe.PaymentIntentStatusSucceeded}, nil
}

// ProcessRenewal attempts subscriptionID's recurring charge cycle: refuses
// outright (ErrGatewayHostNotAllowed) if this plugin's manifest does not
// allowlist gateway.AllowlistHost() via its declared "network" permission —
// network access from this capability always goes through that check before
// any outbound request is attempted, mirroring
// capabilities/commerce.StartCheckout's identical gate. On a successful
// charge, extends the subscription's current_period_end by its tier's
// billing_interval and leaves status StatusActive (no event emitted — a
// renewal is not a new "start" and the subscription never lapsed). On a
// declined/failed charge, marks the subscription StatusExpired and emits
// "membership.expired" via expireSubscription.
func ProcessRenewal(ctx context.Context, host sdk.HostAPI, gateway RecurringGateway, subscriptionID string) error {
	if !host.AllowsNetworkHost(gateway.AllowlistHost()) {
		return fmt.Errorf("%w: %q", ErrGatewayHostNotAllowed, gateway.AllowlistHost())
	}
	item, err := host.Content().Get(ctx, SubscriptionContentType, subscriptionID)
	if err != nil {
		return err
	}
	sub, err := parseSubscription(item)
	if err != nil {
		return fmt.Errorf("membership: process renewal: %w", err)
	}
	t, err := getTier(ctx, host, sub.TierID)
	if err != nil {
		return fmt.Errorf("membership: process renewal: look up tier %q: %w", sub.TierID, err)
	}
	result, err := gateway.ChargeSubscription(ctx, ChargeRequest{
		SubscriptionID: subscriptionID,
		AmountCents:    t.PriceCents,
		Currency:       t.Currency,
	})
	if err != nil {
		return fmt.Errorf("membership: process renewal: %w", err)
	}
	if !result.Succeeded {
		return expireSubscription(ctx, host, subscriptionID, StatusExpired)
	}
	if _, err := patchSubscription(ctx, host, subscriptionID, startPeriod(t, time.Now().UTC())); err != nil {
		return fmt.Errorf("membership: extend subscription period: %w", err)
	}
	return nil
}
