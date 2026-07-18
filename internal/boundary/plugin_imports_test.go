package boundary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginKernelImportsCheck_VacuousOnRealRepo documents, rather than
// merely asserts, that invariant 1 is currently dormant: no plugins/ or
// themes/ directory exists in this repo yet (Phase 1 is headless-only), so
// the checker has nothing to walk and trivially reports zero violations.
// This is NOT the same claim as "enforced" — see
// TestPluginKernelImportsCheck_CatchesViolation for proof the logic works.
func TestPluginKernelImportsCheck_VacuousOnRealRepo(t *testing.T) {
	root := repoRoot(t)

	for _, treeRoot := range pluginTreeRoots {
		if _, err := os.Stat(filepath.Join(root, treeRoot)); err == nil {
			t.Fatalf("expected no %q directory in Phase 1 — if this now exists, invariant 1 is no longer dormant and this test (and the completed tracking doc) need updating", treeRoot)
		}
	}

	violations, err := CheckPluginKernelImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations against the real (plugin-less) repo, got: %v", violations)
	}
}

// TestPluginKernelImportsCheck_CatchesViolation proves the checker's logic
// actually catches a violation, using a synthetic fixture tree (not checked
// into the repo) faking a plugin that imports a kernel-internal package
// alongside a sanctioned pkg/sdk import.
func TestPluginKernelImportsCheck_CatchesViolation(t *testing.T) {
	root := t.TempDir()

	pluginDir := filepath.Join(root, "plugins", "badplugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	src := `package badplugin

import (
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/pkg/sdk"
)

var _ = content.API{}
var _ = sdk.Client{}
`
	if err := os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := CheckPluginKernelImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly 1 violation (the internal/content import), got %d: %v", len(violations), violations)
	}
	if !strings.Contains(violations[0], "internal/content") {
		t.Errorf("expected violation to mention internal/content, got: %s", violations[0])
	}
	for _, v := range violations {
		if strings.Contains(v, "kernel path github.com/glyphux/glyphux/pkg/sdk") {
			t.Errorf("sanctioned pkg/sdk import must not be flagged, got: %s", v)
		}
	}
}

// TestPluginKernelImportsCheck_ThemesRootAlsoChecked confirms the checker
// also walks a themes/ root, using the same synthetic-fixture approach.
func TestPluginKernelImportsCheck_ThemesRootAlsoChecked(t *testing.T) {
	root := t.TempDir()

	themeDir := filepath.Join(root, "themes", "badtheme")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	src := `package badtheme

import "github.com/glyphux/glyphux/internal/identity"

var _ = identity.API{}
`
	if err := os.WriteFile(filepath.Join(themeDir, "theme.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := CheckPluginKernelImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly 1 violation, got %d: %v", len(violations), violations)
	}
	if !strings.Contains(violations[0], "internal/identity") {
		t.Errorf("expected violation to mention internal/identity, got: %s", violations[0])
	}
}
