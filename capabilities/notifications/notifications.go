// Package notifications is the PRD §14 slice 3.1 first-party capability:
// email / SMS / push / in-app dispatch over domain events plus a provider-
// agnostic mailer adapter (PRD §7.2: "notifications -> events, mailer").
//
// Runtime-tier judgment call: this slice ships an in-process sdk.Plugin
// (Tier A), the same convention capabilities/forms established in slice
// 2.9, rather than extending pkg/runtime/rpc's Tier C broker (slice 2.5).
// The PRD's own sketch (§14) names Tier C for this capability specifically
// to "validate the RPC tier"; that remains a legitimate follow-up once a
// real external mailer provider (with its own SDK/dependencies a Tier C
// out-of-process plugin would isolate) is wired in. But this slice ships no
// live provider integration at all (no credentials exist in this
// environment) — only the MailerAdapter boundary and a stub in-memory
// adapter. Standing up gRPC broker/subprocess plumbing to host a plugin
// that makes zero real network calls would exercise no behavior an
// in-process implementation doesn't already prove, and it would import
// pkg/runtime/rpc machinery this ticket doesn't otherwise need. See this
// slice's tracking doc (docs/implementation/active/0021) for the full
// reasoning; a later slice that wires a real provider (Resend/Vonage/FCM)
// is the natural point to revisit Tier C.
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

// MembershipStartedEvent is the payload this capability expects on
// "membership.started".
type MembershipStartedEvent struct {
	Email    string
	PlanName string
}

// MembershipExpiredEvent is the payload this capability expects on
// "membership.expired".
type MembershipExpiredEvent struct {
	Email    string
	PlanName string
}

func (p *Plugin) handleUserCreated(ctx context.Context, payload any) error {
	e, ok := payload.(UserCreatedEvent)
	if !ok {
		return fmt.Errorf("notifications: user.created payload has unexpected type %T, want %T", payload, UserCreatedEvent{})
	}
	subject, body := renderWelcome(e)
	return p.adapter.Send(ctx, e.Email, subject, body)
}

func (p *Plugin) handlePaymentCompleted(ctx context.Context, payload any) error {
	e, ok := payload.(PaymentCompletedEvent)
	if !ok {
		return fmt.Errorf("notifications: payment.completed payload has unexpected type %T, want %T", payload, PaymentCompletedEvent{})
	}
	subject, body := renderPaymentReceipt(e)
	return p.adapter.Send(ctx, e.Email, subject, body)
}

func (p *Plugin) handleMembershipStarted(ctx context.Context, payload any) error {
	e, ok := payload.(MembershipStartedEvent)
	if !ok {
		return fmt.Errorf("notifications: membership.started payload has unexpected type %T, want %T", payload, MembershipStartedEvent{})
	}
	subject, body := renderMembershipStarted(e)
	return p.adapter.Send(ctx, e.Email, subject, body)
}

func (p *Plugin) handleMembershipExpired(ctx context.Context, payload any) error {
	e, ok := payload.(MembershipExpiredEvent)
	if !ok {
		return fmt.Errorf("notifications: membership.expired payload has unexpected type %T, want %T", payload, MembershipExpiredEvent{})
	}
	subject, body := renderMembershipExpired(e)
	return p.adapter.Send(ctx, e.Email, subject, body)
}
