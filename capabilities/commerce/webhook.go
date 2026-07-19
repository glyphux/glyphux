package commerce

import (
	"context"
	"fmt"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// PaymentEvent is the payload emitted on both "payment.completed" and
// "payment.refunded" — one shared type rather than a separate
// PaymentCompletedEvent/PaymentRefundedEvent pair, since neither outcome
// needs a field the other doesn't (both are simply "this order" facts;
// only the event NAME and the order's resulting status differ, not the
// payload shape). capabilities/notifications (slice 3.1) established this
// exact precedent for membership.started/membership.expired
// (MembershipEvent) for the identical reason; revisit (split back into two
// types) if a later need arises for a field one event carries that the
// other doesn't (e.g. a refund reason/amount payment.completed has no
// counterpart for).
type PaymentEvent struct {
	OrderID string
}

// HandleWebhook verifies and processes one payment-gateway callback:
// gateway.ParseWebhook authenticates payload against sigHeader, and the
// resulting event's Type selects the only two outcomes this slice handles —
//   - "checkout.session.completed": marks the referenced order
//     OrderStatusPaid and emits "payment.completed".
//   - "charge.refunded": marks the referenced order OrderStatusRefunded and
//     emits "payment.refunded".
//
// Any other event type is accepted (no error) but produces no order
// mutation or emission — an unrecognized-but-authentic callback should not
// itself be treated as an error, since a real gateway may deliver event
// types this slice has no handling for yet.
func HandleWebhook(ctx context.Context, host sdk.HostAPI, gateway PaymentGateway, payload []byte, sigHeader string) error {
	event, err := gateway.ParseWebhook(payload, sigHeader)
	if err != nil {
		return err
	}
	if event.OrderID == "" {
		return fmt.Errorf("commerce: webhook event %q carries no recoverable order id", event.Type)
	}
	switch event.Type {
	case "checkout.session.completed":
		return transitionOrder(ctx, host, event.OrderID, OrderStatusPaid, "payment.completed")
	case "charge.refunded":
		return transitionOrder(ctx, host, event.OrderID, OrderStatusRefunded, "payment.refunded")
	}
	return nil
}

// transitionOrder is the shared "patch order status, then emit the
// corresponding PaymentEvent" sequence both HandleWebhook branches need —
// the only difference between marking an order paid vs. refunded is the
// target status and the event name, so both funnel through this one seam
// rather than each branch repeating patch-then-emit-then-wrap-error.
func transitionOrder(ctx context.Context, host sdk.HostAPI, orderID, status, eventName string) error {
	if err := patchOrder(ctx, host, orderID, map[string]any{"status": status}); err != nil {
		return fmt.Errorf("commerce: mark order %s: %w", status, err)
	}
	if err := host.Emit(ctx, eventName, PaymentEvent{OrderID: orderID}); err != nil {
		return fmt.Errorf("commerce: emit %s: %w", eventName, err)
	}
	return nil
}
