package membership_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/capabilities/membership"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// testKernel wires real content/composition stores over a fresh SQLite DB —
// this capability's real backing, mirroring capabilities/forms's,
// capabilities/seo's, and capabilities/commerce's own test convention.
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

// newRegisteredHost builds a real HostAPI for the membership Plugin, backed
// by deps, and calls Register on it — the setup every test below needs.
func newRegisteredHost(t *testing.T, deps sdk.KernelDeps, gateway membership.RecurringGateway) (sdk.HostAPI, *membership.Plugin) {
	t.Helper()
	p := membership.New(gateway)
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return host, p
}

func TestManifestIsValid(t *testing.T) {
	p := membership.New(nil)
	if err := p.Manifest().Validate(); err != nil {
		t.Fatalf("Manifest().Validate(): %v", err)
	}
}

func TestRegisterDefinesContentTypes(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)

	if _, err := host.Content().List(context.Background(), membership.TierContentType); err != nil {
		t.Fatalf("membership_tier not registered: %v", err)
	}
	if _, err := host.Content().List(context.Background(), membership.SubscriptionContentType); err != nil {
		t.Fatalf("membership_subscription not registered: %v", err)
	}
}

func TestRegisterDeniedWithoutContentWrite(t *testing.T) {
	deps := testKernel(t)
	manifest := sdk.Manifest{
		Name:    "membership",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "content", Scopes: []string{"read"}}, // no write
		},
	}
	host, err := sdk.NewHostAPI(manifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	p := membership.New(nil)
	if err := p.Register(host); err == nil {
		t.Fatal("Register: expected error without content:write, got nil")
	}
}

func TestCreateTierAndSubscribe(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}

	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if subID == "" {
		t.Fatal("Subscribe: expected non-empty subscription ID")
	}

	item, err := host.Content().Get(ctx, membership.SubscriptionContentType, subID)
	if err != nil {
		t.Fatalf("Get subscription: %v", err)
	}
	if status := item.Data["status"]; status != membership.StatusActive {
		t.Fatalf("subscription status = %v, want %v", status, membership.StatusActive)
	}
}

// subscriberHost builds a SEPARATE HostAPI on the same shared event bus —
// mirrors capabilities/commerce's/capabilities/notifications's own
// cross-plugin event-verification pattern — proving membership.started/
// membership.expired are real, cross-plugin bus events, not merely an
// in-process function call this package's own code happens to make.
func subscriberHost(t *testing.T, deps sdk.KernelDeps) sdk.HostAPI {
	t.Helper()
	manifest := sdk.Manifest{
		Name:    "membership-subscriber",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"subscribe"}},
			{Capability: "membership", Scopes: []string{"manage"}},
		},
	}
	host, err := sdk.NewHostAPI(manifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (subscriber): %v", err)
	}
	return host
}

func TestSubscribeEmitsMembershipStarted(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	sub := subscriberHost(t, deps)
	var got membership.SubscriptionEvent
	received := make(chan struct{}, 1)
	if err := sub.On("membership.started", func(ctx context.Context, payload any) error {
		event, ok := payload.(membership.SubscriptionEvent)
		if !ok {
			t.Fatalf("membership.started payload has unexpected type %T", payload)
		}
		got = event
		received <- struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("On(membership.started): %v", err)
	}

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	select {
	case <-received:
	default:
		t.Fatal("membership.started was not received by subscriber")
	}
	if got.SubscriptionID != subID || got.UserID != "user-1" || got.TierID != tierID {
		t.Fatalf("membership.started payload = %+v, want SubscriptionID=%s UserID=user-1 TierID=%s", got, subID, tierID)
	}
}

func TestOnMembershipStartedDeniedWithoutMembershipCapability(t *testing.T) {
	deps := testKernel(t)
	manifest := sdk.Manifest{
		Name:    "membership-subscriber-no-cap",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"subscribe"}},
		},
	}
	host, err := sdk.NewHostAPI(manifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := host.On("membership.started", func(ctx context.Context, payload any) error { return nil }); err == nil {
		t.Fatal("On(membership.started): expected error without membership capability declared, got nil")
	}
}

func TestCancelSubscriptionEmitsMembershipExpired(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	sub := subscriberHost(t, deps)
	received := make(chan membership.SubscriptionEvent, 1)
	if err := sub.On("membership.expired", func(ctx context.Context, payload any) error {
		event, ok := payload.(membership.SubscriptionEvent)
		if !ok {
			t.Fatalf("membership.expired payload has unexpected type %T", payload)
		}
		received <- event
		return nil
	}); err != nil {
		t.Fatalf("On(membership.expired): %v", err)
	}

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := membership.CancelSubscription(ctx, host, subID); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}

	select {
	case event := <-received:
		if event.SubscriptionID != subID {
			t.Fatalf("membership.expired SubscriptionID = %v, want %v", event.SubscriptionID, subID)
		}
	default:
		t.Fatal("membership.expired was not received by subscriber")
	}

	item, err := host.Content().Get(ctx, membership.SubscriptionContentType, subID)
	if err != nil {
		t.Fatalf("Get subscription: %v", err)
	}
	if status := item.Data["status"]; status != membership.StatusCancelled {
		t.Fatalf("subscription status = %v, want %v", status, membership.StatusCancelled)
	}
}

func TestHasActiveMembershipTrueForActiveSubscriptionAtRequiredTier(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	if _, err := membership.Subscribe(ctx, host, "user-1", tierID); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ok, err := membership.HasActiveMembership(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("HasActiveMembership: %v", err)
	}
	if !ok {
		t.Fatal("HasActiveMembership: want true for an active subscription at the required tier")
	}
}

func TestHasActiveMembershipTrueWhenSubscribedTierRanksAboveRequired(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	silverID, err := membership.CreateTier(ctx, host, "Silver", 499, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier(Silver): %v", err)
	}
	goldID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 2)
	if err != nil {
		t.Fatalf("CreateTier(Gold): %v", err)
	}
	if _, err := membership.Subscribe(ctx, host, "user-1", goldID); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ok, err := membership.HasActiveMembership(ctx, host, "user-1", silverID)
	if err != nil {
		t.Fatalf("HasActiveMembership: %v", err)
	}
	if !ok {
		t.Fatal("HasActiveMembership: want true when subscribed tier ranks above the required tier")
	}
}

func TestHasActiveMembershipFalseWhenSubscribedTierRanksBelowRequired(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	silverID, err := membership.CreateTier(ctx, host, "Silver", 499, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier(Silver): %v", err)
	}
	goldID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 2)
	if err != nil {
		t.Fatalf("CreateTier(Gold): %v", err)
	}
	if _, err := membership.Subscribe(ctx, host, "user-1", silverID); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ok, err := membership.HasActiveMembership(ctx, host, "user-1", goldID)
	if err != nil {
		t.Fatalf("HasActiveMembership: %v", err)
	}
	if ok {
		t.Fatal("HasActiveMembership: want false when subscribed tier ranks below the required tier")
	}
}

func TestHasActiveMembershipFalseWithNoSubscription(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}

	ok, err := membership.HasActiveMembership(ctx, host, "user-never-subscribed", tierID)
	if err != nil {
		t.Fatalf("HasActiveMembership: %v", err)
	}
	if ok {
		t.Fatal("HasActiveMembership: want false for a user with no subscription at all")
	}
}

func TestHasActiveMembershipFalseAfterCancellation(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := membership.CancelSubscription(ctx, host, subID); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}

	ok, err := membership.HasActiveMembership(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("HasActiveMembership: %v", err)
	}
	if ok {
		t.Fatal("HasActiveMembership: want false after cancellation")
	}
}

func TestHasActiveMembershipFalseWhenPeriodElapsedEvenIfStatusStillActive(t *testing.T) {
	deps := testKernel(t)
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Directly patch the subscription's period into the past while leaving
	// status StatusActive — simulating a renewal job that hasn't yet run,
	// proving HasActiveMembership checks the real elapsed time rather than
	// trusting a possibly-stale status field.
	item, err := host.Content().Get(ctx, membership.SubscriptionContentType, subID)
	if err != nil {
		t.Fatalf("Get subscription: %v", err)
	}
	merged := map[string]any{}
	for k, v := range item.Data {
		merged[k] = v
	}
	merged["current_period_end"] = "2000-01-01T00:00:00Z"
	if _, err := host.Content().Update(ctx, membership.SubscriptionContentType, subID, merged); err != nil {
		t.Fatalf("Update subscription: %v", err)
	}

	ok, err := membership.HasActiveMembership(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("HasActiveMembership: %v", err)
	}
	if ok {
		t.Fatal("HasActiveMembership: want false once current_period_end has elapsed, even with status still active")
	}
}

func TestProcessRenewalSuccessExtendsPeriod(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeRecurringGateway()
	defer gw.Close()
	stripeGW, err := membership.NewStripeRecurringGateway("sk_test_123", gw.URL())
	if err != nil {
		t.Fatalf("NewStripeRecurringGateway: %v", err)
	}
	host, _ := newRegisteredHost(t, deps, stripeGW)
	ctx := context.Background()

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	before, err := host.Content().Get(ctx, membership.SubscriptionContentType, subID)
	if err != nil {
		t.Fatalf("Get subscription: %v", err)
	}

	gw.SetOutcome(subID, true)
	if err := membership.ProcessRenewal(ctx, host, stripeGW, subID); err != nil {
		t.Fatalf("ProcessRenewal: %v", err)
	}

	after, err := host.Content().Get(ctx, membership.SubscriptionContentType, subID)
	if err != nil {
		t.Fatalf("Get subscription: %v", err)
	}
	if after.Data["status"] != membership.StatusActive {
		t.Fatalf("status after successful renewal = %v, want %v", after.Data["status"], membership.StatusActive)
	}
	if after.Data["current_period_end"] == before.Data["current_period_end"] {
		t.Fatal("current_period_end was not extended after a successful renewal")
	}
}

func TestProcessRenewalFailureExpiresSubscription(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeRecurringGateway()
	defer gw.Close()
	stripeGW, err := membership.NewStripeRecurringGateway("sk_test_123", gw.URL())
	if err != nil {
		t.Fatalf("NewStripeRecurringGateway: %v", err)
	}
	host, _ := newRegisteredHost(t, deps, stripeGW)
	ctx := context.Background()

	sub := subscriberHost(t, deps)
	received := make(chan membership.SubscriptionEvent, 1)
	if err := sub.On("membership.expired", func(ctx context.Context, payload any) error {
		received <- payload.(membership.SubscriptionEvent)
		return nil
	}); err != nil {
		t.Fatalf("On(membership.expired): %v", err)
	}

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	gw.SetOutcome(subID, false)
	if err := membership.ProcessRenewal(ctx, host, stripeGW, subID); err != nil {
		t.Fatalf("ProcessRenewal: %v", err)
	}

	after, err := host.Content().Get(ctx, membership.SubscriptionContentType, subID)
	if err != nil {
		t.Fatalf("Get subscription: %v", err)
	}
	if after.Data["status"] != membership.StatusExpired {
		t.Fatalf("status after failed renewal = %v, want %v", after.Data["status"], membership.StatusExpired)
	}

	select {
	case event := <-received:
		if event.SubscriptionID != subID {
			t.Fatalf("membership.expired SubscriptionID = %v, want %v", event.SubscriptionID, subID)
		}
	default:
		t.Fatal("membership.expired was not received by subscriber after a failed renewal")
	}

	ok, err := membership.HasActiveMembership(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("HasActiveMembership: %v", err)
	}
	if ok {
		t.Fatal("HasActiveMembership: want false after a failed renewal expired the subscription")
	}
}

func TestProcessRenewalRefusedAgainstDisallowedGatewayHost(t *testing.T) {
	deps := testKernel(t)
	gw := newFakeRecurringGateway()
	defer gw.Close()

	// Build the plugin with NO gateway (so its manifest declares no network
	// permission at all), then attempt ProcessRenewal against a gateway
	// whose host was never allowlisted — proving the refusal is the
	// permission gate itself, not an incidental connection failure.
	host, _ := newRegisteredHost(t, deps, nil)
	ctx := context.Background()

	tierID, err := membership.CreateTier(ctx, host, "Gold", 999, "usd", membership.BillingIntervalMonthly, 1)
	if err != nil {
		t.Fatalf("CreateTier: %v", err)
	}
	subID, err := membership.Subscribe(ctx, host, "user-1", tierID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	stripeGW, err := membership.NewStripeRecurringGateway("sk_test_123", gw.URL())
	if err != nil {
		t.Fatalf("NewStripeRecurringGateway: %v", err)
	}
	err = membership.ProcessRenewal(ctx, host, stripeGW, subID)
	if err == nil {
		t.Fatal("ProcessRenewal: expected error against a disallowed gateway host, got nil")
	}
}
