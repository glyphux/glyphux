// Package membership is the PRD §14 slice 3.4 first-party capability:
// membership-tier/subscription content types, content-gating enforcement
// (does a given user hold an active subscription at/above a required tier),
// and recurring billing against a payment gateway (PRD §7.2:
// "membership -> identity, permissions, content, notifications"). It follows
// the same convention capabilities/forms (slice 2.9) established: import
// only pkg/sdk, pkg/contract, and internal/content solely for the
// *content.Item type sdk.ContentAPI's own method signatures already return
// to a Tier-A (in-process) plugin — every actual data access flows through
// the HostAPI passed into Register, never a kernel-internal shortcut.
//
// This capability does NOT import internal/identity or internal/permission
// directly. "userID" throughout this package is an opaque string the caller
// supplies (in a real deployment, the caller's own stringified identity user
// ID) — membership never looks a user up itself; it only matches subscription
// records already carrying that same string in their "user_id" field. This
// keeps the content-gating logic (gating.go) independently testable against
// real subscription records without depending on a real identity.Service, and
// mirrors the existing separation between HostAPI's own per-request-role
// permission checks (internal/permission, gating WHICH CAPABILITIES a plugin
// PACKAGE was granted) and this ticket's new axis (gating WHICH CONTENT an
// END USER may read based on THEIR subscription state) — the two are
// deliberately not conflated; see this slice's tracking doc.
package membership

import (
	"context"

	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Content type names this capability registers via RegisterContentType,
// following capabilities/forms's/capabilities/commerce's established
// pattern: one shared content type per structural concept, not a
// dynamically-registered type per tier/subscriber.
const (
	TierContentType         = "membership_tier"
	SubscriptionContentType = "membership_subscription"
)

// Subscription status values stored in the membership_subscription content
// type's "status" field. StatusActive is set at Subscribe time;
// StatusExpired/StatusCancelled are the only transitions ProcessRenewal
// (billing.go) and CancelSubscription (subscriptions.go) ever make.
const (
	StatusActive    = "active"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
)

// Billing interval values stored in the membership_tier content type's
// "billing_interval" field. billingPeriod (subscriptions.go) maps each to a
// fixed-duration approximation — a documented simplification, not real
// calendar-month arithmetic; see this slice's tracking doc.
const (
	BillingIntervalMonthly = "monthly"
	BillingIntervalYearly  = "yearly"
)

// Plugin is the membership capability's sdk.Plugin implementation.
type Plugin struct {
	Gateway RecurringGateway
}

// New returns a membership Plugin whose recurring-charge attempts
// (ProcessRenewal) go through gateway. gateway may be nil for a caller that
// only exercises content-gating (HasActiveMembership) and never calls
// ProcessRenewal — mirrors capabilities/commerce.New's own nil-gateway
// allowance.
func New(gateway RecurringGateway) *Plugin {
	return &Plugin{Gateway: gateway}
}

// Manifest declares this capability's identity and its consent-relevant
// axes: content[read,write] (to register/manage tier & subscription content),
// events[emit] (membership.started/membership.expired), payments[charge]
// (the recurring-charge attempt ProcessRenewal makes against Gateway,
// mirroring capabilities/commerce's identical payments:charge declaration
// for its own directly-called gateway), and network (the recurring-billing
// gateway's allowlisted host, when Gateway is configured — see
// NetworkPermission's precedent in capabilities/commerce.Plugin.Manifest).
//
// No "membership" api scope is declared: pkg/sdk/host.go's sensitiveEvents
// only gates SUBSCRIBING (On) to membership.started/membership.expired on
// that capability, never emitting (Emit) them — see Emit's own doc comment
// ("deliberately not gated per-event"). This capability only ever emits
// those two events, never subscribes, so declaring "membership" would grant
// nothing this capability uses.
func (p *Plugin) Manifest() sdk.Manifest {
	perms := []sdk.Permission(nil)
	if p.Gateway != nil {
		if host := p.Gateway.AllowlistHost(); host != "" {
			perms = append(perms, sdk.Permission{Name: "network", Args: []string{host}})
		}
	}
	return sdk.Manifest{
		Name:    "membership",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read", "write"}},
			{Capability: "events", Scopes: []string{"emit"}},
			{Capability: "payments", Scopes: []string{"charge"}},
		},
		Permissions: perms,
	}
}

// Register defines the membership_tier and membership_subscription content
// types, entirely through host — a denial here (e.g. a host built from a
// manifest that dropped content:write) propagates unmodified, proving
// Register carries no bypass of pkg/sdk's own gate.
func (p *Plugin) Register(host sdk.HostAPI) error {
	if err := host.RegisterContentType(context.Background(), TierContentType, contract.ContentType{
		Fields: map[string]contract.Field{
			"name":             {Type: contract.FieldString, Required: true},
			"price_cents":      {Type: contract.FieldNumber, Required: true},
			"currency":         {Type: contract.FieldString, Required: true},
			"billing_interval": {Type: contract.FieldString, Required: true},
			"rank":             {Type: contract.FieldNumber, Required: true},
		},
	}); err != nil {
		return err
	}
	return host.RegisterContentType(context.Background(), SubscriptionContentType, contract.ContentType{
		Fields: map[string]contract.Field{
			"user_id":              {Type: contract.FieldString, Required: true},
			"tier_id":              {Type: contract.FieldString, Required: true},
			"status":               {Type: contract.FieldString, Required: true},
			"current_period_start": {Type: contract.FieldDate, Required: true},
			"current_period_end":   {Type: contract.FieldDate, Required: true},
		},
	})
}
