// Package commerce is the PRD §14 slice 3.3 first-party capability:
// product/order content types, a checkout flow against a payment gateway,
// and order.placed/payment.completed/payment.refunded event emission (PRD
// §8.4, gated by pkg/sdk/host.go's sensitiveEvents on the exact same event
// names slice 2.2 already defined). Runtime tier and other judgment calls
// are documented in this slice's tracking doc
// (docs/implementation/active/0023-phase3-slice3.3-commerce.md), not here.
package commerce

import (
	"context"

	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Content type names this capability registers via RegisterContentType,
// following capabilities/forms's established pattern (slice 2.9).
const (
	ProductContentType = "product"
	OrderContentType   = "order"
)

// Order status values stored in the order content type's "status" field.
// pending is the state set at order creation; paid/refunded/failed are the
// only transitions the webhook handler (webhook.go) ever makes.
const (
	OrderStatusPending  = "pending"
	OrderStatusPaid     = "paid"
	OrderStatusRefunded = "refunded"
	OrderStatusFailed   = "failed"
)

// OrdersAdminPageSlug is the admin page this capability registers for order
// management via host.RegisterAdminPage. Registering the AdminPageDef is
// this Go package's entire admin-UI responsibility — the actual admin-ui
// React page that renders order data is a separate frontend concern this
// ticket does not build (see this slice's tracking doc's Current Decisions).
const OrdersAdminPageSlug = "commerce-orders"

// Plugin is the commerce capability's sdk.Plugin implementation.
type Plugin struct {
	Gateway PaymentGateway
}

// New returns a commerce Plugin whose checkout flow (StartCheckout) makes
// its outbound payment-gateway calls through gateway.
func New(gateway PaymentGateway) *Plugin {
	return &Plugin{Gateway: gateway}
}

// Manifest declares this capability's identity and its consent-relevant
// axes: content[read,write] (to register/manage product & order content),
// events[emit] (order.placed/payment.completed/payment.refunded),
// payments[charge,refund] (the exact scopes PRD §7.4's own example uses,
// and the capability sensitiveEvents gates these three events on),
// admin_ui (order-management admin page), and network (the payment
// gateway's allowlisted host — see NetworkPermission).
func (p *Plugin) Manifest() sdk.Manifest {
	perms := []sdk.Permission{{Name: "admin_ui"}}
	if p.Gateway != nil {
		if host := p.Gateway.AllowlistHost(); host != "" {
			perms = append(perms, sdk.Permission{Name: "network", Args: []string{host}})
		}
	}
	return sdk.Manifest{
		Name:    "commerce",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read", "write"}},
			{Capability: "events", Scopes: []string{"emit"}},
			{Capability: "payments", Scopes: []string{"charge", "refund"}},
		},
		Permissions: perms,
	}
}

// Register defines the product and order content types and registers the
// order-management admin page, entirely through host — a denial here (e.g.
// a host built from a manifest that dropped content:write or admin_ui)
// propagates unmodified, proving Register carries no bypass of pkg/sdk's
// own gate.
func (p *Plugin) Register(host sdk.HostAPI) error {
	if err := host.RegisterContentType(context.Background(), ProductContentType, contract.ContentType{
		Fields: map[string]contract.Field{
			"name":        {Type: contract.FieldString, Required: true},
			"price_cents": {Type: contract.FieldNumber, Required: true},
			"currency":    {Type: contract.FieldString, Required: true},
			"description": {Type: contract.FieldString},
		},
	}); err != nil {
		return err
	}
	if err := host.RegisterContentType(context.Background(), OrderContentType, contract.ContentType{
		Fields: map[string]contract.Field{
			"product_id":          {Type: contract.FieldString, Required: true},
			"quantity":            {Type: contract.FieldNumber, Required: true},
			"amount_cents":        {Type: contract.FieldNumber, Required: true},
			"currency":            {Type: contract.FieldString, Required: true},
			"status":              {Type: contract.FieldString, Required: true},
			"checkout_session_id": {Type: contract.FieldString},
		},
	}); err != nil {
		return err
	}
	return host.RegisterAdminPage(sdk.AdminPageDef{
		Slug:  OrdersAdminPageSlug,
		Title: "Orders",
	})
}
