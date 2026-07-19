package commerce

import (
	"context"
	"fmt"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// CreateProduct records a product entirely through host.Content() — never a
// direct store/DB call. priceCents is the product's price in the smallest
// currency unit (e.g. cents), matching gateway APIs' own convention.
// Returns the new item's ID.
func CreateProduct(ctx context.Context, host sdk.HostAPI, name string, priceCents int64, currency, description string) (string, error) {
	item, err := host.Content().Create(ctx, ProductContentType, map[string]any{
		"name":        name,
		"price_cents": float64(priceCents),
		"currency":    currency,
		"description": description,
	})
	if err != nil {
		return "", err
	}
	return item.ID, nil
}

// CreateOrder records an order against productID in OrderStatusPending and
// emits order.placed (gated on the payments capability per pkg/sdk/host.go's
// sensitiveEvents — this capability's own manifest already declares it).
// Returns the new order's ID.
func CreateOrder(ctx context.Context, host sdk.HostAPI, productID string, quantity int64, amountCents int64, currency string) (string, error) {
	item, err := host.Content().Create(ctx, OrderContentType, map[string]any{
		"product_id":   productID,
		"quantity":     float64(quantity),
		"amount_cents": float64(amountCents),
		"currency":     currency,
		"status":       OrderStatusPending,
	})
	if err != nil {
		return "", err
	}
	if err := host.Emit(ctx, "order.placed", OrderPlacedEvent{
		OrderID:     item.ID,
		ProductID:   productID,
		AmountCents: amountCents,
		Currency:    currency,
	}); err != nil {
		return "", fmt.Errorf("commerce: emit order.placed: %w", err)
	}
	return item.ID, nil
}

// OrderPlacedEvent is the payload emitted on "order.placed" when
// CreateOrder succeeds.
type OrderPlacedEvent struct {
	OrderID     string
	ProductID   string
	AmountCents int64
	Currency    string
}

// StartCheckout initiates a gateway-hosted checkout session for req against
// gateway, refusing the call outright (ErrGatewayHostNotAllowed) if this
// plugin's manifest does not allowlist gateway.AllowlistHost() via its
// declared "network" permission (slice 2.6's host.AllowsNetworkHost) —
// network access from this capability always goes through that check
// before any outbound request is attempted. On success, stores the
// gateway's session ID on the order (host.Content().Update) so the webhook
// handler can later recover which order a callback concerns.
func StartCheckout(ctx context.Context, host sdk.HostAPI, gateway PaymentGateway, req CheckoutRequest) (*CheckoutSession, error) {
	if !host.AllowsNetworkHost(gateway.AllowlistHost()) {
		return nil, fmt.Errorf("%w: %q", ErrGatewayHostNotAllowed, gateway.AllowlistHost())
	}
	sess, err := gateway.CreateCheckoutSession(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := patchOrder(ctx, host, req.OrderID, map[string]any{"checkout_session_id": sess.ID}); err != nil {
		return nil, fmt.Errorf("commerce: record checkout session on order: %w", err)
	}
	return sess, nil
}

// patchOrder merges patch onto orderID's existing content data and writes
// the result back via host.Content().Update. A helper because
// content.API.Update (which host.Content().Update delegates to) validates
// the ENTIRE data map it's given against the order content type's required
// fields, not just the keys being changed — so a naive
// Update(ctx, OrderContentType, orderID, patch) would fail validation for
// every required field patch doesn't happen to touch. Every write this
// capability makes to an already-created order (checkout.go, webhook.go)
// goes through this one seam rather than each caller reimplementing the
// fetch-merge-write sequence.
func patchOrder(ctx context.Context, host sdk.HostAPI, orderID string, patch map[string]any) error {
	existing, err := host.Content().Get(ctx, OrderContentType, orderID)
	if err != nil {
		return err
	}
	merged := make(map[string]any, len(existing.Data)+len(patch))
	for k, v := range existing.Data {
		merged[k] = v
	}
	for k, v := range patch {
		merged[k] = v
	}
	_, err = host.Content().Update(ctx, OrderContentType, orderID, merged)
	return err
}
