// CORE-02 REVIEW (behavior-first): release artifact contract.
//
// Given a set of build outputs (dummy glyphux / glyphuxd binaries per
// platform + a VERSION file), When the release packager runs, Then it
// produces glyphux-<ver>-<goos>-<goarch>.zip archives AND a SHA256SUMS
// listing every zip with a 64-hex, non-zero SHA-256 — and repeated runs
// produce byte-identical archives (deterministic).
//
// Exercises the real internal/release.Package + WriteSHA256SUMS the
// cmd/glyphux-release command drives; fake binaries only, no builds.
package release

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// reviewVersion reads the repo-root VERSION file (single source).
func reviewVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read repo VERSION: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

func reviewWriteBin(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("fake binary "+name), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func reviewSHA256Hex(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// CORE-MED-1 REVIEW (behavior-first): the release workflow's signing-key
// import must FAIL CLOSED on a PARTIAL signing configuration. Signing needs
// BOTH GLYPHUX_RELEASE_GPG_KEY (the key id) and
// GLYPHUX_RELEASE_GPG_PRIVATE_KEY (the armored secret). If only one of the
// two is configured, a two-way branch would silently fall into dev mode
// ("no signing key configured") and PUBLISH AN UNSIGNED manifest while the
// operator believes releases are signed. This static scan asserts release.yml
// carries an explicit partial-config guard (elif ... || ...; exit 1;
// ::error::) placed BETWEEN the both-set import branch and the none-set
// dev-mode fallback.
func TestReviewReleaseWorkflowSigningFailsClosedOnPartialConfig(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	// Scope the scan to the import step (between its heading and the next
	// step) so "exit 1" / "::error::" matches cannot come from elsewhere.
	stepStart := strings.Index(text, "Import release signing key")
	stepEnd := strings.Index(text, "Package release artifacts")
	if stepStart < 0 || stepEnd < 0 || stepEnd <= stepStart {
		t.Fatalf("release.yml: cannot locate the signing-key import step region (%d..%d)", stepStart, stepEnd)
	}
	step := text[stepStart:stepEnd]
	t.Logf("Given %s (canonical release path), import step region", path)
	t.Logf("When  only GLYPHUX_RELEASE_GPG_KEY or only GLYPHUX_RELEASE_GPG_PRIVATE_KEY is configured")

	both := `[ -n "${GLYPHUX_RELEASE_GPG_KEY:-}" ] && [ -n "${GLYPHUX_RELEASE_GPG_PRIVATE_KEY:-}" ]`
	partialA := `[ -n "${GLYPHUX_RELEASE_GPG_KEY:-}" ] || [ -n "${GLYPHUX_RELEASE_GPG_PRIVATE_KEY:-}" ]`
	partialB := `[ -n "${GLYPHUX_RELEASE_GPG_PRIVATE_KEY:-}" ] || [ -n "${GLYPHUX_RELEASE_GPG_KEY:-}" ]`
	if !strings.Contains(step, both) {
		t.Errorf("FAIL: import step has no both-set guard (want `[ -n KEY ] && [ -n PRIVATE ]` importing the key)")
	} else {
		t.Logf("  evidence: both-set guard imports the key (&&)")
	}
	if !strings.Contains(step, partialA) && !strings.Contains(step, partialB) {
		t.Errorf("FAIL: import step has NO partial-config guard (want an elif `[ -n KEY ] || [ -n PRIVATE ]` that fails the run) — exactly-one-secret-set would silently fall through to dev mode and publish an unsigned manifest")
	} else {
		t.Logf("  evidence: partial-config guard rejects exactly-one-set (||) with exit 1")
	}
	if !strings.Contains(step, "exit 1") {
		t.Errorf("FAIL: partial-config branch does not fail the run (no `exit 1`)")
	}
	if !strings.Contains(step, "::error::") {
		t.Errorf("FAIL: partial-config branch does not annotate the failure (no `::error::`)")
	}
	iBoth := strings.Index(step, both)
	iPartial := strings.Index(step, partialA)
	if iPartial < 0 {
		iPartial = strings.Index(step, partialB)
	}
	iDev := strings.Index(step, "no signing key configured")
	if iBoth < 0 || iPartial < 0 || iDev < 0 || !(iBoth < iPartial && iPartial < iDev) {
		t.Errorf("FAIL: import-step branch order is wrong — want both-set guard < partial-config guard < dev-mode fallback (got indexes %d, %d, %d)", iBoth, iPartial, iDev)
	} else {
		t.Logf("PASS: both-set -> partial-config -> dev-mode fallback (fail-closed on partial config)")
	}
}

// matrix mirrors cmd/glyphux-release's platform set.
var reviewMatrix = []struct{ goos, goarch string }{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "arm64"},
	{"darwin", "amd64"},
	{"windows", "amd64"},
}

// TestReviewReleaseArtifactContract packages the full matrix with fake
// binaries and asserts the contract: archive naming, SHA256SUMS
// completeness (64-hex, non-zero), and byte-identical determinism.
func TestReviewReleaseArtifactContract(t *testing.T) {
	version := reviewVersion(t)

	t.Run("Given dummy binaries for the 5-platform matrix and a VERSION file, When Package+WriteSHA256SUMS run, Then archives are named glyphux-<ver>-<goos>-<goarch>.zip", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "dist")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		versionFile := filepath.Join(dir, "VERSION")
		if err := os.WriteFile(versionFile, []byte(version+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		wantNames := map[string]bool{}
		for _, p := range reviewMatrix {
			ext := ""
			if p.goos == "windows" {
				ext = ".exe"
			}
			bins := []Bin{
				{Name: "glyphux", Path: reviewWriteBin(t, dir, "glyphux-"+p.goos+"-"+p.goarch+ext)},
				{Name: "glyphuxd", Path: reviewWriteBin(t, dir, "glyphuxd-"+p.goos+"-"+p.goarch+ext)},
			}
			arch, err := Package(context.Background(), Options{
				VersionFile: versionFile,
				GOOS:        p.goos,
				GOARCH:      p.goarch,
				Bins:        bins,
				OutDir:      out,
			})
			if err != nil {
				t.Fatalf("Package(%s/%s): %v", p.goos, p.goarch, err)
			}
			want := "glyphux-" + version + "-" + p.goos + "-" + p.goarch + ".zip"
			if filepath.Base(arch) != want {
				t.Errorf("FAIL: archive = %q, want %q", filepath.Base(arch), want)
			} else {
				t.Logf("PASS: archive named %s", filepath.Base(arch))
			}
			wantNames[want] = true
		}
		// Every matrix platform produced an archive.
		matches, err := filepath.Glob(filepath.Join(out, "*.zip"))
		if err != nil || len(matches) != len(reviewMatrix) {
			t.Errorf("FAIL: dist has %d zips (err %v), want %d", len(matches), err, len(reviewMatrix))
		}
		for name := range wantNames {
			if _, err := os.Stat(filepath.Join(out, name)); err != nil {
				t.Errorf("FAIL: expected archive %s missing", name)
			}
		}
	})

	t.Run("Given the archives, When SHA256SUMS is written, Then every zip is listed with a 64-hex non-zero SHA-256", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "dist")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		versionFile := filepath.Join(dir, "VERSION")
		if err := os.WriteFile(versionFile, []byte(version+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, p := range reviewMatrix {
			ext := ""
			if p.goos == "windows" {
				ext = ".exe"
			}
			if _, err := Package(context.Background(), Options{
				VersionFile: versionFile,
				GOOS:        p.goos,
				GOARCH:      p.goarch,
				Bins: []Bin{
					{Name: "glyphux", Path: reviewWriteBin(t, dir, "glyphux-"+p.goos+"-"+p.goarch+ext)},
					{Name: "glyphuxd", Path: reviewWriteBin(t, dir, "glyphuxd-"+p.goos+"-"+p.goarch+ext)},
				},
				OutDir: out,
			}); err != nil {
				t.Fatalf("Package(%s/%s): %v", p.goos, p.goarch, err)
			}
		}
		sumsPath, err := WriteSHA256SUMS(out)
		if err != nil {
			t.Fatalf("WriteSHA256SUMS: %v", err)
		}
		raw, _ := os.ReadFile(sumsPath)
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		hexRe := regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
		listed := map[string]string{} // name -> hash
		for _, ln := range lines {
			fields := strings.Fields(ln)
			if len(fields) != 2 {
				t.Errorf("FAIL: SHA256SUMS line %q is not '<hex>  <name>'", ln)
				continue
			}
			if !hexRe.MatchString(fields[0]) {
				t.Errorf("FAIL: SHA256SUMS hash %q for %s is not 64-char hex", fields[0], fields[1])
				continue
			}
			if strings.Trim(fields[0], "0") == "" {
				t.Errorf("FAIL: SHA256SUMS hash for %s is all-zeros (unverified artifact)", fields[1])
			}
			listed[filepath.Base(fields[1])] = fields[0]
		}
		// Every archive present and hash correct by re-hashing.
		archives, _ := filepath.Glob(filepath.Join(out, "*.zip"))
		if len(archives) != len(reviewMatrix) {
			t.Errorf("FAIL: dist has %d archives, want %d", len(archives), len(reviewMatrix))
		}
		for _, a := range archives {
			name := filepath.Base(a)
			h, ok := listed[name]
			if !ok {
				t.Errorf("FAIL: SHA256SUMS does not list %s", name)
				continue
			}
			if h != reviewSHA256Hex(t, a) {
				t.Errorf("FAIL: SHA256SUMS hash for %s = %s, re-hash = %s", name, h, reviewSHA256Hex(t, a))
			} else {
				t.Logf("PASS: SHA256SUMS lists %s with %s", name, h)
			}
		}
		if len(lines) != len(reviewMatrix) {
			t.Errorf("FAIL: SHA256SUMS has %d lines, want %d (one per archive)", len(lines), len(reviewMatrix))
		}
	})

	t.Run("Given identical inputs, When Package runs twice, Then the archives are byte-identical (deterministic)", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "dist")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		versionFile := filepath.Join(dir, "VERSION")
		if err := os.WriteFile(versionFile, []byte(version+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		bins := []Bin{
			{Name: "glyphux", Path: reviewWriteBin(t, dir, "glyphux-linux-amd64")},
			{Name: "glyphuxd", Path: reviewWriteBin(t, dir, "glyphuxd-linux-amd64")},
		}
		opts := Options{VersionFile: versionFile, GOOS: "linux", GOARCH: "amd64", Bins: bins, OutDir: out}
		first, err := Package(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Package(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		b1, _ := os.ReadFile(first)
		b2, _ := os.ReadFile(second)
		if !bytes.Equal(b1, b2) {
			t.Errorf("FAIL: two packaging runs differ (%d vs %d bytes) — must be deterministic (no timestamps)", len(b1), len(b2))
		} else {
			t.Logf("PASS: two runs byte-identical (%d bytes)", len(b1))
		}
		// Also confirm the archive's entries are the contract set (bins + VERSION).
		zr, err := zip.OpenReader(first)
		if err != nil {
			t.Fatal(err)
		}
		defer zr.Close()
		var names []string
		for _, f := range zr.File {
			names = append(names, f.Name)
		}
		sort.Strings(names)
		want := []string{"VERSION", "glyphux", "glyphuxd"}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("FAIL: entries = %v, want %v", names, want)
		} else {
			t.Logf("PASS: archive entries = %v (bins + VERSION)", names)
		}
	})
}
