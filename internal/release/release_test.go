// Ticket T10c RED: the release artifact contract, behavior-first. Fake
// binaries + a temp dir — no real builds, no GitHub.
package release

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// committedVersion reads the repo-root VERSION file — the single source
// (internal/release is two levels below the root).
func committedVersion(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatalf("read repo VERSION: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

// writeBin writes a fake binary and returns its path.
func writeBin(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func zipEntryNames(t *testing.T, zipPath string) []string {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

func zipRead(t *testing.T, zipPath, entry string) string {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == entry {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			buf := new(bytes.Buffer)
			if _, err := buf.ReadFrom(rc); err != nil {
				t.Fatal(err)
			}
			return buf.String()
		}
	}
	t.Fatalf("zip %s has no entry %q (entries: %v)", zipPath, entry, zipEntryNames(t, zipPath))
	return ""
}

func sha256hex(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// --- Acceptance criterion (a): the archive's entries are exactly the
// expected set — binaries + VERSION — in a deterministic order (bins
// sorted by name, VERSION last), and two packaging runs are
// byte-identical (no timestamps). ---

func TestPackageProducesDeterministicArchive(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	glyphuxd := writeBin(t, binDir, "glyphuxd", "fake daemon binary")
	glyphux := writeBin(t, binDir, "glyphux", "fake cli binary")
	versionFile := filepath.Join(dir, "VERSION")
	if err := os.WriteFile(versionFile, []byte("0.2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		VersionFile: versionFile,
		GOOS:        "linux",
		GOARCH:      "amd64",
		Bins:        []Bin{{Name: "glyphuxd", Path: glyphuxd}, {Name: "glyphux", Path: glyphux}},
		OutDir:      out,
	}

	first, err := Package(context.Background(), opts)
	if err != nil {
		t.Fatalf("Package: %v", err)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("archive %s not written: %v", first, err)
	}
	want := []string{"glyphux", "glyphuxd", "VERSION"}
	if got := zipEntryNames(t, first); !equalStrings(got, want) {
		t.Errorf("entries = %v, want exactly %v (bins sorted, VERSION last)", got, want)
	}
	// Determinism: a second packaging run is byte-identical.
	second, err := Package(context.Background(), opts)
	if err != nil {
		t.Fatalf("Package (2nd): %v", err)
	}
	b1, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Errorf("archives differ between runs — must be deterministic (no timestamps)")
	}
}

// --- Acceptance criterion (b): the archive's VERSION entry equals the
// committed repo VERSION file content (asserted against the file, not
// hardcoded). ---

func TestPackageEmbedsCommittedVersion(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	versionFile, err := filepath.Abs(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		VersionFile: versionFile,
		GOOS:        "linux",
		GOARCH:      "arm64",
		Bins:        []Bin{{Name: "glyphux", Path: writeBin(t, binDir, "glyphux", "cli")}},
		OutDir:      out,
	}

	arch, err := Package(context.Background(), opts)
	if err != nil {
		t.Fatalf("Package: %v", err)
	}
	want := committedVersion(t)
	if got := zipRead(t, arch, "VERSION"); got != want {
		t.Errorf("VERSION entry = %q, want committed VERSION %q (single source)", got, want)
	}
}

// --- Acceptance criterion (c): SHA256SUMS lists every archive with the
// correct sha256 (verified by re-hashing the archives). ---

func TestWriteSHA256SUMSListsCorrectHashes(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	versionFile := filepath.Join(dir, "VERSION")
	if err := os.WriteFile(versionFile, []byte("0.2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct{ goos, goarch string }{
		{"linux", "amd64"},
		{"windows", "amd64"},
	} {
		if _, err := Package(context.Background(), Options{
			VersionFile: versionFile,
			GOOS:        p.goos,
			GOARCH:      p.goarch,
			Bins: []Bin{
				{Name: "glyphuxd", Path: writeBin(t, binDir, "glyphuxd-"+p.goos, "daemon")},
				{Name: "glyphux", Path: writeBin(t, binDir, "glyphux-"+p.goos, "cli")},
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
	raw, err := os.ReadFile(sumsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("SHA256SUMS has %d lines, want 2 (one per archive):\n%s", len(lines), raw)
	}
	archives, err := filepath.Glob(filepath.Join(out, "*.zip"))
	if err != nil || len(archives) != 2 {
		t.Fatalf("dist has %d archives (err %v), want 2", len(archives), err)
	}
	got := make(map[string]string) // name → hex from SHA256SUMS
	for _, ln := range lines {
		fields := strings.Fields(ln)
		if len(fields) != 2 || len(fields[0]) != 64 {
			t.Fatalf("SHA256SUMS line %q is not <hex>  <name>", ln)
		}
		got[filepath.Base(fields[1])] = fields[0]
	}
	for _, a := range archives {
		name := filepath.Base(a)
		hexHash, ok := got[name]
		if !ok {
			t.Errorf("SHA256SUMS missing %s", name)
			continue
		}
		if hexHash != sha256hex(t, a) {
			t.Errorf("SHA256SUMS hash for %s = %s, want %s (re-hash)", name, hexHash, sha256hex(t, a))
		}
	}
}

// --- Acceptance criterion (d): the helper fails fast on a missing binary
// or a missing VERSION file. ---

func TestPackageFailsFastOnMissingBinary(t *testing.T) {
	dir := t.TempDir()
	versionFile := filepath.Join(dir, "VERSION")
	if err := os.WriteFile(versionFile, []byte("0.2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Package(context.Background(), Options{
		VersionFile: versionFile,
		GOOS:        "linux",
		GOARCH:      "amd64",
		Bins:        []Bin{{Name: "glyphuxd", Path: filepath.Join(dir, "does-not-exist")}},
		OutDir:      out,
	})
	if err == nil {
		t.Fatal("Package must fail fast on a missing binary")
	}
	if !strings.Contains(err.Error(), "glyphuxd") {
		t.Errorf("error %q does not name the missing binary", err)
	}
}

func TestPackageFailsFastOnMissingVersionFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Package(context.Background(), Options{
		VersionFile: filepath.Join(dir, "no-VERSION-here"),
		GOOS:        "linux",
		GOARCH:      "amd64",
		Bins:        []Bin{{Name: "glyphux", Path: writeBin(t, dir, "glyphux", "cli")}},
		OutDir:      out,
	})
	if err == nil {
		t.Fatal("Package must fail fast on a missing VERSION file")
	}
	if !strings.Contains(err.Error(), "VERSION") {
		t.Errorf("error %q does not name VERSION", err)
	}
}

// --- Acceptance criterion (e): Windows archives name the binary entries
// with the .exe extension. ---

func TestPackageWindowsEntriesUseExeExtension(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "dist")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	versionFile := filepath.Join(dir, "VERSION")
	if err := os.WriteFile(versionFile, []byte("0.2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	arch, err := Package(context.Background(), Options{
		VersionFile: versionFile,
		GOOS:        "windows",
		GOARCH:      "amd64",
		Bins: []Bin{
			{Name: "glyphuxd", Path: writeBin(t, binDir, "glyphuxd.exe", "daemon")},
			{Name: "glyphux", Path: writeBin(t, binDir, "glyphux.exe", "cli")},
		},
		OutDir: out,
	})
	if err != nil {
		t.Fatalf("Package: %v", err)
	}
	want := []string{"glyphux.exe", "glyphuxd.exe", "VERSION"}
	if got := zipEntryNames(t, arch); !equalStrings(got, want) {
		t.Errorf("entries = %v, want exactly %v (.exe on windows)", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
