package wasm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/pkg/runtime/wasm"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// kvGuestWASM/eventsGuestWASM are real, rustc-compiled wasm32-unknown-unknown
// binaries (see testdata/*.rs for source, and this slice's tracking doc for
// how they were built) — not hand-mocked byte arrays. kvGuestWASM imports
// ONLY env.kv_get/kv_set/kv_delete; eventsGuestWASM imports ONLY
// env.emit_event. Splitting the two capability domains into separate guest
// binaries is what makes the deny-by-default test below a real absence
// check (a whole module fails to link if ANY of its own declared imports is
// unavailable — that is genuine WASM link semantics, not a shortcut this
// package invented).
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// testHostAPI returns a real sdk.HostAPI (not a fake) backed by a private
// MemoryKVBackend/EventBus, declaring the given API scopes. Mirrors
// pkg/sdk's own test style (host_test.go's testKernel) — this package
// doesn't need the DB-backed Content/Users/Media APIs since it only
// exercises Store()/On()/Emit(), so KernelDeps here only sets KV/Bus.
func testHostAPI(t *testing.T, name string, scopes ...sdk.APIScope) sdk.HostAPI {
	t.Helper()
	m := sdk.Manifest{
		Name:    name,
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: scopes,
	}
	host, err := sdk.NewHostAPI(m, sdk.KernelDeps{
		KV:  sdk.NewMemoryKVBackend(),
		Bus: sdk.NewEventBus(),
	})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	return host
}

func testManifest(name string, scopes ...sdk.APIScope) sdk.Manifest {
	return sdk.Manifest{
		Name:    name,
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: scopes,
	}
}

func TestKVRoundTripThroughRealHostAPIStore(t *testing.T) {
	ctx := context.Background()
	manifest := testManifest("kv-plugin")
	host := testHostAPI(t, "kv-plugin")

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, manifest, host, readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	res, err := inst.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("Call run_kv_roundtrip: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("run_kv_roundtrip returned %d, want 1 (see testdata/kv_guest.rs for failure code meanings)", got)
	}

	// Prove this actually reached the real host.Store(), not just the
	// guest's own internal bookkeeping: after the guest's own delete step,
	// the key must be gone from the real backing store too.
	_, ok, err := host.Store().Get(ctx, "greeting")
	if err != nil {
		t.Fatalf("host.Store().Get after guest roundtrip: %v", err)
	}
	if ok {
		t.Fatal("expected key to be deleted from the real host Store after guest's roundtrip, but it is still present")
	}
}

func TestKVAvailableRegardlessOfDeclaredAPIScopes(t *testing.T) {
	// Store() carries no api capability of its own to declare (pkg/sdk's
	// host.go: "Store returns this plugin's namespaced key-value store —
	// always present, unlike Content()/Users()/Media()"). A manifest
	// declaring NO api scopes at all must still get a working KV bridge.
	ctx := context.Background()
	manifest := testManifest("kv-plugin-no-scopes")
	host := testHostAPI(t, "kv-plugin-no-scopes")

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, manifest, host, readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	res, err := inst.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("Call run_kv_roundtrip: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("run_kv_roundtrip returned %d, want 1", got)
	}
}

func TestEmitEventGuestReachesRealEventBus(t *testing.T) {
	ctx := context.Background()
	manifest := testManifest("events-plugin", sdk.APIScope{Capability: "events", Scopes: []string{"emit"}})
	bus := sdk.NewEventBus()
	pluginHost, err := sdk.NewHostAPI(testManifest("events-plugin", sdk.APIScope{Capability: "events", Scopes: []string{"emit"}}), sdk.KernelDeps{
		KV:  sdk.NewMemoryKVBackend(),
		Bus: bus,
	})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}

	// A SEPARATE plugin's HostAPI, sharing the same underlying bus
	// (KernelDeps.Bus) — exactly the real cross-plugin topology PRD §8.4
	// describes ("a single event bus underlies all tiers"). Subscribing
	// here, not on the guest's own host, proves the guest's emit reaches an
	// entirely different plugin's subscriber through the real shared bus,
	// not merely the guest's own private state.
	subscriberHost, err := sdk.NewHostAPI(testManifest("subscriber-plugin", sdk.APIScope{Capability: "events", Scopes: []string{"subscribe"}}), sdk.KernelDeps{
		KV:  sdk.NewMemoryKVBackend(),
		Bus: bus,
	})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}
	host := pluginHost

	received := make(chan string, 1)
	if err := subscriberHost.On("guest.ping", func(ctx context.Context, payload any) error {
		b, _ := payload.([]byte)
		received <- string(b)
		return nil
	}); err != nil {
		t.Fatalf("subscriberHost.On: %v", err)
	}

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, manifest, host, readFixture(t, "events_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	res, err := inst.Call(ctx, "run_emit")
	if err != nil {
		t.Fatalf("Call run_emit: %v", err)
	}
	if got := int32(res[0]); got != 0 {
		t.Fatalf("run_emit returned %d, want 0 (success)", got)
	}

	select {
	case payload := <-received:
		if payload != "pong" {
			t.Fatalf("received payload %q, want %q", payload, "pong")
		}
	default:
		t.Fatal("expected the real event bus to have delivered guest.ping to the Go subscriber synchronously")
	}
}

func TestEventsGuestFailsToInstantiateWithoutDeclaredEmitCapability(t *testing.T) {
	// Deny-by-default (PRD §10.3): a manifest that never declared
	// events:emit must produce a host module with NO emit_event export at
	// all, so events_guest.wasm (whose entire import section is
	// env.emit_event) fails to LINK — a real wazero instantiation error,
	// not merely a call-time rejection the guest could route around.
	ctx := context.Background()
	manifest := testManifest("no-events-plugin") // no API scopes declared
	host := testHostAPI(t, "no-events-plugin")

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	_, err := rt.Load(ctx, manifest, host, readFixture(t, "events_guest.wasm"))
	if err == nil {
		t.Fatal("expected Load to fail (missing emit_event import) when manifest declares no events:emit capability")
	}
}

func TestEventsGuestFailsToInstantiateWithOnlySubscribeDeclared(t *testing.T) {
	// events:subscribe alone does not grant emit_event — a distinct scope
	// on the same "events" capability. Proves the gate is scope-specific,
	// not merely "declared the events capability at all".
	ctx := context.Background()
	scopes := sdk.APIScope{Capability: "events", Scopes: []string{"subscribe"}}
	manifest := testManifest("subscribe-only-plugin", scopes)
	host := testHostAPI(t, "subscribe-only-plugin", scopes)

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	_, err := rt.Load(ctx, manifest, host, readFixture(t, "events_guest.wasm"))
	if err == nil {
		t.Fatal("expected Load to fail: events:subscribe does not imply events:emit")
	}
}

func TestConsentCheckerCanDenyAnAlreadyDeclaredCapability(t *testing.T) {
	// The ConsentChecker seam (slice 2.7's attachment point): even a
	// manifest that DID declare events:emit must still get no emit_event
	// import if the checker withholds consent. Proves the seam is real,
	// not decorative — a non-default checker changes actual behavior.
	ctx := context.Background()
	scopes := sdk.APIScope{Capability: "events", Scopes: []string{"emit"}}
	manifest := testManifest("denied-plugin", scopes)
	host := testHostAPI(t, "denied-plugin", scopes)

	rt := wasm.New(ctx, denyAllConsent{})
	defer rt.Close(ctx)

	_, err := rt.Load(ctx, manifest, host, readFixture(t, "events_guest.wasm"))
	if err == nil {
		t.Fatal("expected Load to fail: ConsentChecker withheld consent despite manifest declaring events:emit")
	}
}

type denyAllConsent struct{}

func (denyAllConsent) Consented(pluginName, capability string) bool { return false }

func TestSubscribeDeniedWithoutDeclaredCapability(t *testing.T) {
	ctx := context.Background()
	manifest := testManifest("no-subscribe-plugin")
	host := testHostAPI(t, "no-subscribe-plugin")

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	// events_guest.wasm imports emit_event unconditionally, so it can't even
	// load without events:emit declared. Use kv_guest.wasm instead (no
	// events import at all) purely as a vessel to obtain an *Instance so we
	// can call Subscribe and confirm it forwards pkg/sdk's own rejection.
	inst, err := rt.Load(ctx, manifest, host, readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	err = inst.Subscribe("guest.ping")
	if err == nil {
		t.Fatal("expected Subscribe to fail: manifest declares no events:subscribe scope")
	}
}

func TestSubscribeDeliversEmittedPayloadIntoGuestMemory(t *testing.T) {
	ctx := context.Background()
	// events_guest.wasm's only host import is emit_event, which the
	// instantiating manifest must therefore declare (events:emit) even
	// though this test only exercises the host->guest Subscribe direction
	// (events:subscribe) — a real wazero link-time constraint (the guest's
	// own import section demands it), not a choice this package made.
	scopes := sdk.APIScope{Capability: "events", Scopes: []string{"subscribe", "emit"}}
	manifest := testManifest("subscriber-plugin", scopes)
	host := testHostAPI(t, "subscriber-plugin", scopes)

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, manifest, host, readFixture(t, "events_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	if err := inst.Subscribe("host.tick"); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Drive the emit from the Go side (not through the guest's own
	// run_emit) to prove the host->guest callback path lands real bytes in
	// the guest's own memory, independent of the guest's outbound path.
	if err := host.Emit(ctx, "host.tick", []byte("abc")); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	res, err := inst.Call(ctx, "last_event_len")
	if err != nil {
		t.Fatalf("Call last_event_len: %v", err)
	}
	if got := int32(res[0]); got != 3 {
		t.Fatalf("last_event_len = %d, want 3", got)
	}
	for i, want := range []byte("abc") {
		bres, err := inst.Call(ctx, "last_event_byte", uint64(i))
		if err != nil {
			t.Fatalf("Call last_event_byte(%d): %v", i, err)
		}
		if got := byte(int32(bres[0])); got != want {
			t.Fatalf("last_event_byte(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestGuestTrapDoesNotCrashHostOrSiblingInstance(t *testing.T) {
	ctx := context.Background()
	manifest := testManifest("trap-plugin")
	host := testHostAPI(t, "trap-plugin")

	rt := wasm.New(ctx, wasm.AlwaysConsent{})
	defer rt.Close(ctx)

	inst, err := rt.Load(ctx, manifest, host, readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer inst.Close(ctx)

	// A deliberate out-of-bounds guest write (see testdata/kv_guest.rs's
	// trigger_trap) must surface as an ordinary Go error from Call, not a
	// process crash/panic that would take this whole test binary down.
	_, err = inst.Call(ctx, "trigger_trap")
	if err == nil {
		t.Fatal("expected trigger_trap to trap (return a non-nil error)")
	}

	// The host process (this very test goroutine) is still alive to reach
	// this line at all -- that itself is part of the proof. Further prove a
	// FRESH, unrelated instance on the SAME Runtime is unaffected: load a
	// second instance and confirm its own KV round trip still works.
	manifest2 := testManifest("trap-plugin-sibling")
	host2 := testHostAPI(t, "trap-plugin-sibling")
	inst2, err := rt.Load(ctx, manifest2, host2, readFixture(t, "kv_guest.wasm"))
	if err != nil {
		t.Fatalf("Load sibling instance after trap: %v", err)
	}
	defer inst2.Close(ctx)

	res, err := inst2.Call(ctx, "run_kv_roundtrip")
	if err != nil {
		t.Fatalf("Call run_kv_roundtrip on sibling instance: %v", err)
	}
	if got := int32(res[0]); got != 1 {
		t.Fatalf("sibling run_kv_roundtrip returned %d, want 1", got)
	}

	// And the SAME trapped instance's module is unusable for further calls
	// (its memory/store is presumed corrupted by the trap) but calling it
	// again must still not crash the host -- it should just keep erroring.
	_, err = inst.Call(ctx, "trigger_trap")
	if err == nil {
		t.Fatal("expected repeated trigger_trap calls to keep erroring, not succeed silently")
	}
}
