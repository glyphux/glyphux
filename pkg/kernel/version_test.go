package kernel_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/pkg/kernel"
)

// TestVersionMatchesVERSIONFile is the sync mechanism between the committed
// repo-root VERSION file and kernel.Version's source default (see the var's
// doc comment in kernel.go). A release build ldflags-overrides the var
// (-X github.com/glyphux/glyphux/pkg/kernel.Version=...), which this test
// cannot see; under a plain `go build`/`go test` — the only place the two
// are comparable — they MUST agree. Bump the kernel version by editing the
// VERSION file AND the var together (or run the justfile `release` target,
// which injects the VERSION file's content via ldflags at build time).
func TestVersionMatchesVERSIONFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read repo-root VERSION: %v", err)
	}
	fileVersion := strings.TrimSpace(string(raw))
	if kernel.Version != fileVersion {
		t.Errorf("kernel.Version = %q, VERSION file = %q — edit both together (see kernel.go's doc comment)", kernel.Version, fileVersion)
	}
}

// TestVersionIsSemver proves kernel.Version is always a parseable
// MAJOR.MINOR.PATCH triple — the shape manifest requires.core constraints
// are matched against (pkg/sdk's coreSatisfied parses it as semver).
func TestVersionIsSemver(t *testing.T) {
	parts := strings.Split(kernel.Version, ".")
	if len(parts) != 3 {
		t.Fatalf("kernel.Version = %q, want MAJOR.MINOR.PATCH", kernel.Version)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			t.Errorf("kernel.Version component %d = %q, want a non-negative integer", i, p)
		}
	}
}
