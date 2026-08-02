package wasm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glyphux/glyphux/pkg/runtime/wasm"
)

// --- Defaults (non-breaking) ---

func TestDefaultLimitsResolveAndDoNotBreakNormalGuests(t *testing.T) {
	// A Runtime built with no options must resolve the documented defaults
	// (1024-page memory cap, 30s execution timeout, fuel 0 = unlimited) and
	// keep every pre-T2 guest working under them.
	ctx := context.Background()
	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, testManifest("defaults-plugin"), testHostAPI(t, "defaults-plugin"), readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	lim := inst.Limits()
	if lim.MaxMemoryPages != 1024 {
		t.Errorf("default MaxMemoryPages = %d, want 1024 (~64 MiB)", lim.MaxMemoryPages)
	}
	if lim.ExecutionTimeout != 30*time.Second {
		t.Errorf("default ExecutionTimeout = %v, want 30s", lim.ExecutionTimeout)
	}
	if lim.Fuel != 0 {
		t.Errorf("default Fuel = %d, want 0 (unlimited)", lim.Fuel)
	}

	res, err := inst.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("Call run_kv_roundtrip under defaults: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("run_kv_roundtrip returned %d, want 1", got)
	}
}

// --- Execution timeout ---

func TestExecutionTimeoutInterruptsInfiniteLoopAndIsolatesSiblings(t *testing.T) {
	ctx := context.Background()
	rt := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{ExecutionTimeout: 200 * time.Millisecond}))
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, testManifest("busy-plugin"), testHostAPI(t, "busy-plugin"), readFixture(t, "busy_loop.wasm"))
	if err != nil {
		t.Fatalf("Load busy_loop: %v", err)
	}
	defer inst.Close(ctx)

	start := time.Now()
	_, err = inst.Call(ctx, "spin_forever")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected spin_forever to error under the execution timeout")
	}
	if !errors.Is(err, wasm.ErrExecutionTimeout) {
		t.Fatalf("error = %v, want wasm.ErrExecutionTimeout", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout took %v, want ~200ms (the configured deadline)", elapsed)
	}

	// Isolation (trap-isolation pattern): the host goroutine survived to
	// reach this line, and a FRESH sibling instance on the same Runtime
	// still works normally.
	sibling, err := rt.Load(ctx, testManifest("kv-sibling"), testHostAPI(t, "kv-sibling"), readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load sibling: %v", err)
	}
	defer sibling.Close(ctx)
	res, err := sibling.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("sibling run_kv_roundtrip: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("sibling run_kv_roundtrip returned %d, want 1", got)
	}
}

func TestExecutionTimeoutDoesNotFireForCallsWithinBudget(t *testing.T) {
	// The deadline is per-CALL: a fast guest completes under a short
	// timeout and the same instance stays usable for the next call (the
	// module is only closed when a call actually exceeds its deadline).
	ctx := context.Background()
	rt := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{ExecutionTimeout: 500 * time.Millisecond}))
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, testManifest("fast-plugin"), testHostAPI(t, "fast-plugin"), readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	for i := 0; i < 2; i++ {
		res, err := inst.Call(ctx, "run_kv_roundtrip")
		if err != nil {
			t.Fatalf("call %d under a 500ms deadline: %v", i+1, err)
		}
		if got := int32(res[0]); got != 1 {
			t.Fatalf("call %d returned %d, want 1", i+1, got)
		}
	}
}

// --- Fuel ---

func TestFuelExhaustsDeterministicallyOnHeavyCompute(t *testing.T) {
	// Fuel is the binding constraint: a 100ms compute budget vs a 10s
	// execution timeout. A bounded heavy compute loop (fuel_burn) must be
	// interrupted by the fuel budget with the DISTINCT fuel-exhausted
	// error, not the timeout error. fuel_burn(1<<31) is several seconds of
	// wazevo compute on any machine in existence, so the 100ms budget
	// always trips first — repeated across fresh instances to prove the
	// classification is deterministic, not a race (a fuel trip closes the
	// instance's module, so each iteration needs its own).
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		rt := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{
			Fuel:             uint64(100 * time.Millisecond), // fuel units are ns of compute
			ExecutionTimeout: 10 * time.Second,
		}))
		inst, err := rt.Load(ctx, testManifest("fuel-plugin"), testHostAPI(t, "fuel-plugin"), readFixture(t, "fuel_burn.wasm"))
		if err != nil {
			t.Fatalf("Load fuel_burn: %v", err)
		}
		start := time.Now()
		_, err = inst.Call(ctx, "fuel_burn", 1<<31)
		elapsed := time.Since(start)
		inst.Close(ctx)
		rt.Close(ctx)
		if err == nil {
			t.Fatalf("iteration %d: expected fuel_burn to exhaust the fuel budget", i+1)
		}
		if !errors.Is(err, wasm.ErrFuelExhausted) {
			t.Fatalf("iteration %d: error = %v, want wasm.ErrFuelExhausted (fuel is the binding constraint)", i+1, err)
		}
		if elapsed > 5*time.Second {
			t.Fatalf("iteration %d: fuel exhaustion took %v, want ~100ms (the fuel budget, not the 10s timeout)", i+1, elapsed)
		}
	}
}

func TestExecutionTimeoutWinsWhenItIsTheBindingConstraint(t *testing.T) {
	// The fuel/deadline interaction: whichever budget trips FIRST errors
	// the call. Here a generous fuel budget (10s) and a tight execution
	// timeout (200ms) on an infinite loop must produce the TIMEOUT error —
	// proving the two constraints are enforced independently, not merged.
	ctx := context.Background()
	rt := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{
		Fuel:             uint64(10 * time.Second),
		ExecutionTimeout: 200 * time.Millisecond,
	}))
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, testManifest("busy-plugin"), testHostAPI(t, "busy-plugin"), readFixture(t, "busy_loop.wasm"))
	if err != nil {
		t.Fatalf("Load busy_loop: %v", err)
	}
	defer inst.Close(ctx)

	_, err = inst.Call(ctx, "spin_forever")
	if err == nil {
		t.Fatal("expected spin_forever to error")
	}
	if !errors.Is(err, wasm.ErrExecutionTimeout) {
		t.Fatalf("error = %v, want wasm.ErrExecutionTimeout (execution timeout is the binding constraint, not fuel)", err)
	}
}

// --- Memory cap ---

func TestMemoryCapEnforcedAsErrorNotHostCrash(t *testing.T) {
	ctx := context.Background()

	// (a) Cap LARGER than the module's initial 16 pages: the guest grows
	// past the cap (grow_to(64)), memory.grow fails, the fixture traps —
	// Call returns an error and the host process is not crashed. A sibling
	// instance on the same Runtime still works.
	rt := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{MaxMemoryPages: 32}))
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, testManifest("hog-plugin"), testHostAPI(t, "hog-plugin"), readFixture(t, "memory_hog.wasm"))
	if err != nil {
		t.Fatalf("Load memory_hog: %v", err)
	}
	defer inst.Close(ctx)

	_, err = inst.Call(ctx, "grow_to", 64)
	if err == nil {
		t.Fatal("expected grow_to(64) to error under the 32-page memory cap")
	}

	sibling, err := rt.Load(ctx, testManifest("kv-sibling"), testHostAPI(t, "kv-sibling"), readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load sibling: %v", err)
	}
	defer sibling.Close(ctx)
	res, err := sibling.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("sibling run_kv_roundtrip: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("sibling run_kv_roundtrip returned %d, want 1", got)
	}

	// (b) Cap SMALLER than the module's initial 16 pages: the module
	// cannot even be instantiated — the memory cap is enforced at Load.
	rtTiny := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{MaxMemoryPages: 8}))
	defer rtTiny.Close(ctx)
	if _, err := rtTiny.Load(ctx, testManifest("hog-plugin"), testHostAPI(t, "hog-plugin"), readFixture(t, "memory_hog.wasm")); err == nil {
		t.Fatal("expected Load of a 16-page module under an 8-page cap to fail")
	}

	// (c) Red/green contrast isolating the cap as the cause: the SAME
	// guest completes when the cap is not the binding constraint.
	rtBig := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{MaxMemoryPages: 1024}))
	defer rtBig.Close(ctx)
	instBig, err := rtBig.Load(ctx, testManifest("hog-plugin"), testHostAPI(t, "hog-plugin"), readFixture(t, "memory_hog.wasm"))
	if err != nil {
		t.Fatalf("Load memory_hog (big cap): %v", err)
	}
	defer instBig.Close(ctx)
	res, err = instBig.Call(ctx, "grow_to", 4)
	if err != nil {
		t.Fatalf("grow_to(4) with a 1024-page cap: %v", err)
	}
	if got := int32(res[0]); got != 4 {
		t.Fatalf("grow_to(4) grew %d pages, want 4", got)
	}
}

// --- Per-instance enforcement ---

func TestLimitsAreEnforcedPerInstanceNotShared(t *testing.T) {
	// Two instances of the SAME module loaded from one Runtime with the
	// same limits: each instance's engine enforces its own budget, so A
	// tripping its deadline neither spares nor kills B — B must trip its
	// OWN deadline too, and a fast instance on the same Runtime is
	// unaffected by either.
	ctx := context.Background()
	rt := wasm.New(ctx, wasm.AlwaysConsent{}, wasm.WithLimits(wasm.Limits{ExecutionTimeout: 200 * time.Millisecond}))
	defer rt.Close(ctx)

	instA, err := rt.Load(ctx, testManifest("busy-a"), testHostAPI(t, "busy-a"), readFixture(t, "busy_loop.wasm"))
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	defer instA.Close(ctx)
	if _, err := instA.Call(ctx, "spin_forever"); !errors.Is(err, wasm.ErrExecutionTimeout) {
		t.Fatalf("A: error = %v, want ErrExecutionTimeout", err)
	}

	instB, err := rt.Load(ctx, testManifest("busy-b"), testHostAPI(t, "busy-b"), readFixture(t, "busy_loop.wasm"))
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	defer instB.Close(ctx)
	if _, err := instB.Call(ctx, "spin_forever"); !errors.Is(err, wasm.ErrExecutionTimeout) {
		t.Fatalf("B: error = %v, want ErrExecutionTimeout (limits are per-instance, not a shared budget A exhausted)", err)
	}

	instC, err := rt.Load(ctx, testManifest("kv-c"), testHostAPI(t, "kv-c"), readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load C: %v", err)
	}
	defer instC.Close(ctx)
	res, err := instC.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("C run_kv_roundtrip: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("C run_kv_roundtrip returned %d, want 1", got)
	}
}
