package kernel_test

import (
	"testing"

	"golang.org/x/mod/semver"

	"github.com/glyphux/glyphux/pkg/kernel"
)

// TestVersionIsValidSemver guards against a future hand-edit of Version
// (see kernel.go's doc comment: it is bumped by hand, not derived) breaking
// pkg/sdk's requires.core enforcement, which assumes kernel.Version parses
// as a valid semantic version.
func TestVersionIsValidSemver(t *testing.T) {
	if !semver.IsValid("v" + kernel.Version) {
		t.Fatalf("kernel.Version %q is not a valid semantic version", kernel.Version)
	}
}
