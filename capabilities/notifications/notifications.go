// Package notifications is the PRD §14 slice 3.1 first-party capability:
// dispatching notifications over domain events through a provider-agnostic
// mailer adapter (PRD §7.2: "notifications -> events, mailer"). Runtime
// tier and other judgment calls are documented in this slice's tracking doc
// (docs/implementation/active/0021-phase3-slice3.1-notifications.md), not
// here.
package notifications

import (
	"context"
	"fmt"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// Plugin is the notifications capability's sdk.Plugin implementation.
type Plugin struct {
	adapter MailerAdapter
}

// New returns a notifications Plugin dispatching every notification through
// adapter.
func New(adapter MailerAdapter) *Plugin {
	return &Plugin{adapter: adapter}
}

// Manifest declares this capability's identity and the api scopes it needs
// to subscribe to the domain events it dispatches notifications for (PRD
// §8.4's sensitiveEvents gating, pkg/sdk/host.go):
//   - user.created needs users:read
//   - payment.completed needs payments (any scope suffices per
//     sensitiveEvents' empty-Scope rule)
//   - membership.started / membership.expired need membership (any scope)
//
// No content/media capability is declared — this capability touches no
// content or media surface, only the event bus and the mailer adapter.
func (p *Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    "notifications",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"subscribe"}},
			{Capability: "users", Scopes: []string{"read"}},
			{Capability: "payments", Scopes: []string{"charge"}},
			{Capability: "membership", Scopes: []string{"manage"}},
		},
	}
}

// Register subscribes to every domain event this capability dispatches a
// notification for, entirely through host.On — a denial here (e.g. a host
// built from a manifest that dropped one of the required capabilities)
// propagates unmodified, proving Register carries no bypass of pkg/sdk's
// own gate.
func (p *Plugin) Register(host sdk.HostAPI) error {
	if err := host.On("user.created", p.handleUserCreated); err != nil {
		return err
	}
	if err := host.On("payment.completed", p.handlePaymentCompleted); err != nil {
		return err
	}
	if err := host.On("membership.started", p.handleMembershipStarted); err != nil {
		return err
	}
	if err := host.On("membership.expired", p.handleMembershipExpired); err != nil {
		return err
	}
	return nil
}

// UserCreatedEvent is the payload this capability expects on "user.created".
// The PRD doesn't yet define a canonical cross-plugin payload shape for
// domain events (payments/membership don't back a real HostAPI method
// surface at all yet — see pkg/sdk/manifest.go's knownAPIScopes doc
// comment), so this capability defines its own expected shape, documented
// here as the contract an emitter must satisfy for a notification to fire.
type UserCreatedEvent struct {
	Email string
	Name  string
}

// PaymentCompletedEvent is the payload this capability expects on
// "payment.completed".
type PaymentCompletedEvent struct {
	Email   string
	OrderID string
	Amount  string
}

// MembershipEvent is the payload this capability expects on both
// "membership.started" and "membership.expired" — the two events share one
// type rather than each getting its own, because neither event needs any
// field the other doesn't (both are "this email, this plan" facts); only
// the template rendered off of it differs (see renderMembershipStarted vs.
// renderMembershipExpired in templates.go). Revisit this if a later slice's
// event needs a field the other doesn't (e.g. an expiry date), at which
// point splitting back into two types is the natural move.
type MembershipEvent struct {
	Email    string
	PlanName string
}

// dispatch type-asserts payload to T, formatting a descriptive error naming
// event if the concrete type doesn't match, then renders and sends through
// adapter. The single seam every handler below funnels through, so the
// assert-format-render-send shape exists exactly once regardless of how
// many events this capability grows to support.
func dispatch[T any](ctx context.Context, adapter MailerAdapter, event string, payload any, render func(T) (to, subject, body string)) error {
	e, ok := payload.(T)
	if !ok {
		return fmt.Errorf("notifications: %s payload has unexpected type %T, want %T", event, payload, *new(T))
	}
	to, subject, body := render(e)
	return adapter.Send(ctx, to, subject, body)
}

func (p *Plugin) handleUserCreated(ctx context.Context, payload any) error {
	return dispatch(ctx, p.adapter, "user.created", payload, renderWelcome)
}

func (p *Plugin) handlePaymentCompleted(ctx context.Context, payload any) error {
	return dispatch(ctx, p.adapter, "payment.completed", payload, renderPaymentReceipt)
}

func (p *Plugin) handleMembershipStarted(ctx context.Context, payload any) error {
	return dispatch(ctx, p.adapter, "membership.started", payload, renderMembershipStarted)
}

func (p *Plugin) handleMembershipExpired(ctx context.Context, payload any) error {
	return dispatch(ctx, p.adapter, "membership.expired", payload, renderMembershipExpired)
}
