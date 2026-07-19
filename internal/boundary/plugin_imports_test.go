package boundary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPluginKernelImportsCheck_EnforcedOnRealThemesTree proves invariant 1
// is no longer dormant: Phase 4 slice 4.3 added a real themes/ tree
// (themes/headless, themes/starter), so this now runs the real checker
// against real, shipped production code instead of merely documenting an
// empty tree (see docs/implementation/completed/0027 for the full
// account). plugins/ remains genuinely absent (no plugin system loads
// in-tree plugin code yet), so this only has real themes/ code to prove
// itself against so far — that's still a real assertion, not a vacuous
// one, because themes/headless and themes/starter are real, non-trivial
// packages that could easily have imported internal/content or another
// kernel-internal package directly (and did, in an earlier draft of this
// slice, before being reworked specifically to avoid it).
func TestPluginKernelImportsCheck_EnforcedOnRealThemesTree(t *testing.T) {
	root := repoRoot(t)

	if _, err := os.Stat(filepath.Join(root, "themes")); err != nil {
		t.Fatalf("expected a real themes/ directory to exist (Phase 4 slice 4.3) — got: %v", err)
	}

	violations, err := CheckPluginKernelImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations against the real repo's themes/ tree, got: %v", violations)
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
