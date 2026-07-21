package membership

import (
	"fmt"

	"context"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// tier is this capability's own parsed view of a membership_tier content
// item's Data map — internal, never exposed as this package's public shape
// (callers get back plain IDs from CreateTier, exactly like
// capabilities/commerce.CreateProduct).
type tier struct {
	Name            string
	PriceCents      int64
	Currency        string
	BillingInterval string
	Rank            int64
}

// CreateTier records a membership tier entirely through host.Content() —
// never a direct store/DB call. rank orders tiers for HasActiveMembership's
// "at or above" comparison (gating.go): a higher rank is a higher tier.
// Returns the new item's ID.
func CreateTier(ctx context.Context, host sdk.HostAPI, name string, priceCents int64, currency, billingInterval string, rank int64) (string, error) {
	item, err := host.Content().Create(ctx, TierContentType, map[string]any{
		"name":             name,
		"price_cents":      float64(priceCents),
		"currency":         currency,
		"billing_interval": billingInterval,
		"rank":             float64(rank),
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

// getTier fetches tierID through host.Content() and parses its Data map into
// this package's own tier shape.
func getTier(ctx context.Context, host sdk.HostAPI, tierID string) (*tier, error) {
	item, err := host.Content().Get(ctx, TierContentType, tierID)
	if err != nil {
		return nil, err
	}
	return parseTier(item)
}

func parseTier(item *content.Item) (*tier, error) {
	name, _ := item.Data["name"].(string)
	currency, _ := item.Data["currency"].(string)
	interval, _ := item.Data["billing_interval"].(string)
	priceCents, ok := numberField(item.Data, "price_cents")
	if !ok {
		return nil, fmt.Errorf("membership: tier %q has no numeric price_cents", item.ID)
	}
	rank, ok := numberField(item.Data, "rank")
	if !ok {
		return nil, fmt.Errorf("membership: tier %q has no numeric rank", item.ID)
	}
	return &tier{
		Name:            name,
		PriceCents:      int64(priceCents),
		Currency:        currency,
		BillingInterval: interval,
		Rank:            int64(rank),
	}, nil
}

// numberField extracts a float64-backed field (every content.Item.Data
// number field round-trips as float64 — see capabilities/commerce's own
// float64(...) writes) from data.
func numberField(data map[string]any, key string) (float64, bool) {
	v, ok := data[key].(float64)
	return v, ok
}
