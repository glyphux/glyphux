package plugin_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/capabilities/forms"
	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/plugin"
	"github.com/glyphux/glyphux/pkg/runtime/wasm"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// consentDB opens a real SQLite database migrated with the consent engine's
// own schema (consent_decisions, version 14) plus the audit table (version
// 15) — the same two migrations cmd/glyphuxd appends to its daemon list.
func consentDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "consent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), consent.Migrations); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background(), audit.Migrations); err != nil {
		t.Fatal(err)
	}
	return d
}

// staticPlugin wraps a fixed manifest in the sdk.Plugin shape so the
// adapter (and registrar) can consume it.
type staticPlugin struct{ m sdk.Manifest }

func (p staticPlugin) Manifest() sdk.Manifest     { return p.m }
func (p staticPlugin) Register(sdk.HostAPI) error { return nil }

// commerceManifest mirrors capabilities/commerce's declared shape (gap 2's
// spec example: content[read,write] + payments[charge,refund] + admin_ui).
func commerceManifest() sdk.Manifest {
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
			{Capability: "payments", Scopes: []string{"charge", "refund"}},
		},
		Permissions: []sdk.Permission{{Name: "admin_ui"}},
	}
}

func TestAdapterUndecidedPluginIsNotConsented(t *testing.T) {
	ctx := context.Background()
	engine := consent.NewEngine(consentDB(t))
	adapter := plugin.NewConsentAdapter(engine, []sdk.Plugin{staticPlugin{commerceManifest()}})

	// Undecided manifest: IsConsented reports no live decision...
	if _, ok, err := engine.IsConsented(ctx, commerceManifest()); err != nil || ok {
		t.Fatalf("IsConsented(undecided) = ok=%v err=%v, want false", ok, err)
	}
	// ...and the wasm seam refuses even a DECLARED capability.
	if adapter.Consented("commerce", "content") {
		t.Fatal("Consented(undecided) = true, want false — plugin must be refused at load")
	}
	if adapter.Consented("commerce", "payments") {
		t.Fatal("Consented(undecided) = true, want false")
	}
	// Unknown plugin name is refused too.
	if adapter.Consented("nope", "content") {
		t.Fatal("Consented(unknown plugin) = true, want false")
	}
}

// TestAdapterStaleConsentRequiresReconsent is the fingerprint path
// end-to-end: approve the v1 manifest, then the plugin's API axis gains a
// scope at the SAME version — the old decision no longer matches the new
// fingerprint, so IsConsented is false and the adapter refuses the plugin.
func TestAdapterStaleConsentRequiresReconsent(t *testing.T) {
	ctx := context.Background()
	d := consentDB(t)
	engine := consent.NewEngine(d)
	adapter := plugin.NewConsentAdapter(engine, []sdk.Plugin{staticPlugin{commerceManifest()}})

	req, err := engine.Request(commerceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Approve(ctx, req, 1); err != nil {
		t.Fatal(err)
	}
	if ok := adapter.Consented("commerce", "payments"); !ok {
		t.Fatal("after full approve, payments must be consented")
	}

	// Same version, one added scope on the API axis.
	changed := commerceManifest()
	changed.API = append(changed.API, sdk.APIScope{Capability: "users", Scopes: []string{"manage"}})
	_, ok, err := engine.IsConsented(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("stale consent must not cover a manifest whose API axis gained a scope at the same version")
	}
	stale := plugin.NewConsentAdapter(engine, []sdk.Plugin{staticPlugin{changed}})
	if stale.Consented("commerce", "content") {
		t.Fatal("stale consent must refuse the changed manifest — re-consent required")
	}

	// Re-consent on the new shape restores it.
	req2, err := engine.Request(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Approve(ctx, req2, 1); err != nil {
		t.Fatal(err)
	}
	_, live, err := engine.IsConsented(ctx, changed)
	if err != nil || !live {
		t.Fatal("re-consented manifest must be consented again")
	}
}

// TestAdapterPartialGrantGrantsExactlyTheSubset drives the spec's commerce
// scenario: an admin grants content:read only. Decision.Status == partial,
// IsConsented exposes exactly that subset, and the adapter's two seams map
// it correctly (capability-level for wasm, scope-level for rpc).
func TestAdapterPartialGrantGrantsExactlyTheSubset(t *testing.T) {
	ctx := context.Background()
	engine := consent.NewEngine(consentDB(t))
	m := commerceManifest()
	req, err := engine.Request(m)
	if err != nil {
		t.Fatal(err)
	}
	granted := []sdk.APIScope{{Capability: "content", Scopes: []string{"read"}}}
	dec, err := engine.Decide(ctx, req, granted, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Status != consent.StatusPartial {
		t.Fatalf("Status = %q, want partial", dec.Status)
	}

	live, ok, err := engine.IsConsented(ctx, m)
	if err != nil || !ok {
		t.Fatalf("IsConsented after partial grant = ok=%v err=%v", ok, err)
	}
	if len(live.GrantedAPI) != 1 || live.GrantedAPI[0].Capability != "content" || len(live.GrantedAPI[0].Scopes) != 1 || live.GrantedAPI[0].Scopes[0] != "read" {
		t.Fatalf("GrantedAPI = %+v, want exactly content:[read]", live.GrantedAPI)
	}

	adapter := plugin.NewConsentAdapter(engine, []sdk.Plugin{staticPlugin{m}})
	if !adapter.Consented("commerce", "content") {
		t.Error("wasm seam: content must be consented")
	}
	if adapter.Consented("commerce", "payments") {
		t.Error("wasm seam: payments must NOT be consented under a partial grant")
	}
	if !adapter.Allowed("commerce", "content", "read") {
		t.Error("rpc seam: content/read must be allowed")
	}
	if adapter.Allowed("commerce", "content", "write") {
		t.Error("rpc seam: content/write must NOT be allowed")
	}
	if adapter.Allowed("commerce", "payments", "charge") {
		t.Error("rpc seam: payments/charge must NOT be allowed")
	}
}

// TestAdapterWithAuditLogsDecision proves the daemon's engine wiring:
// WithAudit(logger) produces an audit_records row (action consent.decide);
// without a logger the decision still persists and nothing panics.
func TestAdapterWithAuditLogsDecision(t *testing.T) {
	ctx := context.Background()
	d := consentDB(t)

	// With logger.
	audited := consent.NewEngine(d, consent.WithAudit(audit.NewLogger(d)))
	req, err := audited.Request(commerceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audited.Approve(ctx, req, 7); err != nil {
		t.Fatal(err)
	}
	logger := audit.NewLogger(d)
	rows, err := logger.ListByPlugin(ctx, "commerce")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Action != "consent.decide" || !rows[0].Allowed {
		t.Fatalf("audit rows = %+v, want one consent.decide allowed row", rows)
	}

	// Without logger: decision persists, no audit row, no panic.
	plain := consent.NewEngine(d)
	req2, err := plain.Request(commerceManifest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Deny(ctx, req2, 7); err != nil {
		t.Fatal(err)
	}
	rows, err = logger.ListByPlugin(ctx, "commerce")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows after un-audited deny = %d, want still 1 (deny must not be audited without a logger)", len(rows))
	}
	_, live, _ := plain.IsConsented(ctx, commerceManifest())
	if live {
		t.Fatal("denied decision must not consent")
	}
}

// TestAdapterDeniesWasmSeamForDeclaredButUnconsentedCapability is the
// acceptance criterion "wasm plugin declaring events:emit but unconsented":
// the events_guest fixture imports ONLY env.emit_event, so when the adapter
// refuses the capability the module fails to instantiate (the host function
// is absent — deny-by-absence), and after a full consent it loads and runs.
func TestAdapterDeniesWasmSeamForDeclaredButUnconsentedCapability(t *testing.T) {
	ctx := context.Background()
	engine := consent.NewEngine(consentDB(t))
	m := sdk.Manifest{
		Name:    "events-plugin",
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{{Capability: "events", Scopes: []string{"emit"}}},
	}
	host, err := sdk.NewHostAPI(m, sdk.KernelDeps{KV: sdk.NewMemoryKVBackend(), Bus: sdk.NewEventBus()})
	if err != nil {
		t.Fatal(err)
	}
	code := readWasmFixture(t)

	refused := plugin.NewConsentAdapter(engine, []sdk.Plugin{staticPlugin{m}})
	rt := wasm.New(ctx, refused)
	if _, err := rt.Load(ctx, m, host, code); err == nil {
		t.Fatal("Load must fail when events:emit is declared but unconsented (emit_event absent)")
	}

	// Positive control: full consent, fresh adapter, same runtime shape.
	req, err := engine.Request(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Approve(ctx, req, 1); err != nil {
		t.Fatal(err)
	}
	approved := plugin.NewConsentAdapter(engine, []sdk.Plugin{staticPlugin{m}})
	if !approved.Consented("events-plugin", "events") {
		t.Fatal("approved events:emit must consent")
	}
	rt2 := wasm.New(ctx, approved)
	inst, err := rt2.Load(ctx, m, host, code)
	if err != nil {
		t.Fatalf("Load after consent = %v, want success", err)
	}
	if _, err := inst.Call(ctx, "run_emit"); err != nil {
		t.Fatalf("Call run_emit after consent = %v, want success", err)
	}
}

// TestAdapterDrivesRegistrarRegisteredPlugins proves the adapter integrates
// with the T5 registrar's plugin set: the daemon builds the adapter from
// capReg.Registered(), every registered plugin starts unconsented, and a
// full engine approve of one plugin's request flips only that plugin.
func TestAdapterDrivesRegistrarRegisteredPlugins(t *testing.T) {
	ctx := context.Background()
	deps, _ := realDeps(t)
	reg := plugin.New(deps)
	if err := reg.RegisterPlugin(forms.New()); err != nil {
		t.Fatal(err)
	}
	reg2 := reg // reuse the same registrar below
	_ = reg2

	engine := consent.NewEngine(consentDB(t))
	adapter := plugin.NewConsentAdapter(engine, reg.Registered())
	if adapter.Consented("forms", "content") {
		t.Fatal("freshly-registered forms must start unconsented")
	}

	req, err := engine.Request(forms.New().Manifest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Approve(ctx, req, 1); err != nil {
		t.Fatal(err)
	}
	approved := plugin.NewConsentAdapter(engine, reg.Registered())
	if !approved.Consented("forms", "content") {
		t.Fatal("approved forms must be consented")
	}
}
