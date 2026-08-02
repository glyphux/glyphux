package sdk_test

import (
	"testing"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// netManifest builds a valid WASM-tier manifest declaring a "network"
// permission allowlisting exactly hosts (deny-by-default: every other host
// is denied). The contract/core fields mirror validManifest's; the network
// permission is the axis FilterManifest is about.
func netManifest(hosts ...string) sdk.Manifest {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network", Args: hosts}}
	return m
}

// --- Acceptance criterion: declared [a.example], granted [a.example] =>
// filtered AllowsNetworkHost(a.example)==true, b.example==false. ---

func TestFilterManifestGrantedMatchesDeclaredKeepsExactlyGranted(t *testing.T) {
	m := netManifest("a.example")
	granted := []sdk.Permission{{Name: "network", Args: []string{"a.example"}}}

	filtered := sdk.FilterManifest(m, granted, nil)
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid: %v", err)
	}
	if !filtered.AllowsNetworkHost("a.example") {
		t.Fatal("filtered manifest must allow a.example (declared and granted)")
	}
	if filtered.AllowsNetworkHost("b.example") {
		t.Fatal("filtered manifest must deny b.example (deny-by-default, not declared)")
	}

	// The network permission's Args are exactly the granted list.
	perms := filtered.Permissions
	if len(perms) != 1 || perms[0].Name != "network" {
		t.Fatalf("filtered Permissions = %+v, want exactly one network permission", perms)
	}
	if len(perms[0].Args) != 1 || perms[0].Args[0] != "a.example" {
		t.Fatalf("filtered network Args = %v, want exactly [a.example]", perms[0].Args)
	}

	// The enforcement POINT: a HostAPI built from the filtered manifest
	// answers from the granted subset only.
	host, err := sdk.NewHostAPI(filtered, sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI(filtered): %v", err)
	}
	if !host.AllowsNetworkHost("a.example") {
		t.Fatal("HostAPI built from filtered manifest must allow a.example")
	}
	if host.AllowsNetworkHost("b.example") {
		t.Fatal("HostAPI built from filtered manifest must deny b.example")
	}
}

// --- Acceptance criterion: declared [a.example,b.example], granted only
// a.example => b.example denied (granted wins over declared). ---

func TestFilterManifestGrantedSubsetWinsOverDeclared(t *testing.T) {
	m := netManifest("a.example", "b.example")
	granted := []sdk.Permission{{Name: "network", Args: []string{"a.example"}}}

	filtered := sdk.FilterManifest(m, granted, nil)
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid: %v", err)
	}
	if !filtered.AllowsNetworkHost("a.example") {
		t.Fatal("a.example was granted, must be allowed")
	}
	if filtered.AllowsNetworkHost("b.example") {
		t.Fatal("b.example was declared but NOT granted — granted wins, must be denied")
	}
}

// --- Acceptance criterion: consent drops the network permission entirely =>
// every host denied. ---

func TestFilterManifestNoNetworkGrantDeniesEveryHost(t *testing.T) {
	m := netManifest("a.example", "b.example")

	filtered := sdk.FilterManifest(m, nil, nil) // nothing granted
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid: %v", err)
	}
	if len(filtered.Permissions) != 0 {
		t.Fatalf("filtered Permissions = %+v, want none (network dropped entirely)", filtered.Permissions)
	}
	if filtered.AllowsNetworkHost("a.example") {
		t.Fatal("a.example must be denied: consent dropped the network permission")
	}
	if filtered.AllowsNetworkHost("b.example") {
		t.Fatal("b.example must be denied: consent dropped the network permission")
	}
}

// --- Granted wins per permission: declared [network, admin_ui], granted
// [network] => filtered keeps network, drops admin_ui. ---

func TestFilterManifestOnlyGrantedPermissionsSurvive(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{
		{Name: "network", Args: []string{"a.example"}},
		{Name: "admin_ui"},
	}
	granted := []sdk.Permission{{Name: "network", Args: []string{"a.example"}}}

	filtered := sdk.FilterManifest(m, granted, nil)
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid: %v", err)
	}
	if len(filtered.Permissions) != 1 || filtered.Permissions[0].Name != "network" {
		t.Fatalf("filtered Permissions = %+v, want only the granted network permission (admin_ui dropped)", filtered.Permissions)
	}
}

// --- Empty granted network Args (a grant of "network" with no hosts)
// means deny-all: the permission is dropped so the filtered manifest stays
// valid (a zero-host network permission is itself invalid) and every host
// is denied. ---

func TestFilterManifestEmptyNetworkGrantDeniesAllAndStaysValid(t *testing.T) {
	m := netManifest("a.example")
	granted := []sdk.Permission{{Name: "network", Args: nil}}

	filtered := sdk.FilterManifest(m, granted, nil)
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid even for an empty network grant: %v", err)
	}
	if len(filtered.Permissions) != 0 {
		t.Fatalf("filtered Permissions = %+v, want none (empty network grant = deny-all)", filtered.Permissions)
	}
	if filtered.AllowsNetworkHost("a.example") {
		t.Fatal("a.example must be denied: the grant allowed no hosts")
	}
}

// --- Defensive: a granted permission the manifest never declared cannot
// appear in the filtered manifest (consent itself rejects such a grant, but
// the filter must not invent surface either). ---

func TestFilterManifestNeverAddsUndeclaredPermission(t *testing.T) {
	m := netManifest("a.example")
	granted := []sdk.Permission{{Name: "admin_ui"}} // declared nowhere in m

	filtered := sdk.FilterManifest(m, granted, nil)
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid: %v", err)
	}
	if len(filtered.Permissions) != 0 {
		t.Fatalf("filtered Permissions = %+v, want none (admin_ui was never declared, network was not granted)", filtered.Permissions)
	}
}

// --- Fix A (red-team hardening): the API axis must be narrowed the same
// way the Permissions axis is. A Tier-C plugin granted content:[read] of a
// declared content:[read,write] must get a host whose hasScope gates answer
// only for content:read — the broker's per-scope consent seam is consulted
// at the host, and the host is built from THIS filtered manifest. ---

// apiManifest builds a valid WASM-tier manifest declaring exactly caps as
// its API axis.
func apiManifest(caps ...sdk.APIScope) sdk.Manifest {
	m := validManifest()
	m.API = caps
	return m
}

func TestFilterManifestAPIAxisNarrowsToGrantedSubset(t *testing.T) {
	m := apiManifest(
		sdk.APIScope{Capability: "content", Scopes: []string{"read", "write"}},
		sdk.APIScope{Capability: "media", Scopes: []string{"read"}},
	)
	granted := []sdk.Permission{{Name: "network", Args: []string{"a.example"}}}
	grantedAPI := []sdk.APIScope{{Capability: "content", Scopes: []string{"read"}}}

	filtered := sdk.FilterManifest(m, granted, grantedAPI)
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid: %v", err)
	}
	if len(filtered.API) != 1 || filtered.API[0].Capability != "content" {
		t.Fatalf("filtered API = %+v, want exactly content (media declared but not granted)", filtered.API)
	}
	if len(filtered.API[0].Scopes) != 1 || filtered.API[0].Scopes[0] != "read" {
		t.Fatalf("filtered content scopes = %v, want exactly [read] (granted subset wins over declared)", filtered.API[0].Scopes)
	}

	// The enforcement point: a HostAPI built from the filtered manifest
	// gates scopes from the granted subset only — content:write must be
	// denied even though the plugin DECLARED it.
	host, err := sdk.NewHostAPI(filtered, sdk.KernelDeps{})
	if err != nil {
		t.Fatalf("NewHostAPI(filtered): %v", err)
	}
	if !host.HasAPIScope("content", "read") {
		t.Fatal("host must allow content:read (declared and granted)")
	}
	if host.HasAPIScope("content", "write") {
		t.Fatal("host must deny content:write (declared but NOT granted — the Tier-C API-axis over-grant)")
	}
	if host.HasAPIScope("media", "read") {
		t.Fatal("host must deny media:read (declared but not granted)")
	}
}

func TestFilterManifestDropsAPICapabilityGrantedZeroScopes(t *testing.T) {
	m := apiManifest(sdk.APIScope{Capability: "content", Scopes: []string{"read", "write"}})
	grantedAPI := []sdk.APIScope{{Capability: "content", Scopes: nil}} // "allow nothing"

	filtered := sdk.FilterManifest(m, nil, grantedAPI)
	if len(filtered.API) != 0 {
		t.Fatalf("filtered API = %+v, want empty (a zero-scope grant means allow nothing)", filtered.API)
	}
	if err := filtered.Validate(); err != nil {
		t.Fatalf("filtered manifest must stay valid: %v", err)
	}
}

func TestFilterManifestAPIAxisNeverAddsUndeclaredCapability(t *testing.T) {
	m := apiManifest(sdk.APIScope{Capability: "content", Scopes: []string{"read"}})
	grantedAPI := []sdk.APIScope{
		{Capability: "content", Scopes: []string{"read"}},
		{Capability: "media", Scopes: []string{"read"}}, // declared nowhere in m
	}

	filtered := sdk.FilterManifest(m, nil, grantedAPI)
	if len(filtered.API) != 1 || filtered.API[0].Capability != "content" {
		t.Fatalf("filtered API = %+v, want content only (undeclared grants are never invented)", filtered.API)
	}
}
