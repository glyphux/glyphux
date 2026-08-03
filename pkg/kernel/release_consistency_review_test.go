// CORE-01 REVIEW (behavior-first): version consistency.
//
//  1. kernel.Version must always be a bare MAJOR.MINOR.PATCH triple
//     (regex ^\d+\.\d+\.\d+$) — no v prefix, no -dirty suffix. This is the
//     invariant pkg/kernel/version_test.go already enforces; this review
//     test re-asserts it so the suite runs as one unit.
//  2. STATIC SCAN of .github/workflows/release.yml: the canonical release
//     path must inject pkg/kernel.Version via ldflags (alongside
//     main.version) so release binaries report the tagged version.
//  3. STATIC SCAN of scripts/release/build.sh: the value injected into
//     pkg/kernel.Version must NOT be v-prefixed and must NOT carry a
//     -dirty suffix. The default must derive a bare semver (repo-root
//     VERSION file, or git-describe output stripped of v/-dirty/-NN-gHASH),
//     and a fail-closed guard must validate the final value.
package kernel_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/pkg/kernel"
)

// TestReviewKernelVersionIsBareSemver — the invariant version_test.go
// already enforces. It must PASS.
func TestReviewKernelVersionIsBareSemver(t *testing.T) {
	t.Run("Given the current kernel.Version, When parsed as MAJOR.MINOR.PATCH, Then it matches ^\\d+\\.\\d+\\.\\d+$", func(t *testing.T) {
		re := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
		got := kernel.Version
		t.Logf("Given kernel.Version = %q", got)
		t.Logf("When  we match it against ^\\d+\\.\\d+\\.\\d+$")
		if !re.MatchString(got) {
			t.Errorf("FAIL: kernel.Version = %q — want bare MAJOR.MINOR.PATCH (no v prefix, no -dirty suffix)", got)
			return
		}
		t.Logf("PASS: kernel.Version %q is a bare semver triple (no v prefix, no -dirty)", got)
	})
}

// TestReviewReleaseWorkflowInjectsKernelVersion — STATIC SCAN of
// .github/workflows/release.yml. The canonical release path must inject
// pkg/kernel.Version (not just main.version) with a BARE MAJOR.MINOR.PATCH:
// the tag is v0.2.0, so the workflow must derive a v-stripped VERSION (via
// GITHUB_REF_NAME#v into $GITHUB_ENV) once and inject THAT into both
// main.version and pkg/kernel.Version — consistent with
// scripts/release/build.sh and the pkg/kernel.Version invariant. Injecting
// ${{ github.ref_name }} verbatim (v-prefixed) would violate the invariant.
func TestReviewReleaseWorkflowInjectsKernelVersion(t *testing.T) {
	t.Run("Given the canonical release workflow, When it builds with ldflags, Then it injects BARE pkg/kernel.Version (leading v stripped from the tag)", func(t *testing.T) {
		path := filepath.Join("..", "..", ".github", "workflows", "release.yml")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		t.Logf("Given %s (canonical release path)", path)
		t.Logf("When  we inspect how VERSION is derived and injected")

		// 1. The workflow derives a BARE version: strip the leading v from
		//    the tag once, into $GITHUB_ENV, and reuse it everywhere.
		stripRe := regexp.MustCompile(`VERSION=\$\{GITHUB_REF_NAME#v\}`)
		if !stripRe.MatchString(text) {
			t.Errorf("FAIL: release.yml never derives a bare VERSION from the tag (want a 'VERSION=${GITHUB_REF_NAME#v}' step) — github.ref_name is v-prefixed")
		} else {
			t.Logf("  evidence (release.yml): VERSION=${GITHUB_REF_NAME#v} -> $GITHUB_ENV (v-prefixed tag normalized to bare semver)")
		}
		if strings.Contains(text, "VERSION: ${{ github.ref_name }}") {
			t.Errorf("FAIL: release.yml injects ${{ github.ref_name }} (v-prefixed) directly into the build env — must use the stripped VERSION from $GITHUB_ENV")
		} else {
			t.Logf("  evidence (release.yml): no direct 'VERSION: ${{ github.ref_name }}' env injection")
		}

		// 2. Every ldflags line injects BOTH vars from that stripped VERSION
		//    and never references github.ref_name directly.
		foundInjection := false
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, "ldflags") || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			t.Logf("  evidence (release.yml): %s", strings.TrimSpace(line))
			if strings.Contains(line, "github.ref_name") {
				t.Errorf("FAIL: an ldflags line references ${{ github.ref_name }} directly (v-prefixed); must use the stripped ${VERSION}")
			}
			if strings.Contains(line, "main.version=${VERSION}") && strings.Contains(line, "kernel.Version=${VERSION}") {
				foundInjection = true
			}
		}
		if !foundInjection {
			t.Errorf("FAIL: no ldflags line injects BOTH main.version=${VERSION} and pkg/kernel.Version=${VERSION} from the stripped VERSION")
		} else {
			t.Logf("PASS: ldflags inject main.version and pkg/kernel.Version from the bare ${VERSION}")
		}
	})
}

// TestReviewBuildScriptVersionIsBareSemver — STATIC SCAN of
// scripts/release/build.sh. The injected value must resolve to a bare
// MAJOR.MINOR.PATCH: the default reads the repo-root VERSION file, any
// git-describe fallback strips the v prefix / -NN-gHASH / -dirty suffixes,
// and a fail-closed guard validates the final value.
func TestReviewBuildScriptVersionIsBareSemver(t *testing.T) {
	t.Run("Given build.sh's pkg/kernel.Version injection, When resolved, Then it is not v-prefixed and not -dirty", func(t *testing.T) {
		path := filepath.Join("..", "..", "scripts", "release", "build.sh")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		t.Logf("Given %s", path)
		t.Logf("When  we parse the value injected into pkg/kernel.Version and how VERSION defaults")

		injectRe := regexp.MustCompile(`-X github\.com/glyphux/glyphux/pkg/kernel\.Version=([^ "]+)`)
		inject := injectRe.FindStringSubmatch(text)
		if inject == nil {
			t.Errorf("FAIL: no pkg/kernel.Version injection found in build.sh")
			return
		}
		t.Logf("  evidence (build.sh ldflags): -X github.com/glyphux/glyphux/pkg/kernel.Version=%s", inject[1])

		red := false
		if !strings.Contains(text, "tr -d '[:space:]' < VERSION") {
			red = true
			t.Errorf("FAIL: build.sh default does not read the repo-root VERSION file (the bare-semver source)")
		} else {
			t.Logf("  evidence (build.sh default): VERSION=\"$(tr -d '[:space:]' < VERSION)\" — repo-root VERSION file, bare MAJOR.MINOR.PATCH")
		}
		if strings.Contains(text, "git describe") {
			if !strings.Contains(text, "s/^v//") || !strings.Contains(text, "s/-dirty$//") {
				red = true
				t.Errorf("FAIL: build.sh falls back to `git describe` output without stripping the v prefix and -dirty suffix")
			} else {
				t.Logf("  evidence (build.sh fallback): git describe output is stripped via sed 's/^v//; s/-[0-9]+-g[0-9a-f]+$//; s/-dirty$//' before injection")
			}
		}
		if !strings.Contains(text, `^[0-9]+\.[0-9]+\.[0-9]+$`) {
			red = true
			t.Errorf("FAIL: build.sh lacks a fail-closed bare-semver guard on the final injected VERSION")
		} else {
			t.Logf("  evidence (build.sh guard): VERSION validated against ^[0-9]+\\.[0-9]+\\.[0-9]+$ before building")
		}
		if !strings.Contains(text, `"${VERSION#v}"`) {
			red = true
			t.Errorf("FAIL: build.sh does not normalize a v-prefixed explicit VERSION (want 'VERSION=\"${VERSION#v}\"' before the guard) — VERSION=v0.2.0 must become 0.2.0, not be rejected")
		} else {
			t.Logf("  evidence (build.sh normalize): an explicit VERSION=v0.2.0 is normalized to 0.2.0 (VERSION=\"${VERSION#v}\") before the fail-closed guard")
		}
		if !red {
			t.Logf("PASS: injected value is a bare semver (no v prefix, no -dirty suffix)")
		}
	})
}
