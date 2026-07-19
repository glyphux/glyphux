package commerce

import (
	"context"
	"fmt"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// PaymentCompletedEvent is the payload emitted on "payment.completed" when
// HandleWebhook processes a completed-checkout callback.
type PaymentCompletedEvent struct {
	OrderID string
}

// PaymentRefundedEvent is the payload emitted on "payment.refunded" when
// HandleWebhook processes a refund/failure callback.
type PaymentRefundedEvent struct {
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
		if err := patchOrder(ctx, host, event.OrderID, map[string]any{"status": OrderStatusPaid}); err != nil {
			return fmt.Errorf("commerce: mark order paid: %w", err)
		}
		if err := host.Emit(ctx, "payment.completed", PaymentCompletedEvent{OrderID: event.OrderID}); err != nil {
			return fmt.Errorf("commerce: emit payment.completed: %w", err)
		}
	case "charge.refunded":
		if err := patchOrder(ctx, host, event.OrderID, map[string]any{"status": OrderStatusRefunded}); err != nil {
			return fmt.Errorf("commerce: mark order refunded: %w", err)
		}
		if err := host.Emit(ctx, "payment.refunded", PaymentRefundedEvent{OrderID: event.OrderID}); err != nil {
			return fmt.Errorf("commerce: emit payment.refunded: %w", err)
		}
	}
	return nil
}
