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
// leaves the ConsentChecker seam it plugs into) and a formal WIT contract
// (this package hand-rolls its own minimal host-function ABI instead).
// Resource limits (gap 7 / Ticket T2) are IN: every Instance enforces a
// memory cap, an execution timeout, and (when configured) a fuel budget,
// via wazero's WithMemoryLimitPages/WithCloseOnContextDone plus per-Call
// context deadlines (see Limits and Call).
package wasm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// Default resource limits (gap 7 / Ticket T2), applied per field when a
// Limits value leaves that field at its zero value:
//
//   - defaultMaxMemoryPages: 1024 pages (~64 MiB) — wazero's own default is
//     65536 pages (~4 GiB), far too generous for "small, numerous,
//     untrusted" Tier-B plugins (PRD §8.1).
//   - defaultExecutionTimeout: 30s per Call — a plugin that hangs must be
//     reaped; 30s is generous enough that no legitimate guest today (kv,
//     events) gets anywhere near it.
//   - fuel default 0 = unlimited (see Limits.Fuel for why).
//
// These are documented judgment calls (see docs/implementation/active/
// 0038-*.md) and deliberately non-breaking: a Runtime built with no options
// behaves identically to pre-T2 for every existing guest.
const (
	defaultMaxMemoryPages   = 1024
	defaultExecutionTimeout = 30 * time.Second
)

// ErrExecutionTimeout is returned (wrapped) by Instance.Call when the call
// exceeded the instance's ExecutionTimeout — the guest hung past its
// deadline and was interrupted mid-execution.
var ErrExecutionTimeout = errors.New("wasm: execution timeout")

// ErrFuelExhausted is returned (wrapped) by Instance.Call when the call
// burned through the instance's Fuel budget — the fuel deadline tripped
// before the execution timeout did.
var ErrFuelExhausted = errors.New("wasm: fuel exhausted")

// Limits bounds one WASM instance's resource usage (gap 7 / Ticket T2).
// Every field is "0 = unset": a zero field falls back to the documented
// default (defaultMaxMemoryPages / defaultExecutionTimeout / fuel
// unlimited). Limits are enforced PER INSTANCE — each Instance has its own
// private wazero engine (see Runtime's doc comment), so one instance
// exhausting fuel, timing out, or hitting the memory cap cannot touch a
// sibling instance's budget.
//
// Fuel: wazero v1.12 (the version this package builds on) has no
// instruction-level gas metering — there is no WithFuel option and no
// per-instruction accounting anywhere in its public API (verified against
// the vendored source; the only in-flight interruption hook is
// WithCloseOnContextDone). Fuel is therefore implemented as a deterministic
// per-Call WALL-CLOCK COMPUTE budget: each Fuel unit is one nanosecond of
// execution time (0 = unlimited), enforced as a per-Call context deadline
// that is STRICTER than ExecutionTimeout. It is a wall-clock deadline, NOT
// a count of guest instructions — the guest's actual compute per unit
// varies with host speed, so fuel bounds "how long may this call burn CPU"
// deterministically (fixed budget per call, whichever deadline trips
// first), which is what the resource-limit tests actually assert (e.g.
// fuel_burn's 300:1 budget margin). Whichever deadline fires earlier wins;
// the error reported is the one whose deadline fired (ErrFuelExhausted vs
// ErrExecutionTimeout), so the two constraints stay distinguishable and
// compose as "whichever trips first" (see Instance.Call).
type Limits struct {
	// MaxMemoryPages caps the instance's linear memory (default 1024 =
	// ~64 MiB). Enforced by wazero WithMemoryLimitPages at instantiation
	// and on every guest memory.grow: growth past the cap fails (the
	// guest sees -1), it never crashes the host.
	MaxMemoryPages uint32
	// Fuel is the per-Call compute budget in nanoseconds (0 = unlimited).
	Fuel uint64
	// ExecutionTimeout bounds each Call's wall-clock duration (default
	// 30s; 0 = default).
	ExecutionTimeout time.Duration
}

// resolve returns the effective limits with per-field defaults applied.
func (l Limits) resolve() Limits {
	if l.MaxMemoryPages == 0 {
		l.MaxMemoryPages = defaultMaxMemoryPages
	}
	if l.ExecutionTimeout == 0 {
		l.ExecutionTimeout = defaultExecutionTimeout
	}
	// Fuel 0 stays 0 = unlimited.
	return l
}

// Option configures a Runtime at construction (the same functional-options
// shape internal/api uses: type Option func(*Server) + WithXxx returning
// it). Pass to New; a Runtime built with no options gets the documented
// defaults.
type Option func(*Runtime)

// WithLimits sets the resource limits every Instance loaded from this
// Runtime enforces. Fields left at zero fall back to the defaults (see
// Limits).
func WithLimits(l Limits) Option {
	return func(r *Runtime) { r.limits = l.resolve() }
}

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
	limits  Limits // resolved per-field defaults, shared by every Instance
}

// New returns a Runtime, using checker to decide install-time consent for
// each capability a guest's host module exposes (see ConsentChecker's doc
// comment on why this is a seam, not a real consent decision, in this
// slice). Pass AlwaysConsent{} for the slice-2.4 default behavior (declared
// == consented). opts configure the resource limits every loaded Instance
// enforces (WithLimits); with no opts the documented defaults apply (see
// Limits).
func New(ctx context.Context, checker ConsentChecker, opts ...Option) *Runtime {
	r := &Runtime{
		cache:   wazero.NewCompilationCache(),
		consent: checker,
		limits:  Limits{}.resolve(),
	}
	for _, o := range opts {
		o(r)
	}
	return r
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
	limits   Limits // resolved per-field defaults, captured at Load
}

// Limits returns the resolved resource limits this instance enforces (the
// defaults applied per-field, exactly as configured at New).
func (i *Instance) Limits() Limits {
	return i.limits
}

// Load compiles code and instantiates it against a host module built from
// manifest's declared capabilities and host's real backing (see
// buildHostModule in hostfuncs.go). If code's own import section requires a
// host function this manifest did not declare (and consent did not grant),
// that function is simply absent from the host module Wazero links against,
// so instantiation fails with a genuine Wazero link error — deny-by-default
// enforced by the WASM boundary itself, not by a runtime check the guest
// could route around (PRD §10.3).
//
// Every Instance's private engine is configured with this Runtime's limits:
// WithMemoryLimitPages caps guest memory growth, and WithCloseOnContextDone
// is what lets a per-Call deadline interrupt a guest that never returns to
// the host (wazero compiles context-done checks into loop headers only when
// it is enabled — verified against wazero v1.12's interpreter and wazevo
// engines).
//
// WithCloseOnContextDone has a second, easily-missed consequence: a
// cancelled caller CONTEXT also closes the instance module — wazero treats
// any context cancellation as "shut the module down", not just "stop this
// call". The per-Call deadline here is stacked BELOW the caller's own
// context (a caller-cancelled context trips the outer deadline and closes
// the module even if the inner fuel/timeout deadline hasn't fired), so a
// caller that cancels its context must expect the Instance to be unusable
// afterwards — no in-repo caller does this today (every call site passes a
// fresh, uncancelled context).
func (r *Runtime) Load(ctx context.Context, manifest sdk.Manifest, host sdk.HostAPI, code []byte) (*Instance, error) {
	cfg := wazero.NewRuntimeConfig().WithCompilationCache(r.cache)
	cfg = cfg.WithMemoryLimitPages(r.limits.MaxMemoryPages)
	// Context-done interruption is always enabled: it is what enforces the
	// execution-timeout and fuel deadlines on in-flight guest execution.
	cfg = cfg.WithCloseOnContextDone(true)
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

	return &Instance{wz: wz, mod: mod, manifest: manifest, host: host, limits: r.limits}, nil
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
//
// Limits (gap 7 / Ticket T2): the call runs under a context carrying BOTH
// deadlines — the fuel budget (when finite) and the execution timeout — and
// wazero interrupts in-flight execution when the earlier one fires
// (WithCloseOnContextDone + the loop-header checks compiled into the
// engine). Whichever deadline fired is reported distinctly: a fuel-budget
// trip returns ErrFuelExhausted, an execution-timeout trip returns
// ErrExecutionTimeout, anything else (caller-canceled context, guest trap)
// passes through unwrapped. The instance's module is closed by wazero when
// a deadline fires mid-call (that is the interruption mechanism); a call
// that completes within its budget leaves the instance fully usable for the
// next call.
func (i *Instance) Call(ctx context.Context, fn string, params ...uint64) ([]uint64, error) {
	exported := i.mod.ExportedFunction(fn)
	if exported == nil {
		return nil, fmt.Errorf("wasm: guest module %q exports no function %q", i.manifest.Name, fn)
	}

	// Stack the deadlines so the EARLIER one is the effective call
	// deadline: fuel (when finite) first, execution timeout second — both
	// derived from the caller's ctx so a caller cancellation still wins.
	callCtx := ctx
	var fuelCtx, timeoutCtx context.Context
	if i.limits.Fuel > 0 {
		var cancel context.CancelFunc
		fuelCtx, cancel = context.WithTimeout(ctx, time.Duration(i.limits.Fuel))
		defer cancel()
		callCtx = fuelCtx
	}
	if i.limits.ExecutionTimeout > 0 {
		var cancel context.CancelFunc
		timeoutCtx, cancel = context.WithTimeout(callCtx, i.limits.ExecutionTimeout)
		defer cancel()
		callCtx = timeoutCtx
	}

	results, err := exported.Call(callCtx, params...)
	if err != nil {
		if fuelCtx != nil && errors.Is(fuelCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: plugin %q function %q exceeded its fuel budget", ErrFuelExhausted, i.manifest.Name, fn)
		}
		if timeoutCtx != nil && errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: plugin %q function %q exceeded the %s execution timeout", ErrExecutionTimeout, i.manifest.Name, fn, i.limits.ExecutionTimeout)
		}
		return nil, err
	}
	return results, nil
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
