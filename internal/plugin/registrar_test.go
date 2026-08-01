package plugin_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/capabilities/commerce"
	"github.com/glyphux/glyphux/capabilities/forms"
	"github.com/glyphux/glyphux/capabilities/membership"
	"github.com/glyphux/glyphux/capabilities/notifications"
	"github.com/glyphux/glyphux/capabilities/seo"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/plugin"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// realDeps builds the shared KernelDeps a running daemon would hand the
// registrar: real SQLite-backed domain stores, a shared blocks registry,
// and the process-lifetime defaults for Bus/KV (the same shape
// cmd/glyphuxd's buildFullHandler constructs — Ticket T5).
func realDeps(t *testing.T) (sdk.KernelDeps, *db.DB) {
	t.Helper()
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, media.Migrations...)
	if err := d.Migrate(ctx, migs); err != nil {
		t.Fatal(err)
	}

	comps := composition.NewStore(d)
	if err := comps.Save(ctx, nil, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}
	return sdk.KernelDeps{
		Compositions: comps,
		Content:      content.NewAPI(comps, content.NewStore(d)),
		Media:        media.NewAPI(media.NewStore(d), t.TempDir()),
		Identities:   identity.NewService(d),
		Blocks:       blocks.New(),
	}, d
}

// fiveFirstParty returns the five first-party capability plugins as the
// daemon registers them (Ticket T5): commerce/membership with nil gateways
// (no payment processor configured — their manifests then carry no network
// permission), notifications with the documented memory mailer placeholder.
func fiveFirstParty() []sdk.Plugin {
	return []sdk.Plugin{
		forms.New(),
		seo.New(),
		commerce.New(nil),
		membership.New(nil),
		notifications.New(notifications.NewMemoryMailerAdapter()),
	}
}

// --- Behavior: the five first-party capabilities register and activate
// against real stores; their content types land in the composition. ---

func TestRegistrarActivatesFiveFirstPartyCapabilities(t *testing.T) {
	ctx := context.Background()
	deps, d := realDeps(t)
	_ = d

	reg := plugin.New(deps)
	for _, p := range fiveFirstParty() {
		if err := reg.RegisterPlugin(p); err != nil {
			t.Fatalf("RegisterPlugin(%s): %v", p.Manifest().Name, err)
		}
	}
	if got := len(reg.Registered()); got != 5 {
		t.Fatalf("Registered() = %d plugins, want 5", got)
	}
	if err := reg.Activate(ctx); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	comp, err := deps.Compositions.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"form_submission",    // forms
		"product",            // commerce
		"order",              // commerce
		"membership_tier",    // membership
		"membership_subscription", // membership
	} {
		if _, ok := comp.ContentTypes[want]; !ok {
			t.Errorf("composition content_types missing %q (got %v)", want, keys(comp.ContentTypes))
		}
	}
}

func keys(m map[string]contract.ContentType) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- Fatal-fast invariants (matching blocks/firstparty.RegisterAll's
// duplicate/empty rejection): the registrar must refuse an empty plugin
// name, a duplicate name, and an invalid manifest — each before any
// registration side effect. ---

func TestRegistrarRejectsEmptyPluginName(t *testing.T) {
	deps, _ := realDeps(t)
	reg := plugin.New(deps)
	if err := reg.RegisterPlugin(emptyNamePlugin{}); err == nil {
		t.Fatal("expected empty plugin name to be rejected")
	}
}

func TestRegistrarRejectsDuplicatePluginName(t *testing.T) {
	deps, _ := realDeps(t)
	reg := plugin.New(deps)
	if err := reg.RegisterPlugin(forms.New()); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterPlugin(forms.New()); err == nil {
		t.Fatal("expected duplicate plugin name to be rejected")
	}
}

func TestRegistrarRejectsInvalidManifest(t *testing.T) {
	deps, _ := realDeps(t)
	reg := plugin.New(deps)
	if err := reg.RegisterPlugin(invalidManifestPlugin{}); err == nil {
		t.Fatal("expected an invalid manifest to be rejected at registration")
	}
}

// --- Activation propagates a Register failure (no silent half-boot). ---

func TestRegistrarActivatePropagatesRegisterError(t *testing.T) {
	ctx := context.Background()
	deps, _ := realDeps(t)
	reg := plugin.New(deps)
	if err := reg.RegisterPlugin(failingRegisterPlugin{}); err != nil {
		t.Fatal(err)
	}
	err := reg.Activate(ctx)
	if err == nil {
		t.Fatal("expected Activate to propagate the plugin's Register error")
	}
	if !errors.Is(err, errFakeRegisterFailure) {
		t.Fatalf("Activate error = %v, want wrapping errFakeRegisterFailure", err)
	}
}

// --- Shared defaults: a registrar given a KernelDeps without Bus/KV gets
// a single shared EventBus and MemoryKVBackend every plugin's host sees
// (namespaced per plugin), so cross-plugin events/KV are real. ---

func TestRegistrarDefaultsSharedBusAndKV(t *testing.T) {
	deps, _ := realDeps(t)
	reg := plugin.New(deps)
	if reg.Bus() == nil {
		t.Fatal("registrar must own a shared EventBus when deps.Bus is nil")
	}
	if reg.KV() == nil {
		t.Fatal("registrar must own a shared MemoryKVBackend when deps.KV is nil")
	}
}

// --- Fakes ---

type emptyNamePlugin struct{}

func (emptyNamePlugin) Manifest() sdk.Manifest {
	m := validPluginManifest()
	m.Name = ""
	return m
}
func (emptyNamePlugin) Register(sdk.HostAPI) error { return nil }

type invalidManifestPlugin struct{}

func (invalidManifestPlugin) Manifest() sdk.Manifest {
	m := validPluginManifest()
	m.Version = "not-a-version"
	return m
}
func (invalidManifestPlugin) Register(sdk.HostAPI) error { return nil }

var errFakeRegisterFailure = errors.New("plugin: fake register failure")

type failingRegisterPlugin struct{}

func (failingRegisterPlugin) Manifest() sdk.Manifest { return validPluginManifest() }
func (failingRegisterPlugin) Register(sdk.HostAPI) error {
	return errFakeRegisterFailure
}

func validPluginManifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    "fake-plugin",
		Version: "1.0.0",
		Runtime: sdk.RuntimeInProcess,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
	}
}
