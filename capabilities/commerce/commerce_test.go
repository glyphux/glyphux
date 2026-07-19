package commerce_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/capabilities/commerce"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

const testWebhookSecret = "whsec_test_secret"

// testKernel wires real content/composition stores over a fresh SQLite DB —
// this capability's real backing, mirroring capabilities/forms's and
// capabilities/seo's own test convention.
func testKernel(t *testing.T) sdk.KernelDeps {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), content.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}
	return sdk.KernelDeps{
		Compositions: comps,
		Content:      content.NewAPI(comps, content.NewStore(d)),
		Bus:          sdk.NewEventBus(),
	}
}

// newRegisteredHost builds a real HostAPI for the commerce Plugin, backed
// by deps, and calls Register on it — the setup every test below needs.
func newRegisteredHost(t *testing.T, deps sdk.KernelDeps, gateway commerce.PaymentGateway) (sdk.HostAPI, *commerce.Plugin) {
	t.Helper()
	p := commerce.New(gateway)
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return host, p
}

// webhookReceiver is a merchant-side httptest server exposing exactly one
// endpoint the fakeGateway's simulated webhook delivery POSTs to, which
// funnels straight into commerce.HandleWebhook — proving the whole
// receive-verify-mutate-emit path runs over a real HTTP round trip, not a
// direct in-process function call.
func newWebhookReceiver(host sdk.HostAPI, gateway commerce.PaymentGateway) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := commerce.HandleWebhook(r.Context(), host, gateway, body, r.Header.Get("Stripe-Signature")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}

func TestManifestIsWellFormed(t *testing.T) {
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	p := commerce.New(stripeGW)
	if err := p.Manifest().Validate(); err != nil {
		t.Fatalf("expected commerce's own manifest to be valid, got %v", err)
	}
}

func TestManifestDeclaresGatewayHostInNetworkAllowlist(t *testing.T) {
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	p := commerce.New(stripeGW)
	manifest := p.Manifest()

	gwURL, _ := url.Parse(gw.URL())
	if !manifest.AllowsNetworkHost(gwURL.Hostname()) {
		t.Fatalf("expected manifest's network permission to allowlist %q", gwURL.Hostname())
	}
	if manifest.AllowsNetworkHost("evil.example.com") {
		t.Fatal("expected manifest's network permission to deny an undeclared host")
	}
}

func TestRegisterDefinesProductAndOrderContentTypes(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	newRegisteredHost(t, deps, stripeGW)

	comp, err := deps.Compositions.Load(context.Background())
	if err != nil {
		t.Fatalf("load composition: %v", err)
	}
	if _, ok := comp.ContentTypes[commerce.ProductContentType]; !ok {
		t.Fatal("expected product content type to be defined after Register")
	}
	if _, ok := comp.ContentTypes[commerce.OrderContentType]; !ok {
		t.Fatal("expected order content type to be defined after Register")
	}
}

func TestRegisterRegistersOrdersAdminPage(t *testing.T) {
	// Proves the admin-UI registration boundary this ticket documents as its
	// scope: RegisterAdminPage records the definition server-side; building
	// the actual admin-ui React page is explicitly out of scope for this
	// Go-package ticket (see this slice's tracking doc). We can't inspect
	// hostAPI's private adminPages list directly (it's unexported), so this
	// asserts the only externally-observable fact: Register succeeds against
	// a host whose manifest declares admin_ui (which commerce's own Manifest
	// always does), proving RegisterAdminPage's gate was satisfied rather
	// than silently skipped.
	deps := testKernel(t)
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	newRegisteredHost(t, deps, stripeGW)
}

func TestRegisterDeniedWithoutContentWriteScope(t *testing.T) {
	deps := testKernel(t)
	restricted := sdk.Manifest{
		Name: "commerce", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires:    sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		Permissions: []sdk.Permission{{Name: "admin_ui"}},
	}
	host, err := sdk.NewHostAPI(restricted, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	p := commerce.New(stripeGW)
	if err := p.Register(host); err == nil {
		t.Fatal("expected Register to be denied against a host built without content:write")
	}
}

func TestRegisterDeniedWithoutAdminUIPermission(t *testing.T) {
	deps := testKernel(t)
	restricted := sdk.Manifest{
		Name: "commerce", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read", "write"}},
			{Capability: "events", Scopes: []string{"emit"}},
			{Capability: "payments", Scopes: []string{"charge", "refund"}},
		},
	}
	host, err := sdk.NewHostAPI(restricted, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	p := commerce.New(stripeGW)
	if err := p.Register(host); err == nil {
		t.Fatal("expected Register to be denied against a host built without admin_ui")
	}
}

func TestCreateProductThenOrderPlacedEmitsOrderPlacedAcrossSharedBus(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	host, _ := newRegisteredHost(t, deps, stripeGW)
	ctx := context.Background()

	productID, err := commerce.CreateProduct(ctx, host, "Glyphux T-Shirt", 2500, "usd", "A shirt")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if productID == "" {
		t.Fatal("expected a non-empty product id")
	}

	// A separate subscriber HostAPI on the shared bus, mirroring
	// capabilities/notifications's cross-plugin test pattern.
	var received *commerce.OrderPlacedEvent
	subscriberManifest := sdk.Manifest{
		Name: "subscriber-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"subscribe"}},
			{Capability: "payments", Scopes: []string{"charge"}},
		},
	}
	subscriber, err := sdk.NewHostAPI(subscriberManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (subscriber): %v", err)
	}
	if err := subscriber.On("order.placed", func(ctx context.Context, payload any) error {
		e := payload.(commerce.OrderPlacedEvent)
		received = &e
		return nil
	}); err != nil {
		t.Fatalf("On: %v", err)
	}

	orderID, err := commerce.CreateOrder(ctx, host, productID, 1, 2500, "usd")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if orderID == "" {
		t.Fatal("expected a non-empty order id")
	}
	if received == nil {
		t.Fatal("expected order.placed to be observed by the subscriber")
	}
	if received.OrderID != orderID {
		t.Errorf("got OrderID=%q, want %q", received.OrderID, orderID)
	}
	if received.AmountCents != 2500 {
		t.Errorf("got AmountCents=%d, want 2500", received.AmountCents)
	}

	item, err := host.Content().Get(ctx, commerce.OrderContentType, orderID)
	if err != nil {
		t.Fatalf("Get order: %v", err)
	}
	if item.Data["status"] != commerce.OrderStatusPending {
		t.Errorf("got status=%v, want %q", item.Data["status"], commerce.OrderStatusPending)
	}
}

func TestStartCheckoutHitsFakeGatewayAndReturnsSession(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	host, _ := newRegisteredHost(t, deps, stripeGW)
	ctx := context.Background()

	productID, err := commerce.CreateProduct(ctx, host, "Mug", 1000, "usd", "")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	orderID, err := commerce.CreateOrder(ctx, host, productID, 1, 1000, "usd")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	sess, err := commerce.StartCheckout(ctx, host, stripeGW, commerce.CheckoutRequest{
		OrderID:     orderID,
		AmountCents: 1000,
		Currency:    "usd",
		ProductName: "Mug",
		SuccessURL:  "https://example.com/success",
		CancelURL:   "https://example.com/cancel",
	})
	if err != nil {
		t.Fatalf("StartCheckout: %v", err)
	}
	if sess.ID == "" || sess.URL == "" {
		t.Fatalf("expected a non-empty session id and url, got %+v", sess)
	}

	item, err := host.Content().Get(ctx, commerce.OrderContentType, orderID)
	if err != nil {
		t.Fatalf("Get order: %v", err)
	}
	if item.Data["checkout_session_id"] != sess.ID {
		t.Errorf("got checkout_session_id=%v, want %q", item.Data["checkout_session_id"], sess.ID)
	}
}

func TestStartCheckoutRefusedAgainstDisallowedGatewayHost(t *testing.T) {
	deps := testKernel(t)

	// One fake gateway server, reached under two different hostnames:
	// gw.URL()'s own "127.0.0.1" (what commerce's manifest allowlists, via
	// commerce.New(allowedGateway)), and "localhost" pointed at the exact
	// same port (a second StripeGateway the manifest never declared).
	// "localhost" resolves to the same loopback address, so the connection
	// itself would succeed if attempted — proving AllowsNetworkHost's
	// deny-by-default semantics (slice 2.6) are what refuses the call, not
	// an incidental connection failure.
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()

	allowedGateway, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway (allowed): %v", err)
	}
	gwURL, err := url.Parse(gw.URL())
	if err != nil {
		t.Fatalf("parse fake gateway URL: %v", err)
	}
	disallowedGateway, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, "http://localhost:"+gwURL.Port())
	if err != nil {
		t.Fatalf("NewStripeGateway (disallowed): %v", err)
	}

	host, _ := newRegisteredHost(t, deps, allowedGateway)
	ctx := context.Background()

	productID, err := commerce.CreateProduct(ctx, host, "Mug", 1000, "usd", "")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	orderID, err := commerce.CreateOrder(ctx, host, productID, 1, 1000, "usd")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	_, err = commerce.StartCheckout(ctx, host, disallowedGateway, commerce.CheckoutRequest{
		OrderID:     orderID,
		AmountCents: 1000,
		Currency:    "usd",
		ProductName: "Mug",
		SuccessURL:  "https://example.com/success",
		CancelURL:   "https://example.com/cancel",
	})
	if err == nil {
		t.Fatal("expected StartCheckout to be refused against a gateway host the manifest doesn't allowlist")
	}
}

func TestWebhookCompletedMarksOrderPaidAndEmitsPaymentCompleted(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	host, _ := newRegisteredHost(t, deps, stripeGW)
	ctx := context.Background()

	productID, err := commerce.CreateProduct(ctx, host, "Mug", 1000, "usd", "")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	orderID, err := commerce.CreateOrder(ctx, host, productID, 1, 1000, "usd")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	sess, err := commerce.StartCheckout(ctx, host, stripeGW, commerce.CheckoutRequest{
		OrderID:     orderID,
		AmountCents: 1000,
		Currency:    "usd",
		ProductName: "Mug",
		SuccessURL:  "https://example.com/success",
		CancelURL:   "https://example.com/cancel",
	})
	if err != nil {
		t.Fatalf("StartCheckout: %v", err)
	}

	receiver := newWebhookReceiver(host, stripeGW)
	defer receiver.Close()
	gw.SetWebhookURL(receiver.URL)

	// A separate subscriber HostAPI on the shared bus verifies
	// payment.completed the same cross-plugin way notifications's tests do.
	var received *commerce.PaymentEvent
	subscriberManifest := sdk.Manifest{
		Name: "subscriber-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"subscribe"}},
			{Capability: "payments", Scopes: []string{"charge"}},
		},
	}
	subscriber, err := sdk.NewHostAPI(subscriberManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (subscriber): %v", err)
	}
	if err := subscriber.On("payment.completed", func(ctx context.Context, payload any) error {
		e := payload.(commerce.PaymentEvent)
		received = &e
		return nil
	}); err != nil {
		t.Fatalf("On: %v", err)
	}

	status, err := gw.CompleteSession(sess.ID)
	if err != nil {
		t.Fatalf("CompleteSession: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected webhook receiver to respond 200, got %d", status)
	}

	if received == nil {
		t.Fatal("expected payment.completed to be observed by the subscriber")
	}
	if received.OrderID != orderID {
		t.Errorf("got OrderID=%q, want %q", received.OrderID, orderID)
	}

	item, err := host.Content().Get(ctx, commerce.OrderContentType, orderID)
	if err != nil {
		t.Fatalf("Get order: %v", err)
	}
	if item.Data["status"] != commerce.OrderStatusPaid {
		t.Errorf("got status=%v, want %q", item.Data["status"], commerce.OrderStatusPaid)
	}
}

func TestWebhookRefundedMarksOrderRefundedAndEmitsPaymentRefunded(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	host, _ := newRegisteredHost(t, deps, stripeGW)
	ctx := context.Background()

	productID, err := commerce.CreateProduct(ctx, host, "Mug", 1000, "usd", "")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	orderID, err := commerce.CreateOrder(ctx, host, productID, 1, 1000, "usd")
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	sess, err := commerce.StartCheckout(ctx, host, stripeGW, commerce.CheckoutRequest{
		OrderID:     orderID,
		AmountCents: 1000,
		Currency:    "usd",
		ProductName: "Mug",
		SuccessURL:  "https://example.com/success",
		CancelURL:   "https://example.com/cancel",
	})
	if err != nil {
		t.Fatalf("StartCheckout: %v", err)
	}

	receiver := newWebhookReceiver(host, stripeGW)
	defer receiver.Close()
	gw.SetWebhookURL(receiver.URL)

	var received *commerce.PaymentEvent
	subscriberManifest := sdk.Manifest{
		Name: "subscriber-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"subscribe"}},
			{Capability: "payments", Scopes: []string{"charge"}},
		},
	}
	subscriber, err := sdk.NewHostAPI(subscriberManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (subscriber): %v", err)
	}
	if err := subscriber.On("payment.refunded", func(ctx context.Context, payload any) error {
		e := payload.(commerce.PaymentEvent)
		received = &e
		return nil
	}); err != nil {
		t.Fatalf("On: %v", err)
	}

	status, err := gw.RefundSession(sess.ID)
	if err != nil {
		t.Fatalf("RefundSession: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected webhook receiver to respond 200, got %d", status)
	}

	if received == nil {
		t.Fatal("expected payment.refunded to be observed by the subscriber")
	}
	if received.OrderID != orderID {
		t.Errorf("got OrderID=%q, want %q", received.OrderID, orderID)
	}

	item, err := host.Content().Get(ctx, commerce.OrderContentType, orderID)
	if err != nil {
		t.Fatalf("Get order: %v", err)
	}
	if item.Data["status"] != commerce.OrderStatusRefunded {
		t.Errorf("got status=%v, want %q", item.Data["status"], commerce.OrderStatusRefunded)
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeGateway(testWebhookSecret)
	defer gw.Close()
	stripeGW, err := commerce.NewStripeGateway("sk_test_123", testWebhookSecret, gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway: %v", err)
	}
	host, _ := newRegisteredHost(t, deps, stripeGW)

	wrongGW, err := commerce.NewStripeGateway("sk_test_123", "whsec_wrong_secret", gw.URL())
	if err != nil {
		t.Fatalf("NewStripeGateway (wrong secret): %v", err)
	}

	err = commerce.HandleWebhook(context.Background(), host, wrongGW, []byte(`{"type":"checkout.session.completed"}`), "t=1,v1=deadbeef")
	if err == nil {
		t.Fatal("expected HandleWebhook to reject a badly-signed payload")
	}
}
