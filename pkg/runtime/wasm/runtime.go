// Package wasm is Tier B of the PRD's three-tier plugin system (§8.1): a
// Wazero-hosted (pure Go, no CGO — github.com/tetratelabs/wazero) sandbox
// for "small, numerous, untrusted, latency-sensitive plugins." It exposes a
// guest WASM module ONLY the host functions backed by capabilities the
// plugin's manifest declared (PRD §10.3: "capabilities granted = host
// functions exposed") by bridging into a real pkg/sdk.HostAPI instance —
// never a stub or a Go-struct-only simulation of what a call might do.
//
// Deferred out of this slice (see docs/implementation/active/0015-*.md):
// the real install-time consent engine (slice 2.7 — this package only
// leaves the ConsentChecker seam it plugs into), a formal WIT contract
// (this package hand-rolls its own minimal host-function ABI instead), and
// resource limits beyond whatever wazero itself provides by default (no
// fuel/gas metering, no explicit memory cap, no execution timeout wiring).
package wasm

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// Runtime is a factory for loading plugin guest modules. Every capability's
// host module a guest imports from is conventionally named "env" (matching
// wasm32-unknown-unknown's default `#[link(wasm_import_module = "env")]`
// convention — see testdata/*.rs), and Wazero requires module names to be
// unique within one wazero.Runtime's namespace. Since every Tier-B plugin
// needs its OWN "env" bound to its OWN capability set and its OWN
// sdk.HostAPI, Load gives each loaded Instance a private child
// wazero.Runtime rather than sharing one across plugins — this is also what
// makes plugin isolation strictly per-instance rather than merely
// per-module: a trap in one plugin cannot touch a sibling plugin's runtime
// state at all, not just its linear memory (see runtime_test.go's
// TestGuestTrapDoesNotCrashHostOrSiblingInstance). A shared
// wazero.CompilationCache means re-loading the same guest bytecode across
// multiple Instances still reuses compiled code instead of recompiling.
type Runtime struct {
	cache   wazero.CompilationCache
	consent ConsentChecker
}

// New returns a Runtime, using checker to decide install-time consent for
// each capability a guest's host module exposes (see ConsentChecker's doc
// comment on why this is a seam, not a real consent decision, in this
// slice). Pass AlwaysConsent{} for the slice-2.4 default behavior (declared
// == consented).
func New(ctx context.Context, checker ConsentChecker) *Runtime {
	return &Runtime{cache: wazero.NewCompilationCache(), consent: checker}
}

// Close releases the shared compilation cache. It does not close any
// Instance still loaded — each Instance owns its own child engine and must
// be closed independently via Instance.Close.
func (r *Runtime) Close(ctx context.Context) error {
	return r.cache.Close(ctx)
}

// Instance is one loaded, running guest WASM module — a single plugin's
// live Tier-B execution context, with its own private Wazero engine (see
// Runtime's doc comment for why).
type Instance struct {
	wz       wazero.Runtime
	mod      api.Module
	manifest sdk.Manifest
	host     sdk.HostAPI
}

// Load compiles code and instantiates it against a host module built from
// manifest's declared capabilities and host's real backing (see
// buildHostModule in hostfuncs.go). If code's own import section requires a
// host function this manifest did not declare (and consent did not grant),
// that function is simply absent from the host module Wazero links against,
// so instantiation fails with a genuine Wazero link error — deny-by-default
// enforced by the WASM boundary itself, not by a runtime check the guest
// could route around (PRD §10.3).
func (r *Runtime) Load(ctx context.Context, manifest sdk.Manifest, host sdk.HostAPI, code []byte) (*Instance, error) {
	cfg := wazero.NewRuntimeConfig().WithCompilationCache(r.cache)
	wz := wazero.NewRuntimeWithConfig(ctx, cfg)

	hostBuilder := buildHostModule(wz, manifest, host, r.consent)
	if _, err := hostBuilder.Instantiate(ctx); err != nil {
		wz.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiate host module for plugin %q: %w", manifest.Name, err)
	}

	compiled, err := wz.CompileModule(ctx, code)
	if err != nil {
		wz.Close(ctx)
		return nil, fmt.Errorf("wasm: compile guest module for plugin %q: %w", manifest.Name, err)
	}

	modCfg := wazero.NewModuleConfig().WithName(manifest.Name)
	mod, err := wz.InstantiateModule(ctx, compiled, modCfg)
	if err != nil {
		wz.Close(ctx)
		return nil, fmt.Errorf("wasm: instantiate guest module for plugin %q: %w", manifest.Name, err)
	}

	return &Instance{wz: wz, mod: mod, manifest: manifest, host: host}, nil
}

// Close tears down this instance's private engine (and, with it, its guest
// module); sibling instances loaded from the same Runtime are entirely
// unaffected — they do not share an engine at all (see Runtime's doc
// comment).
func (i *Instance) Close(ctx context.Context) error {
	return i.wz.Close(ctx)
}

// Call invokes the guest's exported function fn with params, returning its
// raw i64-encoded result stack exactly as Wazero returns it (the guest
// fixtures in testdata/ export plain i32-returning functions, so callers in
// this package's tests read result[0] as a uint32/int32). A guest trap
// (e.g. testdata/kv_guest.rs's trigger_trap, or an out-of-bounds guest
// memory access) surfaces here as a non-nil error — it does not panic or
// otherwise affect the calling goroutine or Runtime.
func (i *Instance) Call(ctx context.Context, fn string, params ...uint64) ([]uint64, error) {
	exported := i.mod.ExportedFunction(fn)
	if exported == nil {
		return nil, fmt.Errorf("wasm: guest module %q exports no function %q", i.manifest.Name, fn)
	}
	return exported.Call(ctx, params...)
}

// Subscribe wires event on the shared cross-plugin event bus (via
// i.host.On — pkg/sdk's real EventBus, slice 2.2) to a callback into this
// guest instance: on each emitted payload, Subscribe writes the payload
// bytes into the guest's own exported event buffer (its exported
// event_buf_ptr() function reports where) and calls its exported
// on_event(len) function so the guest observes the event.
//
// This goes through i.host.On unmodified, so every gate pkg/sdk itself
// already enforces (events:subscribe, plus any sensitive-event capability —
// see host.go's sensitiveEvents) applies exactly as it does for a Tier-A
// in-process plugin; Subscribe adds no separate WASM-side capability check
// of its own; it is a real, undiminished forward of that same rejection
// (see runtime_test.go's TestSubscribeDeniedWithoutDeclaredCapability).
//
// The guest must export event_buf_ptr() (returns i32 pointer) and
// on_event(len i32) (i32) — see testdata/events_guest.rs.
func (i *Instance) Subscribe(event string) error {
	bufPtrFn := i.mod.ExportedFunction("event_buf_ptr")
	onEventFn := i.mod.ExportedFunction("on_event")
	if bufPtrFn == nil || onEventFn == nil {
		return fmt.Errorf("wasm: guest module %q does not export event_buf_ptr/on_event", i.manifest.Name)
	}
	return i.host.On(event, func(ctx context.Context, payload any) error {
		data, _ := payload.([]byte)
		res, err := bufPtrFn.Call(ctx)
		if err != nil {
			return err
		}
		ptr := uint32(res[0])
		if len(data) > 0 && !i.mod.Memory().Write(ptr, data) {
			return fmt.Errorf("wasm: guest module %q event buffer too small for payload", i.manifest.Name)
		}
		_, err = onEventFn.Call(ctx, uint64(len(data)))
		return err
	})
}
