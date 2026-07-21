package wasm

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// hostModuleName is the WASM import module name every guest fixture in this
// package imports its host functions from (Rust's
// `#[link(wasm_import_module = "env")]`, matching the wasm32-unknown-unknown
// convention).
const hostModuleName = "env"

// KV result sentinels returned to the guest as an i32 (see testdata/*.rs):
// non-negative means success/length, negative means a specific failure.
const (
	kvNotFound  int32 = -1
	kvHostError int32 = -2
)

// declaredScope reports whether manifest declares capability with scope in
// its API axis (PRD §7.3/§7.4) — the same question pkg/sdk's own unexported
// hasScope answers internally for its runtime-error gate, replicated here
// because Runtime must decide, BEFORE any guest call happens, whether a
// given host function is even present in the guest's imports at all. That
// is the deny-by-default distinction this slice is built to prove (PRD
// §10.3): absence from the import set, not merely a call-time rejection.
func declaredScope(manifest sdk.Manifest, capability, scope string) bool {
	for _, s := range manifest.API {
		if s.Capability != capability {
			continue
		}
		for _, sc := range s.Scopes {
			if sc == scope {
				return true
			}
		}
	}
	return false
}

// buildHostModule constructs the "env" host module a guest imports from,
// exposing ONLY the host functions backed by a capability this manifest
// declared and consent (via checker) granted. Every function is a real
// bridge into host — never a stub — so a guest call actually reaches
// pkg/sdk's HostAPI.
//
// kv_get/kv_set/kv_delete are unconditional: pkg/sdk's HostAPI.Store()
// itself carries no api capability to declare (see host.go's doc comment on
// Store — scoped persistence is available regardless of the manifest's API
// axis), so Runtime mirrors that same contract rather than inventing a
// stricter one that doesn't exist anywhere else in the SDK.
//
// emit_event is gated on the "events" capability's "emit" scope, plus
// checker.Consented — the demonstrable deny-by-default vertical this slice
// proves end to end: a manifest that never declared events:emit produces a
// host module with no emit_event export at all, so a guest binary that
// imports it fails to instantiate (a real wazero link error), not merely a
// runtime rejection a guest could work around by catching an error code.
func buildHostModule(rt wazero.Runtime, manifest sdk.Manifest, host sdk.HostAPI, checker ConsentChecker) wazero.HostModuleBuilder {
	b := rt.NewHostModuleBuilder(hostModuleName)

	b.NewFunctionBuilder().WithFunc(kvGetFunc(host)).Export("kv_get")
	b.NewFunctionBuilder().WithFunc(kvSetFunc(host)).Export("kv_set")
	b.NewFunctionBuilder().WithFunc(kvDeleteFunc(host)).Export("kv_delete")

	if declaredScope(manifest, "events", "emit") && checker.Consented(manifest.Name, "events") {
		b.NewFunctionBuilder().WithFunc(emitEventFunc(host)).Export("emit_event")
	}

	return b
}

// kvGetFunc bridges the guest's kv_get(keyPtr, keyLen, outPtr, outCap) i32
// call to a real host.Store().Get. Returns the value's length on success (a
// short read is the guest's own bug, signaled via kvHostError since the
// value could not fully be written), kvNotFound if absent, kvHostError on
// any host-side error or an out-of-bounds guest pointer.
func kvGetFunc(host sdk.HostAPI) func(ctx context.Context, mod api.Module, keyPtr, keyLen, outPtr, outCap uint32) int32 {
	return func(ctx context.Context, mod api.Module, keyPtr, keyLen, outPtr, outCap uint32) int32 {
		key, ok := mod.Memory().Read(keyPtr, keyLen)
		if !ok {
			return kvHostError
		}
		val, found, err := host.Store().Get(ctx, string(key))
		if err != nil {
			return kvHostError
		}
		if !found {
			return kvNotFound
		}
		if uint32(len(val)) > outCap {
			return kvHostError
		}
		if !mod.Memory().Write(outPtr, val) {
			return kvHostError
		}
		return int32(len(val))
	}
}

// kvSetFunc bridges the guest's kv_set(keyPtr, keyLen, valPtr, valLen) i32
// call to a real host.Store().Set. Returns 0 on success, kvHostError
// otherwise.
func kvSetFunc(host sdk.HostAPI) func(ctx context.Context, mod api.Module, keyPtr, keyLen, valPtr, valLen uint32) int32 {
	return func(ctx context.Context, mod api.Module, keyPtr, keyLen, valPtr, valLen uint32) int32 {
		key, ok := mod.Memory().Read(keyPtr, keyLen)
		if !ok {
			return kvHostError
		}
		val, ok := mod.Memory().Read(valPtr, valLen)
		if !ok {
			return kvHostError
		}
		if err := host.Store().Set(ctx, string(key), val); err != nil {
			return kvHostError
		}
		return 0
	}
}

// kvDeleteFunc bridges the guest's kv_delete(keyPtr, keyLen) i32 call to a
// real host.Store().Delete. Returns 0 on success, kvHostError otherwise.
func kvDeleteFunc(host sdk.HostAPI) func(ctx context.Context, mod api.Module, keyPtr, keyLen uint32) int32 {
	return func(ctx context.Context, mod api.Module, keyPtr, keyLen uint32) int32 {
		key, ok := mod.Memory().Read(keyPtr, keyLen)
		if !ok {
			return kvHostError
		}
		if err := host.Store().Delete(ctx, string(key)); err != nil {
			return kvHostError
		}
		return 0
	}
}

// emitEventFunc bridges the guest's
// emit_event(namePtr, nameLen, payloadPtr, payloadLen) i32 call to a real
// host.Emit — the payload crosses the boundary as raw bytes (the guest's
// own encoding choice; this slice doesn't impose one). Returns 0 on
// success, kvHostError if host.Emit returned an error (e.g. no subscriber
// requires no error — errors here are genuine host.Emit failures) or the
// guest pointers were out of bounds.
func emitEventFunc(host sdk.HostAPI) func(ctx context.Context, mod api.Module, namePtr, nameLen, payloadPtr, payloadLen uint32) int32 {
	return func(ctx context.Context, mod api.Module, namePtr, nameLen, payloadPtr, payloadLen uint32) int32 {
		name, ok := mod.Memory().Read(namePtr, nameLen)
		if !ok {
			return kvHostError
		}
		payload, ok := mod.Memory().Read(payloadPtr, payloadLen)
		if !ok {
			return kvHostError
		}
		if err := host.Emit(ctx, string(name), append([]byte(nil), payload...)); err != nil {
			return kvHostError
		}
		return 0
	}
}
