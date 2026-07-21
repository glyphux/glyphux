package boundary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCapabilityDeclarationCheck_StillVacuousOnRealThemesTree re-evaluates
// invariant 4 now that Phase 4 slice 4.3 made themes/ real (previously this
// test asserted the directory didn't exist at all — see
// docs/implementation/completed/0027 for the full account). Invariant 4 is
// about a manifest declaring which capabilities plugin/theme code requests
// at runtime (capabilityCallName, "RequireCapability") — but themes have no
// such mechanism at all: PRD §9.1's hard law is that a theme is read-only
// and never mutates composition, so it never calls into any
// capability-gated API a manifest would need to declare (unlike a
// pkg/sdk.Plugin, which does). This is proven for real, not just asserted:
// scanning the actual themes/ tree with CheckCapabilityDeclarations
// against an empty manifest reports zero violations, because no
// RequireCapability-shaped call exists anywhere in themes/headless or
// themes/starter's real code. Invariant 4 therefore remains dormant for
// themes specifically — not because there's nothing to scan, but because
// nothing scanned uses the pattern it checks for — which is a materially
// different (and now real, not hypothetical) claim than before this slice.
func TestCapabilityDeclarationCheck_StillVacuousOnRealThemesTree(t *testing.T) {
	root := repoRoot(t)
	themesDir := filepath.Join(root, "themes")
	if _, err := os.Stat(themesDir); err != nil {
		t.Fatalf("expected a real themes/ directory to exist (Phase 4 slice 4.3) — got: %v", err)
	}

	violations, err := CheckCapabilityDeclarations(themesDir, ManifestStub{})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero RequireCapability-shaped calls anywhere under themes/ (themes have no capability-request mechanism, PRD §9.1), got: %v", violations)
	}
}

// TestCapabilityDeclarationCheck_CatchesViolation proves the checker's
// logic works against a synthetic fixture: a provisional manifest
// declaring one capability, and fixture code that uses both the declared
// capability and an undeclared one.
func TestCapabilityDeclarationCheck_CatchesViolation(t *testing.T) {
	dir := t.TempDir()

	manifest := ManifestStub{Capabilities: []string{"cms.read"}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	src := `package fixtureplugin

type sdk struct{}

func (s sdk) RequireCapability(name string) {}

func Run() {
	var s sdk
	s.RequireCapability("cms.read")  // declared, fine
	s.RequireCapability("cms.write") // NOT declared — violation
}
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifestStub(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	violations, err := CheckCapabilityDeclarations(dir, loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly 1 violation (cms.write undeclared), got %d: %v", len(violations), violations)
	}
	if !strings.Contains(violations[0], "cms.write") {
		t.Errorf("expected violation to mention cms.write, got: %s", violations[0])
	}
	if strings.Contains(violations[0], "cms.read") {
		t.Errorf("declared capability cms.read must not be flagged, got: %s", violations[0])
	}
}

// TestCapabilityDeclarationCheck_NoUndeclaredUses confirms the checker
// reports zero violations when every used capability is declared.
func TestCapabilityDeclarationCheck_NoUndeclaredUses(t *testing.T) {
	dir := t.TempDir()

	manifest := ManifestStub{Capabilities: []string{"cms.read", "cms.write"}}

	src := `package fixtureplugin

type sdk struct{}

func (s sdk) RequireCapability(name string) {}

func Run() {
	var s sdk
	s.RequireCapability("cms.read")
	s.RequireCapability("cms.write")
}
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := CheckCapabilityDeclarations(dir, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations, got: %v", violations)
	}
}
