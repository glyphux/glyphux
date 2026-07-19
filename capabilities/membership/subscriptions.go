package membership

import (
	"context"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// billingPeriod maps a membership_tier's billing_interval to a fixed
// duration approximation: 30 days for monthly, 365 days for yearly. A
// documented simplification (real calendar-month arithmetic — e.g. "the
// 15th of next month" — would need a time.AddDate(0, 1, 0)-style call keyed
// to a wall-clock date rather than a pure duration); see this slice's
// tracking doc. Any other interval string defaults to 30 days.
func billingPeriod(interval string) time.Duration {
	if interval == BillingIntervalYearly {
		return 365 * 24 * time.Hour
	}
	return 30 * 24 * time.Hour
}

// startPeriod returns the current_period_start/current_period_end fields for
// a subscription period beginning at now and running for t's
// billing_interval (billingPeriod) — the shared "begin a new billing
// period" seam both Subscribe (a brand-new subscription's first period) and
// ProcessRenewal's success branch (billing.go; a renewed subscription's next
// period) need, mirroring expireSubscription's identical role as the shared
// seam on the lapse/cancel side.
func startPeriod(t *tier, now time.Time) map[string]any {
	return map[string]any{
		"current_period_start": now.Format(time.RFC3339Nano),
		"current_period_end":   now.Add(billingPeriod(t.BillingInterval)).Format(time.RFC3339Nano),
	}
}

// SubscriptionEvent is the payload emitted on both "membership.started" and
// "membership.expired" — one shared type rather than a separate pair, since
// neither event needs a field the other doesn't (both are "this user, this
// tier" facts); only the event NAME differs. capabilities/commerce (slice
// 3.3) and capabilities/notifications (slice 3.1) established this exact
// precedent for their own paired events, for the identical reason. This
// package defines its OWN expected/emitted shape rather than importing
// capabilities/notifications's MembershipEvent{Email, PlanName} type: PRD
// §8.4's domain events carry no canonical cross-plugin payload shape yet
// (every existing emitter — commerce's OrderPlacedEvent/PaymentEvent
// included — defines its own), so a real membership-emits/notifications-
// receives wiring today would still need a payload-shape adapter in
// between; documented as a known gap in this slice's tracking doc, not
// silently glossed over.
type SubscriptionEvent struct {
	SubscriptionID string
	UserID         string
	TierID         string
}

// Subscribe creates an active subscription linking userID to tierID,
// starting now and running for tierID's billing_interval (billingPeriod),
// entirely through host.Content() — never a direct store/DB call. Emits
// "membership.started" (gated on events:emit only, per Plugin.Manifest's
// doc comment). Returns the new subscription's ID.
func Subscribe(ctx context.Context, host sdk.HostAPI, userID, tierID string) (string, error) {
	t, err := getTier(ctx, host, tierID)
	if err != nil {
		return "", fmt.Errorf("membership: subscribe: look up tier %q: %w", tierID, err)
	}
	data := map[string]any{
		"user_id": userID,
		"tier_id": tierID,
		"status":  StatusActive,
	}
	for k, v := range startPeriod(t, time.Now().UTC()) {
		data[k] = v
	}
	item, err := host.Content().Create(ctx, SubscriptionContentType, data)
	if err != nil {
		return "", err
	}
	if err := host.Emit(ctx, "membership.started", SubscriptionEvent{
		SubscriptionID: item.ID,
		UserID:         userID,
		TierID:         tierID,
	}); err != nil {
		return "", fmt.Errorf("membership: emit membership.started: %w", err)
	}
	return item.ID, nil
}

// CancelSubscription marks subscriptionID StatusCancelled and emits
// "membership.expired" — cancellation is one of the two lapse paths that
// event describes (the other being ProcessRenewal's charge-failure path,
// billing.go), per this ticket's spec ("membership.expired (on lapse or
// cancellation)").
func CancelSubscription(ctx context.Context, host sdk.HostAPI, subscriptionID string) error {
	return expireSubscription(ctx, host, subscriptionID, StatusCancelled)
}

// expireSubscription is the shared "patch subscription status, then emit
// membership.expired" sequence both CancelSubscription and ProcessRenewal's
// charge-failure branch need.
func expireSubscription(ctx context.Context, host sdk.HostAPI, subscriptionID, status string) error {
	item, err := patchSubscription(ctx, host, subscriptionID, map[string]any{"status": status})
	if err != nil {
		return fmt.Errorf("membership: mark subscription %s: %w", status, err)
	}
	sub, err := parseSubscription(item)
	if err != nil {
		return fmt.Errorf("membership: mark subscription %s: %w", status, err)
	}
	if err := host.Emit(ctx, "membership.expired", SubscriptionEvent{
		SubscriptionID: subscriptionID,
		UserID:         sub.UserID,
		TierID:         sub.TierID,
	}); err != nil {
		return fmt.Errorf("membership: emit membership.expired: %w", err)
	}
	return nil
}

// patchSubscription fetches subscriptionID's existing content data, merges
// patch onto it, and writes the result back via host.Content().Update,
// returning the merged item. A helper because content.API.Update validates
// the ENTIRE data map it's given against the subscription content type's
// required fields, not just the keys being changed — mirrors
// capabilities/commerce's patchOrder helper for the identical reason (see
// its doc comment).
func patchSubscription(ctx context.Context, host sdk.HostAPI, subscriptionID string, patch map[string]any) (*content.Item, error) {
	existing, err := host.Content().Get(ctx, SubscriptionContentType, subscriptionID)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]any, len(existing.Data)+len(patch))
	for k, v := range existing.Data {
		merged[k] = v
	}
	for k, v := range patch {
		merged[k] = v
	}
	return host.Content().Update(ctx, SubscriptionContentType, subscriptionID, merged)
}

// subscription is this capability's own parsed view of a
// membership_subscription content item's Data map — internal, never exposed
// as this package's public shape (callers get back plain IDs from
// Subscribe, exactly like CreateTier), mirroring tiers.go's identical
// tier/parseTier pattern.
type subscription struct {
	UserID             string
	TierID             string
	Status             string
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
}

// parseSubscription parses item's Data map into this package's own
// subscription shape, following the exact same convention parseTier
// (tiers.go) established for membership_tier items. current_period_start/
// current_period_end are parsed as time.RFC3339Nano (the format Subscribe
// and ProcessRenewal's success branch both write via startPeriod) — an
// unparseable date is reported as an error rather than silently zero-valued,
// since every subscription this package itself ever writes has one.
func parseSubscription(item *content.Item) (*subscription, error) {
	userID, _ := item.Data["user_id"].(string)
	tierID, _ := item.Data["tier_id"].(string)
	status, _ := item.Data["status"].(string)
	startStr, _ := item.Data["current_period_start"].(string)
	endStr, _ := item.Data["current_period_end"].(string)
	start, err := time.Parse(time.RFC3339Nano, startStr)
	if err != nil {
		return nil, fmt.Errorf("membership: subscription %q has invalid current_period_start: %w", item.ID, err)
	}
	end, err := time.Parse(time.RFC3339Nano, endStr)
	if err != nil {
		return nil, fmt.Errorf("membership: subscription %q has invalid current_period_end: %w", item.ID, err)
	}
	return &subscription{
		UserID:             userID,
		TierID:             tierID,
		Status:             status,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   end,
	}, nil
}
