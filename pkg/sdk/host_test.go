package sdk_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/kernel"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// testKernel wires real content/composition/media/identity APIs over a
// fresh SQLite DB — a HostAPI's real backing, exactly like a running
// glyphuxd, so gating behavior is exercised against genuine domain APIs
// rather than mocks.
func testKernel(t *testing.T) sdk.KernelDeps {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, media.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
			}},
		},
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}
	return sdk.KernelDeps{
		Compositions: comps,
		Content:      content.NewAPI(comps, content.NewStore(d)),
		Media:        media.NewAPI(media.NewStore(d), t.TempDir()),
		Identities:   identity.NewService(d),
		KV:           sdk.NewMemoryKVBackend(),
	}
}

func manifestWithAPI(scopes ...sdk.APIScope) sdk.Manifest {
	m := validManifest()
	m.API = scopes
	return m
}

func TestHostAPIContentIsNilWithoutDeclaredCapability(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if host.Content() != nil {
		t.Error("expected Content() to be nil when manifest declares no content api scope")
	}
}

func TestHostAPIContentReadWorksWhenDeclared(t *testing.T) {
	m := manifestWithAPI(sdk.APIScope{Capability: "content", Scopes: []string{"read", "write"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := host.Content().Create(context.Background(), "article", map[string]any{"title": "hello"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := host.Content().Get(context.Background(), "article", created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("got id %q, want %q", got.ID, created.ID)
	}
}

func TestHostAPIContentWriteDeniedWhenOnlyReadDeclared(t *testing.T) {
	m := manifestWithAPI(sdk.APIScope{Capability: "content", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = host.Content().Create(context.Background(), "article", map[string]any{"title": "hello"})
	if err == nil {
		t.Fatal("expected Create to be denied when manifest declares content:read only")
	}
}

func TestHostAPIUsersIsNilWithoutDeclaredCapability(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if host.Users() != nil {
		t.Error("expected Users() to be nil when manifest declares no users api scope")
	}
}

func TestHostAPIUsersReadWorksWhenDeclared(t *testing.T) {
	m := manifestWithAPI(sdk.APIScope{Capability: "users", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Users().List(context.Background()); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestHostAPIUsersManageDeniedWhenOnlyReadDeclared(t *testing.T) {
	m := manifestWithAPI(sdk.APIScope{Capability: "users", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Users().UpdateRole(context.Background(), 1, "editor"); err == nil {
		t.Fatal("expected UpdateRole to be denied when manifest declares users:read only")
	}
}

func TestHostAPIMediaIsNilWithoutDeclaredCapability(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if host.Media() != nil {
		t.Error("expected Media() to be nil when manifest declares no media api scope")
	}
}

func TestHostAPIMediaWriteDeniedWhenOnlyReadDeclared(t *testing.T) {
	m := manifestWithAPI(sdk.APIScope{Capability: "media", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Media().Upload(context.Background(), "x.png", "image/png", []byte("x")); err == nil {
		t.Fatal("expected Upload to be denied when manifest declares media:read only")
	}
}

func TestHostAPIMediaReadWorksWhenDeclared(t *testing.T) {
	m := manifestWithAPI(sdk.APIScope{Capability: "media", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Media().List(context.Background()); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func manifestWithPermissions(perms ...sdk.Permission) sdk.Manifest {
	m := validManifest()
	m.Permissions = perms
	return m
}

func TestHostAPIRegisterAdminPageDeniedWithoutPermission(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterAdminPage(sdk.AdminPageDef{Slug: "forms"}); err == nil {
		t.Fatal("expected RegisterAdminPage to be denied without admin_ui permission")
	}
}

func TestHostAPIRegisterAdminPageWorksWithPermission(t *testing.T) {
	m := manifestWithPermissions(sdk.Permission{Name: "admin_ui"})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterAdminPage(sdk.AdminPageDef{Slug: "forms"}); err != nil {
		t.Fatalf("register admin page: %v", err)
	}
}

func TestHostAPIRegisterJobDeniedWithoutPermission(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterJob(sdk.JobDef{Name: "cleanup"}); err == nil {
		t.Fatal("expected RegisterJob to be denied without scheduled_jobs permission")
	}
}

func TestHostAPIRegisterJobWorksWithPermission(t *testing.T) {
	m := manifestWithPermissions(sdk.Permission{Name: "scheduled_jobs"})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterJob(sdk.JobDef{Name: "cleanup"}); err != nil {
		t.Fatalf("register job: %v", err)
	}
}

func newTestAuditLogger(t *testing.T) *audit.Logger {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), audit.Migrations); err != nil {
		t.Fatal(err)
	}
	return audit.NewLogger(d)
}

func TestHostAPIAuditsRegisterAdminPageAllowAndDeny(t *testing.T) {
	logger := newTestAuditLogger(t)
	deps := testKernel(t)
	deps.Audit = logger

	denied, err := sdk.NewHostAPI(validManifest(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := denied.RegisterAdminPage(sdk.AdminPageDef{Slug: "forms"}); err == nil {
		t.Fatal("expected denial without admin_ui permission")
	}

	allowedManifest := manifestWithPermissions(sdk.Permission{Name: "admin_ui"})
	allowedManifest.Name = "forms-plugin"
	allowed, err := sdk.NewHostAPI(allowedManifest, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := allowed.RegisterAdminPage(sdk.AdminPageDef{Slug: "forms"}); err != nil {
		t.Fatalf("register admin page: %v", err)
	}

	deniedRecords, err := logger.ListByPlugin(context.Background(), validManifest().Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(deniedRecords) != 1 || deniedRecords[0].Allowed {
		t.Fatalf("expected exactly one denied audit record for %q, got %+v", validManifest().Name, deniedRecords)
	}

	allowedRecords, err := logger.ListByPlugin(context.Background(), "forms-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if len(allowedRecords) != 1 || !allowedRecords[0].Allowed {
		t.Fatalf("expected exactly one allowed audit record for forms-plugin, got %+v", allowedRecords)
	}
	if allowedRecords[0].Action != "hostapi.register_admin_page" {
		t.Fatalf("unexpected action: %q", allowedRecords[0].Action)
	}
}

func TestHostAPIWithoutAudit_StillWorks(t *testing.T) {
	// KernelDeps.Audit left nil (the default in every other test in this
	// file) must not panic or otherwise change behavior — audit logging is
	// strictly additive.
	host, err := sdk.NewHostAPI(manifestWithPermissions(sdk.Permission{Name: "admin_ui"}), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterAdminPage(sdk.AdminPageDef{Slug: "forms"}); err != nil {
		t.Fatalf("register admin page without audit configured: %v", err)
	}
}

func TestHostAPIStoreRoundTripsAValue(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	store := host.Store()
	if err := store.Set(context.Background(), "greeting", []byte("hello")); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok, err := store.Get(context.Background(), "greeting")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || string(got) != "hello" {
		t.Fatalf("got (%q, %v), want (\"hello\", true)", got, ok)
	}
}

func TestHostAPIStoreIsNamespacedPerPlugin(t *testing.T) {
	kernel := testKernel(t)
	one, err := sdk.NewHostAPI(validManifest(), kernel)
	if err != nil {
		t.Fatal(err)
	}
	otherManifest := validManifest()
	otherManifest.Name = "a-different-plugin"
	two, err := sdk.NewHostAPI(otherManifest, kernel)
	if err != nil {
		t.Fatal(err)
	}

	if err := one.Store().Set(context.Background(), "key", []byte("plugin-one")); err != nil {
		t.Fatalf("set: %v", err)
	}
	_, ok, err := two.Store().Get(context.Background(), "key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok {
		t.Error("expected a different plugin's store to not see plugin one's key")
	}
}

func TestHostAPIStoreDeleteRemovesAValue(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	store := host.Store()
	if err := store.Set(context.Background(), "key", []byte("value")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := store.Delete(context.Background(), "key"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, err := store.Get(context.Background(), "key"); err != nil || ok {
		t.Fatalf("get after delete = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestHostAPIRegisterContentTypeDeniedWithoutContentWrite(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	def := contract.ContentType{Fields: map[string]contract.Field{
		"name": {Type: contract.FieldString, Required: true},
	}}
	if err := host.RegisterContentType(context.Background(), "widget", def); err == nil {
		t.Fatal("expected RegisterContentType to be denied without content:write")
	}
}

func TestHostAPIRegisterContentTypeWorksWithContentWrite(t *testing.T) {
	kernel := testKernel(t)
	m := manifestWithAPI(sdk.APIScope{Capability: "content", Scopes: []string{"write"}})
	host, err := sdk.NewHostAPI(m, kernel)
	if err != nil {
		t.Fatal(err)
	}
	def := contract.ContentType{Fields: map[string]contract.Field{
		"name": {Type: contract.FieldString, Required: true},
	}}
	if err := host.RegisterContentType(context.Background(), "widget", def); err != nil {
		t.Fatalf("register content type: %v", err)
	}

	// It really landed in the composition, not just a no-op success.
	comp, err := kernel.Compositions.Load(context.Background())
	if err != nil {
		t.Fatalf("load composition: %v", err)
	}
	if _, ok := comp.ContentTypes["widget"]; !ok {
		t.Error("expected \"widget\" content type to be defined in the composition")
	}
}

func TestHostAPIRegisterBlockDeniedWithoutContentCapability(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterBlock(sdk.BlockDef{Name: "hero"}); err == nil {
		t.Fatal("expected RegisterBlock to be denied without a declared content api scope")
	}
}

func TestHostAPIRegisterBlockWorksWithContentCapability(t *testing.T) {
	m := manifestWithAPI(sdk.APIScope{Capability: "content", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterBlock(sdk.BlockDef{Name: "hero"}); err != nil {
		t.Fatalf("register block: %v", err)
	}
}

func TestHostAPIRegisterBlockDefinesItInSharedRegistry(t *testing.T) {
	registry := blocks.New()
	deps := testKernel(t)
	deps.Blocks = registry
	m := manifestWithAPI(sdk.APIScope{Capability: "content", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterBlock(sdk.BlockDef{
		Name:        "hero",
		DisplayName: "Hero",
		Props:       map[string]contract.Field{"title": {Type: contract.FieldString}},
		Slots:       []string{"content"},
	}); err != nil {
		t.Fatalf("register block: %v", err)
	}

	def, ok := registry.Get("hero")
	if !ok {
		t.Fatal("expected \"hero\" to be defined in the shared block registry")
	}
	if def.DisplayName != "Hero" || len(def.Slots) != 1 || def.Slots[0] != "content" {
		t.Fatalf("unexpected definition: %+v", def)
	}
}

func TestHostAPIRegisterBlockRejectsDuplicateAcrossPlugins(t *testing.T) {
	registry := blocks.New()
	deps := testKernel(t)
	deps.Blocks = registry
	m := manifestWithAPI(sdk.APIScope{Capability: "content", Scopes: []string{"read"}})

	hostA, err := sdk.NewHostAPI(m, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := hostA.RegisterBlock(sdk.BlockDef{Name: "hero"}); err != nil {
		t.Fatalf("register block: %v", err)
	}

	hostB, err := sdk.NewHostAPI(m, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err := hostB.RegisterBlock(sdk.BlockDef{Name: "hero"}); err == nil {
		t.Fatal("expected a second plugin registering the same block name to be denied")
	}
}

func TestHostAPIRegisterBlockWithoutSharedRegistryStillWorks(t *testing.T) {
	// KernelDeps.Blocks left nil (the default in every other test in this
	// file) must not panic — a private registry is used instead.
	m := manifestWithAPI(sdk.APIScope{Capability: "content", Scopes: []string{"read"}})
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterBlock(sdk.BlockDef{Name: "hero"}); err != nil {
		t.Fatalf("register block without shared registry configured: %v", err)
	}
}

func TestNewHostAPIRejectsCoreConstraintKernelDoesNotSatisfy(t *testing.T) {
	m := validManifest()
	m.Requires.Core = ">=99.0.0"
	if _, err := sdk.NewHostAPI(m, testKernel(t)); !errors.Is(err, sdk.ErrUnsupportedCoreVersion) {
		t.Fatalf("expected ErrUnsupportedCoreVersion, got %v", err)
	}
}

func TestNewHostAPIAcceptsCoreConstraintKernelSatisfies(t *testing.T) {
	m := validManifest()
	m.Requires.Core = ">=0.1.0"
	if _, err := sdk.NewHostAPI(m, testKernel(t)); err != nil {
		t.Fatalf("expected satisfied requires.core to be accepted, got %v", err)
	}
}

func TestNewHostAPIAcceptsExactCoreConstraintMatchingKernelVersion(t *testing.T) {
	m := validManifest()
	m.Requires.Core = kernel.Version
	if _, err := sdk.NewHostAPI(m, testKernel(t)); err != nil {
		t.Fatalf("expected exact-match requires.core to be accepted, got %v", err)
	}
}

func TestNewHostAPIRejectsExactCoreConstraintNotMatchingKernelVersion(t *testing.T) {
	m := validManifest()
	m.Requires.Core = "0.0.1"
	if _, err := sdk.NewHostAPI(m, testKernel(t)); !errors.Is(err, sdk.ErrUnsupportedCoreVersion) {
		t.Fatalf("expected ErrUnsupportedCoreVersion, got %v", err)
	}
}

func TestNewHostAPIRejectsUnknownContractVersion(t *testing.T) {
	m := validManifest()
	m.Requires.Contract = "content-composition/v99"
	if _, err := sdk.NewHostAPI(m, testKernel(t)); !errors.Is(err, sdk.ErrUnsupportedContract) {
		t.Fatalf("expected ErrUnsupportedContract, got %v", err)
	}
}

func TestNewHostAPIAcceptsKnownContractVersion(t *testing.T) {
	m := validManifest()
	m.Requires.Contract = string(contract.ContentCompositionV0)
	if _, err := sdk.NewHostAPI(m, testKernel(t)); err != nil {
		t.Fatalf("expected known requires.contract to be accepted, got %v", err)
	}
}

func TestHostAPIAllowsNetworkHostDelegatesToManifest(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network", Args: []string{"api.stripe.com"}}}
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if !host.AllowsNetworkHost("api.stripe.com") {
		t.Fatal("expected allow: host is in the declared allowlist")
	}
	if host.AllowsNetworkHost("evil.example.com") {
		t.Fatal("expected deny: host is not in the declared allowlist")
	}
}

func manifestWithEvents(scopes ...string) sdk.Manifest {
	return manifestWithAPI(sdk.APIScope{Capability: "events", Scopes: scopes})
}

func TestHostAPIOnAndEmitAreCallable(t *testing.T) {
	host, err := sdk.NewHostAPI(manifestWithEvents("subscribe", "emit"), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("content.after_save", func(context.Context, any) error { return nil }); err != nil {
		t.Fatalf("On: %v", err)
	}
	if err := host.Emit(context.Background(), "content.after_save", map[string]any{"id": "1"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
}

func TestHostAPIOnDeniedWithoutEventsSubscribeScope(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("content.after_save", func(context.Context, any) error { return nil }); err == nil {
		t.Fatal("expected On to be denied without events:subscribe declared")
	}
}

func TestHostAPIEmitDeniedWithoutEventsEmitScope(t *testing.T) {
	host, err := sdk.NewHostAPI(manifestWithEvents("subscribe"), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Emit(context.Background(), "content.after_save", nil); err == nil {
		t.Fatal("expected Emit to be denied without events:emit declared")
	}
}

func TestHostAPIEventBusIsSharedAcrossPluginsOnSameKernelDeps(t *testing.T) {
	kernel := testKernel(t)
	kernel.Bus = sdk.NewEventBus()

	emitter, err := sdk.NewHostAPI(manifestWithEvents("emit"), kernel)
	if err != nil {
		t.Fatal(err)
	}
	subscriberManifest := manifestWithEvents("subscribe")
	subscriberManifest.Name = "a-different-plugin"
	subscriber, err := sdk.NewHostAPI(subscriberManifest, kernel)
	if err != nil {
		t.Fatal(err)
	}

	received := make(chan any, 1)
	if err := subscriber.On("content.after_save", func(_ context.Context, payload any) error {
		received <- payload
		return nil
	}); err != nil {
		t.Fatalf("On: %v", err)
	}

	if err := emitter.Emit(context.Background(), "content.after_save", "hello"); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	select {
	case got := <-received:
		if got != "hello" {
			t.Errorf("got payload %v, want %q", got, "hello")
		}
	default:
		t.Fatal("expected a shared bus to dispatch the emitting plugin's event to the other plugin's subscriber")
	}
}

func TestHostAPIEmitDispatchesToAllSubscribersAndCollectsErrors(t *testing.T) {
	m := manifestWithEvents("subscribe", "emit")
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	var calledA, calledB bool
	if err := host.On("content.after_save", func(context.Context, any) error {
		calledA = true
		return errors.New("handler a failed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.On("content.after_save", func(context.Context, any) error {
		calledB = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err = host.Emit(context.Background(), "content.after_save", nil)
	if err == nil {
		t.Fatal("expected Emit to report the first subscriber's error")
	}
	if !calledA || !calledB {
		t.Errorf("expected both subscribers to run despite the first's error, got calledA=%v calledB=%v", calledA, calledB)
	}
}

func TestHostAPIOnDeniedForSensitiveEventWithoutDomainCapability(t *testing.T) {
	host, err := sdk.NewHostAPI(manifestWithEvents("subscribe"), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("user.created", func(context.Context, any) error { return nil }); err == nil {
		t.Fatal("expected On(\"user.created\") to be denied without users:read declared")
	}
}

func TestHostAPIOnAllowedForSensitiveEventWithDomainCapability(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{
		{Capability: "events", Scopes: []string{"subscribe"}},
		{Capability: "users", Scopes: []string{"read"}},
	}
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("user.created", func(context.Context, any) error { return nil }); err != nil {
		t.Fatalf("On(\"user.created\"): %v", err)
	}
}

func TestHostAPIOnDeniedForPaymentEventWithoutPaymentsCapability(t *testing.T) {
	host, err := sdk.NewHostAPI(manifestWithEvents("subscribe"), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("payment.completed", func(context.Context, any) error { return nil }); err == nil {
		t.Fatal("expected On(\"payment.completed\") to be denied without a declared payments capability")
	}
}

func TestHostAPIOnAllowedForPaymentEventWithPaymentsCapability(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{
		{Capability: "events", Scopes: []string{"subscribe"}},
		{Capability: "payments", Scopes: []string{"charge"}},
	}
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("order.placed", func(context.Context, any) error { return nil }); err != nil {
		t.Fatalf("On(\"order.placed\"): %v", err)
	}
	if err := host.On("payment.refunded", func(context.Context, any) error { return nil }); err != nil {
		t.Fatalf("On(\"payment.refunded\"): %v", err)
	}
}

func TestHostAPIOnDeniedForMembershipEventWithoutMembershipCapability(t *testing.T) {
	host, err := sdk.NewHostAPI(manifestWithEvents("subscribe"), testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("membership.started", func(context.Context, any) error { return nil }); err == nil {
		t.Fatal("expected On(\"membership.started\") to be denied without a declared membership capability")
	}
}

func TestHostAPIOnAllowedForMembershipEventWithMembershipCapability(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{
		{Capability: "events", Scopes: []string{"subscribe"}},
		{Capability: "membership", Scopes: []string{"manage"}},
	}
	host, err := sdk.NewHostAPI(m, testKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := host.On("membership.expired", func(context.Context, any) error { return nil }); err != nil {
		t.Fatalf("On(\"membership.expired\"): %v", err)
	}
}
