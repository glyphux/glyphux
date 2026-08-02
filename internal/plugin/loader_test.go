// RED tests — Ticket T6 (gap 1, plugin loader wiring). The 3-tier loader
// (internal/plugin.Loader) does not exist yet; these tests pin the contract
// in docs/specs/phase5-gap-closure-spec.md (Ticket T6) and
// docs/implementation/active/0042-phase5-plugin-loader.md, behavior-first
// against real SQLite, the committed wasm fixtures
// (pkg/runtime/wasm/testdata), and the real rpc fixtureplugin subprocess —
// no mocks anywhere.
//
// Intended loader surface these tests compile against (to be implemented in
// loader.go):
//
//	type PluginConfig struct {
//		Name     string        // must equal Manifest.Name
//		Tier     string        // "b" (wasm) | "c" (rpc subprocess)
//		Source   string        // tier b: .wasm file within Dir; tier c: executable path
//		Manifest sdk.Manifest  // declared trust surface consent is keyed on
//		Limits   wasm.Limits   // tier b: per-instance T2 limits (0 = defaults)
//	}
//	type LoadConfig struct {
//		Dir     string         // tier-b plugins dir (GLYPHUX_PLUGINS_DIR / config plugins_dir)
//		Plugins []PluginConfig
//	}
//	type Loader struct{ ... }
//	func NewLoader(deps sdk.KernelDeps, engine *consent.Engine) *Loader
//	func (l *Loader) RegisterPlugin(p sdk.Plugin) error   // tier A, T5 path unchanged
//	func (l *Loader) Load(ctx context.Context, cfg LoadConfig) error // tiers B+C, fatal-fast
//	func (l *Loader) Registered() []sdk.Plugin
//	func (l *Loader) Activate(ctx context.Context) error  // deferred until composition exists (T4)
//	func (l *Loader) Instance(name string) *wasm.Instance
//	func (l *Loader) Broker(name string) *rpc.Broker
//	func (l *Loader) HostAPI(name string) sdk.HostAPI
//	func (l *Loader) Refused() []string
//	func (l *Loader) Close() error
package plugin_test

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glyphux/glyphux/capabilities/forms"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/plugin"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/runtime/rpc"
	"github.com/glyphux/glyphux/pkg/runtime/wasm"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// wasmTestdata is the committed wasm fixture directory (the same fixtures
// pkg/runtime/wasm's own suite runs; see that package's REBUILD note).
const wasmTestdata = "../../pkg/runtime/wasm/testdata"

// --- helpers (real SQLite, real engine, real fixtures) ---

func openLoaderDB(t *testing.T, path string) *db.DB {
	t.Helper()
	d, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := d.Migrate(context.Background(), pluginstore.Migrations); err != nil {
		t.Fatalf("migrate pluginstore: %v", err)
	}
	if err := d.Migrate(context.Background(), consent.Migrations); err != nil {
		t.Fatalf("migrate consent: %v", err)
	}
	return d
}

func newEngine(t *testing.T, d *db.DB) *consent.Engine {
	t.Helper()
	return consent.NewEngine(d)
}

func loaderDeps(t *testing.T, d *db.DB) sdk.KernelDeps {
	t.Helper()
	return sdk.KernelDeps{
		KV:     pluginstore.NewStore(d),
		Bus:    sdk.NewEventBus(),
		Blocks: blocks.New(),
	}
}

// plainManifest is the shared declared trust surface for the fixtures: the
// same shape pkg/runtime/wasm's own testManifest uses (content:read is the
// routine surface a kv plugin declares; kv host funcs themselves are
// unconditional — see hostfuncs.go).
func plainManifest(name string) sdk.Manifest {
	return sdk.Manifest{
		Name:    name,
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{{Capability: "content", Scopes: []string{"read"}}},
	}
}

// decideAll records a full (approved) consent decision for each manifest —
// the install-time decision that must exist on file before the loader will
// build a host.
func decideAll(t *testing.T, e *consent.Engine, ms ...sdk.Manifest) {
	t.Helper()
	ctx := context.Background()
	for _, m := range ms {
		req, err := e.Request(m)
		if err != nil {
			t.Fatalf("request consent for %s: %v", m.Name, err)
		}
		if _, err := e.Approve(ctx, req, 42); err != nil {
			t.Fatalf("approve consent for %s: %v", m.Name, err)
		}
	}
}

// buildFixturePlugin compiles the committed rpc fixtureplugin (the same
// binary pkg/runtime/rpc's own suite uses) and returns its path.
func buildFixturePlugin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fixtureplugin")
	cmd := exec.Command("go", "build", "-o", bin, "./pkg/runtime/rpc/testdata/fixtureplugin")
	cmd.Dir = filepath.Join("..", "..") // repo root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixtureplugin: %v\n%s", err, out)
	}
	return bin
}

// --- acceptance-criterion tests ---

// TestLoaderRefusesWasmPluginWithoutLiveConsentBeforeBuildingHost pins the
// deny-by-default load gate under the owner-confirmed no-decision policy
// (CONTINUE): a wasm plugin with no consent decision on file is refused
// BEFORE any host is built — the load itself succeeds (the daemon keeps
// booting), the plugin is recorded in Refused (and logged), and the loader
// exposes neither an instance nor a host for it.
func TestLoaderRefusesWasmPluginWithoutLiveConsentBeforeBuildingHost(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })
	m := plainManifest("kv-persist-plugin") // no decision recorded

	l := plugin.NewLoader(loaderDeps(t, d), newEngine(t, d))
	if err := l.Load(ctx, plugin.LoadConfig{
		Dir: wasmTestdata,
		Plugins: []plugin.PluginConfig{
			{Name: m.Name, Tier: "b", Source: "kv_persist_guest.wasm", Manifest: m},
		},
	}); err != nil {
		t.Fatalf("refusing an unconsented plugin must not fail the load (continue policy): %v", err)
	}
	if !slices.Contains(l.Refused(), m.Name) {
		t.Errorf("plugin %q must be recorded as refused", m.Name)
	}
	if got := l.Instance(m.Name); got != nil {
		t.Error("no wasm instance may exist for an unconsented plugin")
	}
	if got := l.HostAPI(m.Name); got != nil {
		t.Error("no host may be built for an unconsented plugin")
	}
}

// TestLoaderLoadsConsentedWasmPluginAndKVSurvivesRestart is the T1-through-
// real-host-bridge proof: a wasm plugin with an approved decision loads;
// its Store().Set/Get reaches the SQL-backed pluginstore backend, and the
// guest's value survives a daemon restart onto the same SQLite file
// (kv_persist_guest.wasm: persist_set writes, persist_get reads; -2 =
// not found).
func TestLoaderLoadsConsentedWasmPluginAndKVSurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "glyphux.db")
	ctx := context.Background()
	m := plainManifest("kv-persist-plugin")

	// Phase A — first boot: consent recorded, plugin loaded, guest writes.
	d1 := openLoaderDB(t, dbPath)
	eng1 := newEngine(t, d1)
	decideAll(t, eng1, m)
	l1 := plugin.NewLoader(loaderDeps(t, d1), eng1)
	if err := l1.Load(ctx, plugin.LoadConfig{
		Dir: wasmTestdata,
		Plugins: []plugin.PluginConfig{
			{Name: m.Name, Tier: "b", Source: "kv_persist_guest.wasm", Manifest: m},
		},
	}); err != nil {
		t.Fatalf("first load: %v", err)
	}
	inst1 := l1.Instance(m.Name)
	if inst1 == nil {
		t.Fatal("loader must expose the loaded wasm instance")
	}
	if res, err := inst1.Call(ctx, "persist_set"); err != nil {
		t.Fatalf("persist_set: %v", err)
	} else if got := int32(res[0]); got != 0 {
		t.Fatalf("persist_set = %d, want 0", got)
	}
	if err := l1.Close(); err != nil {
		t.Fatalf("close loader: %v", err)
	}
	if err := d1.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	// Phase B — "restart": reopen the same file, fresh engine (decisions
	// persist), fresh loader.
	d2 := openLoaderDB(t, dbPath)
	t.Cleanup(func() { d2.Close() })
	l2 := plugin.NewLoader(loaderDeps(t, d2), newEngine(t, d2))
	if err := l2.Load(ctx, plugin.LoadConfig{
		Dir: wasmTestdata,
		Plugins: []plugin.PluginConfig{
			{Name: m.Name, Tier: "b", Source: "kv_persist_guest.wasm", Manifest: m},
		},
	}); err != nil {
		t.Fatalf("second load: %v", err)
	}
	t.Cleanup(func() { l2.Close() })
	res, err := l2.Instance(m.Name).Call(ctx, "persist_get")
	if err != nil {
		t.Fatalf("persist_get after restart: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("persist_get after restart = %d, want 1 — the guest's value must survive a restart through the pluginstore backend", got)
	}
}

// TestLoaderAppliesGrantedNetworkFilterToLoadedWasmHost pins the T3 filter
// in the loaded host: a manifest declaring network:[a.example,b.example]
// consented to only a.example must yield a host where
// AllowsNetworkHost(b.example) is false and AllowsNetworkHost(a.example) is
// true.
func TestLoaderAppliesGrantedNetworkFilterToLoadedWasmHost(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })
	eng := newEngine(t, d)

	m := sdk.Manifest{
		Name:    "net-plugin",
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		Permissions: []sdk.Permission{{Name: "network", Args: []string{"a.example", "b.example"}}},
	}
	req, err := eng.Request(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Decide(ctx, req, nil, []sdk.Permission{{Name: "network", Args: []string{"a.example"}}}, 42); err != nil {
		t.Fatal(err)
	}

	l := plugin.NewLoader(loaderDeps(t, d), eng)
	if err := l.Load(ctx, plugin.LoadConfig{
		Dir: wasmTestdata,
		Plugins: []plugin.PluginConfig{
			{Name: m.Name, Tier: "b", Source: "kv_guest.wasm", Manifest: m},
		},
	}); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	host := l.HostAPI(m.Name)
	if host == nil {
		t.Fatal("loaded plugin must expose its consent-filtered host")
	}
	if host.AllowsNetworkHost("b.example") {
		t.Error("AllowsNetworkHost(b.example) = true, want false — the T3 filter must strip unconsented hosts from the loaded host")
	}
	if !host.AllowsNetworkHost("a.example") {
		t.Error("AllowsNetworkHost(a.example) = false, want true")
	}
}

// TestLoaderEnforcesExecutionTimeoutOnRunawayWasmAndKeepsSiblingAlive pins
// the T2 limits wired per-instance: a busy-loop guest with an ExecutionTimeout
// set is reaped with ErrExecutionTimeout while a sibling instance on the
// same loader — and therefore the daemon — keeps working.
func TestLoaderEnforcesExecutionTimeoutOnRunawayWasmAndKeepsSiblingAlive(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })
	eng := newEngine(t, d)
	busy := plainManifest("busy-plugin")
	kv := plainManifest("kv-plugin")
	decideAll(t, eng, busy, kv)

	l := plugin.NewLoader(loaderDeps(t, d), eng)
	if err := l.Load(ctx, plugin.LoadConfig{
		Dir: wasmTestdata,
		Plugins: []plugin.PluginConfig{
			{Name: busy.Name, Tier: "b", Source: "busy_loop.wasm", Manifest: busy, Limits: wasm.Limits{ExecutionTimeout: 200 * time.Millisecond}},
			{Name: kv.Name, Tier: "b", Source: "kv_guest.wasm", Manifest: kv},
		},
	}); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	if _, err := l.Instance(busy.Name).Call(ctx, "spin_forever"); !errors.Is(err, wasm.ErrExecutionTimeout) {
		t.Fatalf("spin_forever error = %v, want ErrExecutionTimeout", err)
	}
	res, err := l.Instance(kv.Name).Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("sibling run_kv_roundtrip: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("sibling run_kv_roundtrip = %d, want 1 — a runaway plugin must not take down its sibling or the daemon", got)
	}
}

// TestLoaderLaunchesRpcPluginAndSupervisesKillToDead pins Tier C
// supervision: the fixtureplugin subprocess launches to StateRunning, its
// Register round-trips through the real process boundary, and killing it
// transitions to StateDead without taking down the loader (daemon).
func TestLoaderLaunchesRpcPluginAndSupervisesKillToDead(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })
	eng := newEngine(t, d)
	m := plainManifest("rpc-plugin")
	decideAll(t, eng, m)

	l := plugin.NewLoader(loaderDeps(t, d), eng)
	if err := l.Load(ctx, plugin.LoadConfig{
		Plugins: []plugin.PluginConfig{
			{Name: m.Name, Tier: "c", Source: buildFixturePlugin(t), Manifest: m},
		},
	}); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	b := l.Broker(m.Name)
	if b == nil {
		t.Fatal("loader must expose the launched rpc broker")
	}
	if got := b.State(); got != rpc.StateRunning {
		t.Fatalf("State() = %v, want StateRunning", got)
	}
	resp, err := b.Register(ctx, "store-roundtrip")
	if err != nil {
		t.Fatalf("Register(store-roundtrip): %v", err)
	}
	if !resp.GetOk() {
		t.Fatalf("Register(store-roundtrip) = %s", resp.GetError())
	}
	if err := b.Close(); err != nil {
		t.Fatalf("broker close: %v", err)
	}
	if got := b.State(); got != rpc.StateDead {
		t.Fatalf("State() after kill = %v, want StateDead — a dead subprocess must not take down the daemon", got)
	}
}

// TestLoaderInjectsGrantedNetworkAllowlistIntoRpcPluginEnv pins the T3 env
// injection for Tier C: a plugin consented to network:[a.example] must see
// exactly GLYPHUX_NETWORK_ALLOWLIST=a.example in its subprocess environment
// (fixtureplugin's "env-allowlist" mode echoes the variable it was given).
func TestLoaderInjectsGrantedNetworkAllowlistIntoRpcPluginEnv(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })
	eng := newEngine(t, d)

	m := sdk.Manifest{
		Name:    "rpc-net-plugin",
		Version: "1.0.0",
		Runtime: sdk.RuntimeRPC,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		Permissions: []sdk.Permission{{Name: "network", Args: []string{"a.example", "b.example"}}},
	}
	req, err := eng.Request(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Decide(ctx, req, nil, []sdk.Permission{{Name: "network", Args: []string{"a.example"}}}, 42); err != nil {
		t.Fatal(err)
	}

	l := plugin.NewLoader(loaderDeps(t, d), eng)
	if err := l.Load(ctx, plugin.LoadConfig{
		Plugins: []plugin.PluginConfig{
			{Name: m.Name, Tier: "c", Source: buildFixturePlugin(t), Manifest: m},
		},
	}); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { l.Close() })

	resp, err := l.Broker(m.Name).Register(ctx, "env-allowlist")
	if err != nil {
		t.Fatalf("Register(env-allowlist): %v", err)
	}
	logs := resp.GetLog()
	if len(logs) != 1 || logs[0] != "a.example" {
		t.Fatalf("GLYPHUX_NETWORK_ALLOWLIST = %v, want exactly [a.example] — the subprocess must carry only the granted hosts", logs)
	}
}

// TestLoaderFailsFastOnDuplicatePluginNames pins the fatal-fast invariant:
// two configured plugins sharing one name (across tiers) is a first-party /
// operator invariant violation the loader must reject at load, not half-boot.
func TestLoaderFailsFastOnDuplicatePluginNames(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })
	eng := newEngine(t, d)
	m := plainManifest("dup-plugin")
	decideAll(t, eng, m)

	l := plugin.NewLoader(loaderDeps(t, d), eng)
	err := l.Load(ctx, plugin.LoadConfig{
		Dir: wasmTestdata,
		Plugins: []plugin.PluginConfig{
			{Name: m.Name, Tier: "b", Source: "kv_guest.wasm", Manifest: m},
			{Name: m.Name, Tier: "c", Source: buildFixturePlugin(t), Manifest: m},
		},
	})
	if err == nil {
		t.Fatal("loader must fail fast on duplicate plugin names")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q must cite the duplicate name", err)
	}
}

// TestLoaderFailsFastOnTierMisconfiguration pins the fatal-fast tier
// validation (owner resolution #3): an unknown tier — and the reserved
// tier "a", which is first-party-only via RegisterPlugin — is an operator
// error the loader rejects at load.
func TestLoaderFailsFastOnTierMisconfiguration(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })
	m := plainManifest("mystery-plugin")

	for _, tier := range []string{"x", "a"} {
		l := plugin.NewLoader(loaderDeps(t, d), newEngine(t, d))
		err := l.Load(ctx, plugin.LoadConfig{
			Plugins: []plugin.PluginConfig{
				{Name: m.Name, Tier: tier, Source: "anything", Manifest: m},
			},
		})
		if err == nil {
			t.Errorf("tier %q: loader must fail fast on an unsupported tier", tier)
			continue
		}
		if !strings.Contains(err.Error(), "tier") {
			t.Errorf("tier %q: error %q must cite the tier", tier, err)
		}
	}
}

// TestLoaderRegistersTierAFirstPartyPluginsUnchanged pins that the T5
// registrar path survives the generalization: a first-party plugin
// registered via RegisterPlugin is part of the loader's Registered() set
// with an empty config load (tier B/C absent).
func TestLoaderRegistersTierAFirstPartyPluginsUnchanged(t *testing.T) {
	ctx := context.Background()
	d := openLoaderDB(t, filepath.Join(t.TempDir(), "glyphux.db"))
	t.Cleanup(func() { d.Close() })

	l := plugin.NewLoader(loaderDeps(t, d), newEngine(t, d))
	if err := l.RegisterPlugin(forms.New()); err != nil {
		t.Fatalf("RegisterPlugin(forms): %v", err)
	}
	if err := l.Load(ctx, plugin.LoadConfig{}); err != nil {
		t.Fatalf("load: %v", err)
	}
	found := false
	for _, p := range l.Registered() {
		if p.Manifest().Name == "forms" {
			found = true
		}
	}
	if !found {
		t.Error("tier A first-party plugin must remain in the loader's registered set")
	}
}
