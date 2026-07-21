package sdk_test

import (
	"testing"

	"github.com/glyphux/glyphux/pkg/sdk"
)

func TestManifestValidateRejectsEmptyName(t *testing.T) {
	m := sdk.Manifest{Version: "1.0.0"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestManifestValidateRejectsEmptyVersion(t *testing.T) {
	m := sdk.Manifest{Name: "forms"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for empty version, got nil")
	}
}

func TestManifestValidateRejectsMalformedVersion(t *testing.T) {
	m := sdk.Manifest{Name: "forms", Version: "not-a-version"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for malformed version, got nil")
	}
}

func TestManifestValidateAcceptsWellFormedNameAndVersion(t *testing.T) {
	m := validManifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid manifest to pass, got %v", err)
	}
}

func TestManifestValidateRejectsUnknownRuntime(t *testing.T) {
	m := validManifest()
	m.Runtime = "quantum"
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for unknown runtime tier, got nil")
	}
}

func TestManifestValidateAcceptsEachKnownRuntime(t *testing.T) {
	for _, rt := range []sdk.RuntimeTier{sdk.RuntimeWASM, sdk.RuntimeRPC, sdk.RuntimeInProcess} {
		m := validManifest()
		m.Runtime = rt
		if err := m.Validate(); err != nil {
			t.Errorf("runtime %q: expected valid, got %v", rt, err)
		}
	}
}

func TestManifestValidateAcceptsWellFormedRequires(t *testing.T) {
	m := validManifest()
	m.Requires = sdk.Requires{Core: ">=1.0.0", Contract: "content-composition/v0"}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid requires to pass, got %v", err)
	}
}

func TestManifestValidateRejectsMalformedRequiresCore(t *testing.T) {
	m := validManifest()
	m.Requires = sdk.Requires{Core: "whenever", Contract: "content-composition/v0"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for malformed requires.core, got nil")
	}
}

func TestManifestValidateRejectsEmptyRequiresContract(t *testing.T) {
	m := validManifest()
	m.Requires = sdk.Requires{Core: ">=1.0.0"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for empty requires.contract, got nil")
	}
}

func TestManifestValidateAcceptsWellFormedAPIScopes(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{
		{Capability: "content", Scopes: []string{"read", "write"}},
		{Capability: "events", Scopes: []string{"emit", "subscribe"}},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid api scopes to pass, got %v", err)
	}
}

func TestManifestValidateRejectsUnknownAPICapability(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{{Capability: "quantum", Scopes: []string{"read"}}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for unknown api capability, got nil")
	}
}

func TestManifestValidateRejectsUnknownScopeForKnownCapability(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{{Capability: "content", Scopes: []string{"levitate"}}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for unknown scope, got nil")
	}
}

func TestManifestValidateRejectsAPICapabilityWithNoScopes(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{{Capability: "content", Scopes: nil}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for capability with no scopes, got nil")
	}
}

func TestManifestValidateRejectsDuplicateAPICapability(t *testing.T) {
	m := validManifest()
	m.API = []sdk.APIScope{
		{Capability: "content", Scopes: []string{"read"}},
		{Capability: "content", Scopes: []string{"write"}},
	}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for duplicate api capability entries, got nil")
	}
}

func TestManifestValidateAcceptsWellFormedPermissions(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{
		{Name: "admin_ui"},
		{Name: "scheduled_jobs"},
		{Name: "network", Args: []string{"api.stripe.com"}},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid permissions to pass, got %v", err)
	}
}

func TestManifestValidateRejectsUnknownPermission(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "raw_database"}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for unknown/forbidden permission, got nil")
	}
}

func TestManifestValidateRejectsNetworkPermissionWithNoAllowlist(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network"}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for network permission with no allowlisted domains, got nil")
	}
}

func TestManifestValidateRejectsNonNetworkPermissionWithArgs(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "admin_ui", Args: []string{"unexpected"}}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for non-network permission carrying args, got nil")
	}
}

func TestManifestValidateRejectsDuplicatePermission(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "admin_ui"}, {Name: "admin_ui"}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for duplicate permission entries, got nil")
	}
}

func TestManifestAllowsNetworkHostDeniesWithoutNetworkPermission(t *testing.T) {
	m := validManifest()
	if m.AllowsNetworkHost("api.stripe.com") {
		t.Fatal("expected deny: manifest declares no network permission at all")
	}
}

func TestManifestAllowsNetworkHostAllowsExactAllowlistedHost(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network", Args: []string{"api.stripe.com"}}}
	if !m.AllowsNetworkHost("api.stripe.com") {
		t.Fatal("expected allow: host is in the declared allowlist")
	}
}

func TestManifestAllowsNetworkHostDeniesUnlistedHost(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network", Args: []string{"api.stripe.com"}}}
	if m.AllowsNetworkHost("evil.example.com") {
		t.Fatal("expected deny: host is not in the declared allowlist")
	}
}

func TestManifestAllowsNetworkHostDeniesSubdomainOfAllowlistedHost(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network", Args: []string{"api.stripe.com"}}}
	if m.AllowsNetworkHost("evil.api.stripe.com") {
		t.Fatal("expected deny: matching is exact-hostname only, no subdomain/wildcard matching")
	}
}

func TestManifestAllowsNetworkHostIsCaseInsensitive(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network", Args: []string{"api.stripe.com"}}}
	if !m.AllowsNetworkHost("API.STRIPE.COM") {
		t.Fatal("expected allow: hostname matching should be case-insensitive")
	}
}

func TestManifestAllowsNetworkHostDeniesEmptyHost(t *testing.T) {
	m := validManifest()
	m.Permissions = []sdk.Permission{{Name: "network", Args: []string{"api.stripe.com"}}}
	if m.AllowsNetworkHost("") {
		t.Fatal("expected deny: empty host must never be allowed")
	}
}

// validManifest returns a minimally well-formed manifest for tests that
// only care about one specific field under test. Requires.Core is
// deliberately ">=0.1.0", not ">=1.0.0" as the PRD's own §7.3 example
// shows — it must be satisfiable by the real running kernel.Version (see
// pkg/kernel) for NewHostAPI's requires.core enforcement (slice 2.6) to
// accept it; ">=1.0.0" would be rejected by today's actual pre-1.0 kernel.
func validManifest() sdk.Manifest {
	return sdk.Manifest{
		Name:    "forms",
		Version: "1.0.0",
		Runtime: sdk.RuntimeWASM,
		Requires: sdk.Requires{
			Core:     ">=0.1.0",
			Contract: "content-composition/v0",
		},
	}
}
