package wasm

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// hostFuncNames instantiates the "env" host module exactly as Load would
// (buildHostModule over manifest/host/checker) and returns the sorted names
// of every exported host function — the full surface a guest can import.
func hostFuncNames(t *testing.T, m sdk.Manifest, host sdk.HostAPI, checker ConsentChecker) []string {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	mod, err := buildHostModule(rt, m, host, checker).Instantiate(ctx)
	if err != nil {
		t.Fatalf("instantiate host module: %v", err)
	}
	defer mod.Close(ctx)

	var names []string
	for name := range mod.ExportedFunctionDefinitions() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// validHostManifest is a valid manifest declaring events:emit (the widest
// host surface a guest can legitimately get today: the three unconditional
// KV funcs plus emit_event).
func validHostManifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    "host-surface-plugin",
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
		API: []sdk.APIScope{{Capability: "events", Scopes: []string{"emit"}}},
	}
}

// TestNoNetworkHostFunctions is the Tier-B deny-by-absence proof (Ticket
// T3): the "env" host module exposes NO function a guest could use to reach
// the network. There is no dial/fetch/connect/socket/http host function —
// not gated, not hidden behind a scope: absent from the import surface
// entirely, so a guest binary that imports one fails to instantiate with a
// real wazero link error (the same deny-by-absence mechanism events:emit
// proves in reverse). The maximal surface is exactly the three KV functions
// plus emit_event.
func TestNoNetworkHostFunctions(t *testing.T) {
	m := validHostManifest()
	host, err := sdk.NewHostAPI(m, sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}

	names := hostFuncNames(t, m, host, AlwaysConsent{})
	want := []string{"emit_event", "kv_delete", "kv_get", "kv_set"}
	if len(names) != len(want) {
		t.Fatalf("host exports = %v, want exactly %v (the maximal surface)", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("host exports = %v, want exactly %v", names, want)
		}
	}

	for _, name := range names {
		lower := strings.ToLower(name)
		for _, banned := range []string{"net", "dial", "fetch", "connect", "socket", "http", "tcp", "udp", "dns"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("host export %q looks like a network primitive — Tier B must have no network host functions", name)
			}
		}
	}
}

// TestHostSurfaceWithoutEventsIsExactlyKV proves the base (non-events)
// surface is exactly the three KV functions — nothing else appears when the
// manifest declares no api capability at all.
func TestHostSurfaceWithoutEventsIsExactlyKV(t *testing.T) {
	m := validHostManifest()
	m.API = nil
	host, err := sdk.NewHostAPI(m, sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI: %v", err)
	}

	names := hostFuncNames(t, m, host, AlwaysConsent{})
	want := []string{"kv_delete", "kv_get", "kv_set"}
	if len(names) != len(want) {
		t.Fatalf("host exports = %v, want exactly %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("host exports = %v, want exactly %v", names, want)
		}
	}
}
