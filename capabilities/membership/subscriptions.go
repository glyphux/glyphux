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
	now := time.Now().UTC()
	item, err := host.Content().Create(ctx, SubscriptionContentType, map[string]any{
		"user_id":              userID,
		"tier_id":              tierID,
		"status":               StatusActive,
		"current_period_start": now.Format(time.RFC3339Nano),
		"current_period_end":   now.Add(billingPeriod(t.BillingInterval)).Format(time.RFC3339Nano),
	})
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
	sub, err := patchSubscription(ctx, host, subscriptionID, map[string]any{"status": status})
	if err != nil {
		return fmt.Errorf("membership: mark subscription %s: %w", status, err)
	}
	userID, _ := sub.Data["user_id"].(string)
	tierID, _ := sub.Data["tier_id"].(string)
	if err := host.Emit(ctx, "membership.expired", SubscriptionEvent{
		SubscriptionID: subscriptionID,
		UserID:         userID,
		TierID:         tierID,
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
