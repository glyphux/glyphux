// Ticket T6 (gap 1): the 3-tier plugin loader. It generalizes the T5
// registrar into the daemon's full plugin-load path: Tier A in-process
// first-party plugins keep registering through RegisterPlugin (unchanged),
// Tier B wasm plugins load from a configured plugins dir through
// pkg/runtime/wasm with per-instance T2 Limits, and Tier C rpc plugins
// launch through pkg/runtime/rpc with T3 env injection — all consent-gated
// at load (internal/consent, deny-by-default), all sharing one durable
// pluginstore KV backend (T1) and one event bus/block registry.
//
// Policy (owner-confirmed resolutions for the T6 spec ambiguities):
//
//   - Manifest source: each PluginConfig carries the sdk.Manifest the
//     caller assembled (cmd/glyphuxd from config); interim carrier until
//     T8's .gxp/.gxt/.gxb containers ship it.
//   - No-decision/undecided/denied/stale => REFUSED: no host is built, the
//     refusal is logged, and the load continues (the daemon keeps booting).
//     Deny-by-default; never AlwaysConsent in the daemon path.
//   - Tier "a" in config is rejected (fail-fast, naming plugin + tier):
//     Tier A is first-party-only via RegisterPlugin; config tiers are b/c.
//   - Invariant violations (duplicate names, unknown tier) fail fast: an
//     operator/first-party programming bug must not half-boot the daemon.
//
// The loader never touches database/sql: KV goes through
// internal/pluginstore only (boundary invariant), and no exported signature
// leaks a raw *sql.DB or *os.File.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/runtime/rpc"
	"github.com/glyphux/glyphux/pkg/runtime/wasm"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// PluginConfig is one configured plugin the loader can materialize: Tier B
// (wasm from a .wasm file under Dir) or Tier C (an rpc subprocess binary).
// Tier A plugins never appear here — they register in-process via
// RegisterPlugin.
type PluginConfig struct {
	// Name is the plugin's identity; it must equal Manifest.Name for a
	// loadable plugin.
	Name string
	// Tier is "b" (wasm) or "c" (rpc subprocess). Anything else —
	// including "a" — is rejected at load.
	Tier string
	// Source is the tier-b .wasm filename within Dir, or the tier-c
	// executable path.
	Source string
	// Manifest is the declared trust surface consent is keyed on and the
	// T3 filter narrows to the granted subset.
	Manifest sdk.Manifest
	// Limits are the tier-b per-instance T2 resource limits (zero fields
	// fall back to pkg/runtime/wasm's documented defaults).
	Limits wasm.Limits
}

// LoadConfig is everything the loader needs for one load pass.
type LoadConfig struct {
	// Dir is the tier-b plugins directory (GLYPHUX_PLUGINS_DIR / config
	// plugins_dir). Tier-c sources are absolute paths and ignore Dir.
	Dir     string
	Plugins []PluginConfig
}

// Loader loads the daemon's configured plugins. It owns the shared
// KernelDeps every plugin host is built from (KV = the durable
// pluginstore backend, Bus, Blocks), the consent engine gating every load,
// the Tier-A registrar, and the runtime artifacts (wasm instances, rpc
// brokers) it materializes. One Loader per daemon boot; the daemon rebuilds
// the handler (and thus the loader) after the setup committer switches.
type Loader struct {
	mu      sync.Mutex
	deps    sdk.KernelDeps
	engine  *consent.Engine
	log     *slog.Logger
	reg     *Registrar   // tier A first-party (T5, unchanged)
	loaded  []sdk.Plugin // tier B/C records, for Registered()/consent adapter
	wasmRTs map[string]*wasm.Runtime
	insts   map[string]*wasm.Instance
	brokers map[string]*rpc.Broker
	hosts   map[string]sdk.HostAPI
	refused []string
}

// NewLoader builds a loader over the shared kernel deps and the consent
// engine. deps.KV should be the durable pluginstore backend (a nil KV/Bus/
// Blocks falls back to the process-lifetime defaults, as the registrar's).
func NewLoader(deps sdk.KernelDeps, engine *consent.Engine) *Loader {
	if deps.Bus == nil {
		deps.Bus = sdk.NewEventBus()
	}
	if deps.Blocks == nil {
		deps.Blocks = blocks.New()
	}
	return &Loader{
		deps:    deps,
		engine:  engine,
		log:     slog.Default(),
		reg:     New(deps),
		wasmRTs: make(map[string]*wasm.Runtime),
		insts:   make(map[string]*wasm.Instance),
		brokers: make(map[string]*rpc.Broker),
		hosts:   make(map[string]sdk.HostAPI),
	}
}

// RegisterPlugin adds a tier A (in-process first-party) plugin — the
// unchanged T5 path. Duplicate/empty names fail fast, exactly as before.
func (l *Loader) RegisterPlugin(p sdk.Plugin) error {
	return l.reg.RegisterPlugin(p)
}

// Load materializes the configured tier B/C plugins. It fails fast — before
// any side effect — on an invariant violation (unknown tier — including
// "a" — or a duplicate plugin name); a plugin whose consent is not live is
// refused (logged, recorded in Refused) and the load continues, so the
// daemon never half-boots on an operator mistake and never fails to boot on
// an unconsented plugin.
func (l *Loader) Load(ctx context.Context, cfg LoadConfig) error {
	// Pass 1 — invariant validation, fatal-fast, before any plugin loads.
	seen := make(map[string]bool, len(cfg.Plugins))
	for _, pc := range cfg.Plugins {
		if pc.Tier != "b" && pc.Tier != "c" {
			return fmt.Errorf("plugin %q: unsupported tier %q (want \"b\" wasm or \"c\" rpc; tier \"a\" is first-party-only via RegisterPlugin)", pc.Name, pc.Tier)
		}
		if seen[pc.Name] {
			return fmt.Errorf("plugin %q: duplicate plugin name across configured entries", pc.Name)
		}
		seen[pc.Name] = true
	}

	// Pass 2 — load each configured plugin; refusals are recorded, not fatal.
	l.mu.Lock()
	l.refused = nil
	l.mu.Unlock()
	for _, pc := range cfg.Plugins {
		var err error
		switch pc.Tier {
		case "b":
			err = l.loadWasm(ctx, cfg.Dir, pc)
		case "c":
			err = l.loadRPC(ctx, pc)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Registered returns every loaded plugin — tier A first-party (registration
// order) then tier B/C — the consent adapter and the API surface snapshot
// from.
func (l *Loader) Registered() []sdk.Plugin {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := append([]sdk.Plugin(nil), l.reg.Registered()...)
	return append(out, l.loaded...)
}

// Activate runs every tier A plugin's Register against its host — the T5
// activation, deferred on a virgin install until the setup committer writes
// the initial composition (tier B/C register through their own transport at
// load and define no content types — documented scope).
func (l *Loader) Activate(ctx context.Context) error {
	return l.reg.Activate(ctx)
}

// Instance returns the loaded tier-b wasm instance for name, or nil.
func (l *Loader) Instance(name string) *wasm.Instance {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.insts[name]
}

// Broker returns the launched tier-c broker for name, or nil.
func (l *Loader) Broker(name string) *rpc.Broker {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.brokers[name]
}

// HostAPI returns the consent-filtered host built for name (tier b or c),
// or nil for a plugin that was refused or never configured.
func (l *Loader) HostAPI(name string) sdk.HostAPI {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.hosts[name]
}

// Refused returns the names of plugins refused on the last Load (deny-by-
// default: no live consent decision on file). Nil when nothing was refused.
func (l *Loader) Refused() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.refused...)
}

// Close tears down every loaded artifact — rpc brokers (subprocess killed,
// supervision transitions them to dead) and wasm runtimes/instances.
func (l *Loader) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	ctx := context.Background()
	var errs []error
	for name, b := range l.brokers {
		if err := b.Close(); err != nil {
			errs = append(errs, fmt.Errorf("plugin %q: close broker: %w", name, err))
		}
	}
	for name, inst := range l.insts {
		if err := inst.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("plugin %q: close instance: %w", name, err))
		}
	}
	for name, rt := range l.wasmRTs {
		if err := rt.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("plugin %q: close runtime: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// loadedPlugin is the tier B/C registration record: the manifest every
// other surface (consent adapter, GET /api/v0/plugins) sees. Register is a
// no-op because tier B/C plugins register through their own transport at
// load time (wasm guest exports / the rpc Register round-trip), not through
// the sdk.Plugin Go interface — and tier B cannot register content types
// (documented scope).
type loadedPlugin struct {
	m sdk.Manifest
}

func (p loadedPlugin) Manifest() sdk.Manifest     { return p.m }
func (p loadedPlugin) Register(sdk.HostAPI) error { return nil }

// consentFor resolves the plugin's live decision. ok=false means the plugin
// is refused (deny-by-default): no manifest, no decision on file, a denied
// decision, or a stale fingerprint (the manifest's consent axes changed
// since the decision was recorded). Refusal happens before any host is
// built.
func (l *Loader) consentFor(ctx context.Context, m sdk.Manifest) (consent.Decision, bool) {
	if l.engine == nil || m.Name == "" {
		return consent.Decision{}, false
	}
	d, live, err := l.engine.IsConsented(ctx, m)
	if err != nil || !live {
		return consent.Decision{}, false
	}
	return d, true
}

// refuse records the plugin as refused and logs why. Refusal never fails
// the load: the daemon boots and keeps serving everything that did load.
func (l *Loader) refuse(pc PluginConfig, reason string) {
	l.mu.Lock()
	l.refused = append(l.refused, pc.Name)
	l.mu.Unlock()
	l.log.Warn("plugin refused", "plugin", pc.Name, "reason", reason)
}

// loadWasm materializes a tier-b plugin: consent-gated, host built from the
// T3-filtered manifest (granted subset), wasm loaded with the plugin's T2
// Limits, and the durable pluginstore KV already behind KernelDeps.KV.
func (l *Loader) loadWasm(ctx context.Context, dir string, pc PluginConfig) error {
	decision, ok := l.consentFor(ctx, pc.Manifest)
	if !ok {
		l.refuse(pc, "no live consent decision on file (deny-by-default)")
		return nil
	}
	if pc.Manifest.Name != pc.Name {
		return fmt.Errorf("plugin %q: manifest name %q does not match the configured name", pc.Name, pc.Manifest.Name)
	}
	filtered := sdk.FilterManifest(pc.Manifest, decision.GrantedPermissions)
	host, err := sdk.NewHostAPI(filtered, l.deps)
	if err != nil {
		return fmt.Errorf("plugin %q: build host: %w", pc.Name, err)
	}
	code, err := os.ReadFile(filepath.Join(dir, pc.Source))
	if err != nil {
		return fmt.Errorf("plugin %q: read wasm: %w", pc.Name, err)
	}

	// Record before building the consent adapter so the adapter's manifest
	// snapshot (keyed by name) includes this plugin.
	l.mu.Lock()
	l.loaded = append(l.loaded, loadedPlugin{m: pc.Manifest})
	l.mu.Unlock()
	checker := NewConsentAdapter(l.engine, l.Registered())

	rt := wasm.New(ctx, checker, wasm.WithLimits(pc.Limits))
	inst, err := rt.Load(ctx, filtered, host, code)
	if err != nil {
		_ = rt.Close(ctx)
		return fmt.Errorf("plugin %q: load wasm: %w", pc.Name, err)
	}
	l.mu.Lock()
	l.wasmRTs[pc.Name] = rt
	l.insts[pc.Name] = inst
	l.hosts[pc.Name] = host
	l.mu.Unlock()
	l.log.Info("plugin loaded", "plugin", pc.Name, "tier", "b")
	return nil
}

// loadRPC materializes a tier-c plugin: consent-gated, host built from the
// T3-filtered manifest, and a supervised subprocess broker launched with the
// granted network allowlist injected into its environment.
func (l *Loader) loadRPC(ctx context.Context, pc PluginConfig) error {
	decision, ok := l.consentFor(ctx, pc.Manifest)
	if !ok {
		l.refuse(pc, "no live consent decision on file (deny-by-default)")
		return nil
	}
	if pc.Manifest.Name != pc.Name {
		return fmt.Errorf("plugin %q: manifest name %q does not match the configured name", pc.Name, pc.Manifest.Name)
	}
	filtered := sdk.FilterManifest(pc.Manifest, decision.GrantedPermissions)
	host, err := sdk.NewHostAPI(filtered, l.deps)
	if err != nil {
		return fmt.Errorf("plugin %q: build host: %w", pc.Name, err)
	}
	var allowlist []string
	for _, perm := range filtered.Permissions {
		if perm.Name == "network" {
			allowlist = perm.Args
		}
	}

	b, err := rpc.Launch(rpc.Config{
		Command:          pc.Source,
		HostAPI:          host,
		NetworkAllowlist: allowlist,
	})
	if err != nil {
		return fmt.Errorf("plugin %q: launch rpc broker: %w", pc.Name, err)
	}
	l.mu.Lock()
	l.brokers[pc.Name] = b
	l.hosts[pc.Name] = host
	l.loaded = append(l.loaded, loadedPlugin{m: pc.Manifest})
	l.mu.Unlock()
	l.log.Info("plugin loaded", "plugin", pc.Name, "tier", "c")
	return nil
}
