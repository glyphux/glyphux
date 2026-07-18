// Invariant 4 of PRD §17.2's boundary-verify gate: every capability used by
// plugin/theme code must be declared in that code's manifest.
//
// PHASE-2 STATUS: no manifest system exists yet — there is no real manifest
// schema, no real capability-request API in pkg/sdk (which doesn't exist
// either yet), and no plugin/theme code to scan. This file therefore
// defines a minimal, explicitly provisional manifest shape (ManifestStub)
// and a provisional capability-request call convention
// (capabilityCallName), sufficient to exercise the checker's logic against
// synthetic fixtures now. Both MUST be replaced with the real schema and
// real pkg/sdk call convention when Phase 2 designs them — nothing here is
// meant to survive as the actual manifest format. Until then this checker
// is written and tested, but dormant: it has nothing real to scan.
package boundary

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ManifestStub is a provisional, minimal shape for a capability manifest —
// just enough to declare which capability names a plugin/theme is allowed
// to use. NOT the Phase-2 manifest schema; replace when that's designed.
type ManifestStub struct {
	Capabilities []string `json:"capabilities"`
}

// capabilityCallName is the provisional method name this checker treats as
// "requesting a capability at runtime" (e.g. sdk.RequireCapability("cms.read")).
// This is a stand-in for pkg/sdk's real capability-request API, which
// doesn't exist yet; update this constant when Phase 2 lands it.
const capabilityCallName = "RequireCapability"

// LoadManifestStub reads and parses a ManifestStub from a JSON file at path.
func LoadManifestStub(path string) (ManifestStub, error) {
	var m ManifestStub
	data, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("boundary: reading manifest %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("boundary: parsing manifest %s: %w", path, err)
	}
	return m, nil
}

// CheckCapabilityDeclarations walks the .go files under codeDir looking for
// calls of the form `<x>.RequireCapability("name")` (see capabilityCallName)
// and reports every capability name used that manifest.Capabilities doesn't
// declare.
func CheckCapabilityDeclarations(codeDir string, manifest ManifestStub) ([]string, error) {
	declared := make(map[string]bool, len(manifest.Capabilities))
	for _, c := range manifest.Capabilities {
		declared[c] = true
	}

	var violations []string
	err := filepath.WalkDir(codeDir, func(path string, d fs.DirEntry, err error) error {
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
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(codeDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != capabilityCallName {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			capName, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if !declared[capName] {
				violations = append(violations, fmt.Sprintf(
					"%s: uses capability %q not declared in manifest", rel, capName,
				))
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return violations, nil
}
