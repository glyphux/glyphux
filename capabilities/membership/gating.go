package membership

import (
	"context"
	"time"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// HasActiveMembership reports whether userID currently holds an active
// subscription at a tier ranked at or above requiredTierID's own rank — the
// content-gating enforcement this ticket adds (distinct from and additional
// to internal/permission's existing role-based capability gating; see this
// package's doc comment). This is a decision PRIMITIVE, not an enforcement
// POINT: nothing in this package intercepts an actual content-serving HTTP
// request to call this automatically — wiring it into a real request path
// (internal/api or a theme's rendering) is a separate frontend/transport
// concern this ticket does not build, exactly as capabilities/commerce
// documented RegisterAdminPage's own frontend-integration boundary. Callers
// (a future request-gating middleware, or a test proving the logic itself)
// invoke this directly against real subscription records.
//
// A subscription counts as active for this check only if BOTH:
//   - its "status" field is StatusActive (not StatusExpired/StatusCancelled
//     — a status ProcessRenewal or CancelSubscription may have set), AND
//   - its "current_period_end" has not yet passed (real elapsed-time check,
//     not merely trusting the stored status — a subscription whose period
//     lapsed before any renewal job got around to calling ProcessRenewal is
//     not truly active, even if its status field still says so).
//
// Tier ranking is requiredTierID's own "rank" field (tiers.go): userID's
// subscribed tier must have a rank >= requiredTierID's rank. If userID holds
// multiple subscriptions, the highest-ranked ACTIVE one determines the
// result — one lapsed higher tier alongside one active lower tier still
// gates correctly against the active tier's own rank, never the lapsed one.
func HasActiveMembership(ctx context.Context, host sdk.HostAPI, userID, requiredTierID string) (bool, error) {
	required, err := getTier(ctx, host, requiredTierID)
	if err != nil {
		return false, err
	}
	items, err := host.Content().List(ctx, SubscriptionContentType)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	for _, item := range items {
		uid, _ := item.Data["user_id"].(string)
		if uid != userID {
			continue
		}
		status, _ := item.Data["status"].(string)
		if status != StatusActive {
			continue
		}
		periodEndStr, _ := item.Data["current_period_end"].(string)
		periodEnd, err := time.Parse(time.RFC3339Nano, periodEndStr)
		if err != nil || now.After(periodEnd) {
			continue
		}
		tierID, _ := item.Data["tier_id"].(string)
		t, err := getTier(ctx, host, tierID)
		if err != nil {
			return false, err
		}
		if t.Rank >= required.Rank {
			return true, nil
		}
	}
	return false, nil
}
