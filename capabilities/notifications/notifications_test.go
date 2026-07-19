package notifications_test

import (
	"context"
	"testing"

	"github.com/glyphux/glyphux/capabilities/notifications"
	"github.com/glyphux/glyphux/pkg/sdk"
)

func TestManifestIsWellFormed(t *testing.T) {
	p := notifications.New(notifications.NewMemoryMailerAdapter())
	if err := p.Manifest().Validate(); err != nil {
		t.Fatalf("expected notifications's own manifest to be valid, got %v", err)
	}
}

func TestRegisterSubscribesToAllFourEventsWithoutError(t *testing.T) {
	p := notifications.New(notifications.NewMemoryMailerAdapter())
	host, err := sdk.NewHostAPI(p.Manifest(), sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestRegisterDeniedWithoutUsersReadScope(t *testing.T) {
	// A HostAPI built from a manifest that dropped users:read (unlike
	// notifications's own manifest, which declares it for user.created)
	// must refuse Register's subscription to user.created — pkg/sdk's own
	// gate, exercised here to prove Register carries no bypass of it.
	restricted := sdk.Manifest{
		Name: "notifications", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"subscribe"}},
			{Capability: "payments", Scopes: []string{"charge"}},
			{Capability: "membership", Scopes: []string{"manage"}},
		},
	}
	host, err := sdk.NewHostAPI(restricted, sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	p := notifications.New(notifications.NewMemoryMailerAdapter())
	if err := p.Register(host); err == nil {
		t.Fatal("expected Register to be denied against a host built without users:read")
	}
}

func TestUserCreatedEventDispatchesWelcomeEmailAcrossSharedBus(t *testing.T) {
	// Proves cross-plugin dispatch (mirroring slice 2.2's own
	// TestHostAPIEventBusIsSharedAcrossPluginsOnSameKernelDeps): a separate
	// "emitter" HostAPI instance sharing the same KernelDeps.Bus emits
	// user.created, and this capability's own registered subscriber
	// receives it and dispatches through the mailer adapter.
	deps := sdk.KernelDeps{Bus: sdk.NewEventBus()}

	adapter := notifications.NewMemoryMailerAdapter()
	p := notifications.New(adapter)
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	emitterManifest := sdk.Manifest{
		Name: "emitter-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"emit"}},
			{Capability: "users", Scopes: []string{"read"}},
		},
	}
	emitter, err := sdk.NewHostAPI(emitterManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (emitter): %v", err)
	}

	event := notifications.UserCreatedEvent{Email: "new-user@example.com", Name: "Ada"}
	if err := emitter.Emit(context.Background(), "user.created", event); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	sent := adapter.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected exactly 1 sent message, got %d: %+v", len(sent), sent)
	}
	if sent[0].To != "new-user@example.com" {
		t.Errorf("got To=%q, want %q", sent[0].To, "new-user@example.com")
	}
	if sent[0].Subject == "" || sent[0].Body == "" {
		t.Errorf("expected non-empty subject/body, got %+v", sent[0])
	}
}

func TestPaymentCompletedEventDispatchesReceiptEmailAcrossSharedBus(t *testing.T) {
	deps := sdk.KernelDeps{Bus: sdk.NewEventBus()}

	adapter := notifications.NewMemoryMailerAdapter()
	p := notifications.New(adapter)
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	emitterManifest := sdk.Manifest{
		Name: "commerce-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"emit"}},
			{Capability: "payments", Scopes: []string{"charge"}},
		},
	}
	emitter, err := sdk.NewHostAPI(emitterManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (emitter): %v", err)
	}

	event := notifications.PaymentCompletedEvent{Email: "buyer@example.com", OrderID: "ord_123", Amount: "$42.00"}
	if err := emitter.Emit(context.Background(), "payment.completed", event); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	sent := adapter.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected exactly 1 sent message, got %d: %+v", len(sent), sent)
	}
	if sent[0].To != "buyer@example.com" {
		t.Errorf("got To=%q, want %q", sent[0].To, "buyer@example.com")
	}
}

func TestMembershipStartedEventDispatchesWelcomeEmailAcrossSharedBus(t *testing.T) {
	deps := sdk.KernelDeps{Bus: sdk.NewEventBus()}

	adapter := notifications.NewMemoryMailerAdapter()
	p := notifications.New(adapter)
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	emitterManifest := sdk.Manifest{
		Name: "membership-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"emit"}},
			{Capability: "membership", Scopes: []string{"manage"}},
		},
	}
	emitter, err := sdk.NewHostAPI(emitterManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (emitter): %v", err)
	}

	event := notifications.MembershipEvent{Email: "member@example.com", PlanName: "Gold"}
	if err := emitter.Emit(context.Background(), "membership.started", event); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	sent := adapter.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected exactly 1 sent message, got %d: %+v", len(sent), sent)
	}
	if sent[0].To != "member@example.com" {
		t.Errorf("got To=%q, want %q", sent[0].To, "member@example.com")
	}
}

func TestMembershipExpiredEventDispatchesReminderEmailAcrossSharedBus(t *testing.T) {
	deps := sdk.KernelDeps{Bus: sdk.NewEventBus()}

	adapter := notifications.NewMemoryMailerAdapter()
	p := notifications.New(adapter)
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	emitterManifest := sdk.Manifest{
		Name: "membership-plugin", Version: "1.0.0", Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{Core: ">=0.1.0", Contract: "content-composition/v0"},
		API: []sdk.APIScope{
			{Capability: "events", Scopes: []string{"emit"}},
			{Capability: "membership", Scopes: []string{"manage"}},
		},
	}
	emitter, err := sdk.NewHostAPI(emitterManifest, deps)
	if err != nil {
		t.Fatalf("NewHostAPI (emitter): %v", err)
	}

	event := notifications.MembershipEvent{Email: "member@example.com", PlanName: "Gold"}
	if err := emitter.Emit(context.Background(), "membership.expired", event); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	sent := adapter.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected exactly 1 sent message, got %d: %+v", len(sent), sent)
	}
	if sent[0].Subject == "" {
		t.Errorf("expected non-empty subject, got %+v", sent[0])
	}
}

func TestWrongPayloadTypeReturnsErrorInsteadOfSendingAnything(t *testing.T) {
	deps := sdk.KernelDeps{Bus: sdk.NewEventBus()}

	adapter := notifications.NewMemoryMailerAdapter()
	p := notifications.New(adapter)
	host, err := sdk.NewHostAPI(p.Manifest(), deps)
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	if err := p.Register(host); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := host.Emit(context.Background(), "user.created", "not-the-expected-struct"); err == nil {
		t.Fatal("expected Emit to report the handler's type-assertion error")
	}
	if len(adapter.Sent()) != 0 {
		t.Fatalf("expected no message sent for a malformed payload, got %+v", adapter.Sent())
	}
}
