// Package boundary enforces the Communication Law (§5.2): nothing above the
// kernel's database adapter may see a raw database/sql type. This is the
// first slice of the §17.2 boundary-verify CI gate — scoped for now to the
// database/sql rule; a general-purpose checker is separate future work.
package boundary

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// allowedRawSQLPackage is the only package permitted to import database/sql.
// Every other package reaches the database through internal/db's
// Queryer/Row/Rows/Result boundary instead.
const allowedRawSQLPackage = "internal/db"

func TestNoRawSQLOutsideDBPackage(t *testing.T) {
	root := repoRoot(t)

	var violations []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", ".claude", ".worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		relDir := filepath.ToSlash(filepath.Dir(rel))
		if relDir == allowedRawSQLPackage || strings.HasPrefix(relDir, allowedRawSQLPackage+"/") {
			return nil
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "database/sql" {
				violations = append(violations, rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(violations) > 0 {
		t.Errorf("database/sql must only be imported by %s; found in:\n  %s",
			allowedRawSQLPackage, strings.Join(violations, "\n  "))
	}
}

// repoRoot locates the module root from this test file's own path, so the
// walk works regardless of the working directory `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// internal/boundary/imports_test.go -> repo root
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
