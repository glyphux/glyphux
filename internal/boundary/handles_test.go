package boundary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestNoRawHandleLeakageInDomainAPIs is the real, enforced-today check:
// invariant 2 against the actual Phase-1 domain-API packages.
func TestNoRawHandleLeakageInDomainAPIs(t *testing.T) {
	root := repoRoot(t)
	cfg := &packages.Config{Dir: root}

	violations, err := CheckRawHandleLeakage(cfg, domainAPIPackages...)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Errorf("raw handle leakage found in domain APIs:\n  %s", strings.Join(violations, "\n  "))
	}
}

// TestRawHandleLeakageChecker_CatchesViolation proves the checker itself
// works, rather than merely observing that today's code happens to be
// clean: it builds a synthetic fixture module (a standalone temp Go module,
// not checked into the repo, so it can import database/sql freely without
// tripping the unrelated TestNoRawSQLOutsideDBPackage walk) with a
// deliberately leaky exported API, and asserts the checker flags exactly
// the leaky exports and nothing else.
func TestRawHandleLeakageChecker_CatchesViolation(t *testing.T) {
	dir := t.TempDir()
	writeLeakyFixtureModule(t, dir)

	cfg := &packages.Config{Dir: dir}
	violations, err := CheckRawHandleLeakage(cfg, "./...")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) == 0 {
		t.Fatal("expected the checker to flag the synthetic leaky fixture, but it found no violations")
	}

	joined := strings.Join(violations, "\n")
	for _, want := range []string{
		"LeakyParam",
		"LeakyResult",
		"LeakyFile",
		"API.LeakyMethod",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a violation mentioning %q, got:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "Clean") {
		t.Errorf("checker flagged the non-leaky Clean export as a violation:\n%s", joined)
	}
}

// writeLeakyFixtureModule writes a standalone Go module under dir with one
// package containing a mix of exported functions/methods that leak raw
// database/sql and os handles, and one that doesn't.
func writeLeakyFixtureModule(t *testing.T, dir string) {
	t.Helper()

	goMod := "module leakyfixture\n\ngo 1.23\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	src := `package leakyfixture

import (
	"database/sql"
	"os"
)

// LeakyParam accepts a raw *sql.DB — a violation.
func LeakyParam(db *sql.DB) error { return nil }

// LeakyResult returns a raw *sql.Rows — a violation.
func LeakyResult() *sql.Rows { return nil }

// LeakyFile returns a raw *os.File — a violation.
func LeakyFile() *os.File { return nil }

// Clean takes and returns only ordinary types — not a violation.
func Clean(x int) string { return "" }

// API is an exported type with both a leaky and a clean method.
type API struct{}

// LeakyMethod accepts a raw *sql.Tx — a violation.
func (a *API) LeakyMethod(tx *sql.Tx) error { return nil }

// CleanMethod takes and returns only ordinary types — not a violation.
func (a *API) CleanMethod(x int) int { return x }
`
	if err := os.WriteFile(filepath.Join(dir, "leaky.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
