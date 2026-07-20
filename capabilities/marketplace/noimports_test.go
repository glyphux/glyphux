package marketplace

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenNetworkImports is every stdlib package that could perform actual
// network I/O (as opposed to merely modeling addresses/URLs in memory).
// "net/url" is deliberately absent: it parses/builds URL strings without
// ever dialing anything, so it carries no phone-home risk.
var forbiddenNetworkImports = []string{
	"net",
	"net/http",
	"net/http/httputil",
	"net/rpc",
	"net/smtp",
	"net/textproto",
	"google.golang.org/grpc",
}

// TestNonTestSourceImportsNoNetworkingPackage is the structural half of the
// "no phone-home" proof (the behavioral half is
// entitlement_test.go's TestVerifyEntitlementDoesNotRequireNetworkAccess).
// It parses every non-test .go file in this package and asserts none of
// them import any networking package at all — not just that
// VerifyEntitlement happens not to call one at runtime, but that the
// capability to do so isn't even present in the package's import graph.
// Test files are deliberately excluded: entitlement_test.go itself
// legitimately imports net/http to construct the broken-transport proof.
func TestNonTestSourceImportsNoNetworkingPackage(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob source files: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no source files found in this package — glob pattern likely broken")
	}

	forbidden := make(map[string]bool, len(forbiddenNetworkImports))
	for _, pkg := range forbiddenNetworkImports {
		forbidden[pkg] = true
	}

	checked := 0
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if forbidden[importPath] {
				t.Errorf("%s imports %q — this package must perform no network I/O in its non-test source (PRD §12.5 no-phone-home requirement)", path, importPath)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test source files were checked — glob pattern or _test.go filter is likely broken")
	}
}
