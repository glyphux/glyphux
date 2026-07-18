// Invariant 1 of PRD §17.2's boundary-verify gate: no plugin/theme code
// imports kernel internals — the only sanctioned surfaces are pkg/sdk and
// pkg/contract.
//
// PHASE-2 STATUS: no plugin or theme system exists yet (Phase 1 is
// headless-only), so this checker is written and tested against synthetic
// fixtures now, ahead of need, and is vacuously passing on today's real
// tree because the plugins/ and themes/ roots it looks for don't exist.
// It's ready the day Phase 2 adds them. Do not fabricate a real plugins/ or
// themes/ directory just to give this something to check.
package boundary

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// pluginModulePrefix is this module's import path — anything under it other
// than the sanctioned surfaces below counts as "kernel internals" for the
// purpose of this check.
const pluginModulePrefix = "github.com/glyphux/glyphux/"

// sanctionedPluginImportPrefixes are the only glyphux-module import paths
// plugin/theme code may use. A subpackage of either (e.g. pkg/sdk/http) is
// also allowed.
var sanctionedPluginImportPrefixes = []string{
	pluginModulePrefix + "pkg/sdk",
	pluginModulePrefix + "pkg/contract",
}

// pluginTreeRoots are the top-level directory names, relative to the repo
// root, that Phase 2 is expected to use for first-party and marketplace
// plugin/theme code living in-tree. A directory that doesn't exist is
// silently skipped (see PHASE-2 STATUS above) rather than treated as an
// error.
var pluginTreeRoots = []string{"plugins", "themes"}

// CheckPluginKernelImports walks each existing root in pluginTreeRoots
// beneath repoRoot and reports every .go file that imports a
// github.com/glyphux/glyphux/... package outside sanctionedPluginImportPrefixes.
func CheckPluginKernelImports(repoRoot string) ([]string, error) {
	var violations []string

	for _, treeRoot := range pluginTreeRoots {
		root := filepath.Join(repoRoot, treeRoot)
		if _, err := os.Stat(root); err != nil {
			// No such directory yet — nothing to check (see PHASE-2 STATUS).
			continue
		}

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)

			for _, imp := range f.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(importPath, pluginModulePrefix) {
					continue // not a glyphux-module import at all
				}
				if isSanctionedPluginImport(importPath) {
					continue
				}
				violations = append(violations, fmt.Sprintf(
					"%s: plugin/theme code imports kernel path %s (only pkg/sdk and pkg/contract are sanctioned)",
					rel, importPath,
				))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return violations, nil
}

func isSanctionedPluginImport(importPath string) bool {
	for _, prefix := range sanctionedPluginImportPrefixes {
		if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
			return true
		}
	}
	return false
}
