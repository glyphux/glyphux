package boundary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCapabilityDeclarationCheck_VacuousOnRealRepo documents that invariant
// 4 is currently dormant: there is no manifest file and no plugin/theme
// code anywhere in this repo to scan, since Phase 1 has no manifest system.
// This is deliberately not asserted as "no violations found" against a real
// manifest — there is no real manifest to point the checker at yet.
func TestCapabilityDeclarationCheck_VacuousOnRealRepo(t *testing.T) {
	root := repoRoot(t)
	for _, treeRoot := range pluginTreeRoots {
		if _, err := os.Stat(filepath.Join(root, treeRoot)); err == nil {
			t.Fatalf("expected no %q directory in Phase 1 — invariant 4 needs re-evaluating against real manifests now", treeRoot)
		}
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
