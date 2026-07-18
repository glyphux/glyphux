package sdk_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
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

func TestHostAPIOnAndEmitAreCallable(t *testing.T) {
	host, err := sdk.NewHostAPI(validManifest(), testKernel(t))
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
