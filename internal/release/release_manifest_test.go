// I4-core (signed release metadata) — behavior-first tests for
// WriteReleaseManifest. Reuses the package's hermetic fixtures; the
// manifest must be deterministic (sorted, no timestamps), consistent with
// SHA256SUMS (one source of truth for hashes), optionally signed with the
// exact manifest bytes, and fail-closed on any divergence.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// capturingSigner records the exact bytes handed to Sign and returns a
// fixed signature, so tests can assert the .sig seals the exact manifest.
type capturingSigner struct {
	got []byte
	out []byte
	err error
}

func (c *capturingSigner) Sign(data []byte) ([]byte, error) {
	c.got = append([]byte(nil), data...)
	return append([]byte(nil), c.out...), c.err
}

func (c *capturingSigner) saw(data []byte) bool { return string(c.got) == string(data) }

// seedManifest builds a dist dir with 3 platform archives (windows
// deliberately "built" before the two linux ones, to prove the manifest
// sorts by goos+goarch) plus SHA256SUMS, returning (distDir, versionFile).
func seedManifest(t *testing.T) (dist, versionFile string) {
	t.Helper()
	version := "1.2.3"
	dir := t.TempDir()
	dist = filepath.Join(dir, "dist")
	binDir := filepath.Join(dir, "bin")
	for _, d := range []string{dist, binDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	versionFile = filepath.Join(dir, "VERSION")
	if err := os.WriteFile(versionFile, []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Shuffled: windows first, then linux/arm64, then linux/amd64.
	platforms := []struct{ goos, goarch string }{
		{"windows", "amd64"},
		{"linux", "arm64"},
		{"linux", "amd64"},
	}
	for i, p := range platforms {
		ext := ""
		if p.goos == "windows" {
			ext = ".exe"
		}
		b := filepath.Join(binDir, fmt.Sprintf("glyphux-%s-%s-%d%s", p.goos, p.goarch, i, ext))
		if err := os.WriteFile(b, []byte(fmt.Sprintf("fake binary payload %d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Package(context.Background(), Options{
			VersionFile: versionFile,
			GOOS:        p.goos,
			GOARCH:      p.goarch,
			Bins:        []Bin{{Name: "glyphux", Path: b}},
			OutDir:      dist,
		}); err != nil {
			t.Fatalf("Package(%s/%s): %v", p.goos, p.goarch, err)
		}
	}
	if _, err := WriteSHA256SUMS(dist); err != nil {
		t.Fatal(err)
	}
	return dist, versionFile
}

func readManifest(t *testing.T, dist string) (*Manifest, string) {
	t.Helper()
	path := filepath.Join(dist, "release-manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return &m, string(raw)
}

// S1: manifest content + sort + determinism.
func TestWriteReleaseManifest_ContentAndDeterminism(t *testing.T) {
	hexRe := regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

	t.Run("Given 3 packaged platforms plus SHA256SUMS, When WriteReleaseManifest runs, Then release-manifest.json lists each zip with its 64-hex sha256, byte-exact size, sorted by goos+goarch", func(t *testing.T) {
		dist, versionFile := seedManifest(t)
		manifestPath, sigPath, err := WriteReleaseManifest(context.Background(), ManifestOptions{
			OutDir:      dist,
			VersionFile: versionFile,
			Channel:     "", // must default to "stable"
		})
		if err != nil {
			t.Fatalf("WriteReleaseManifest: %v", err)
		}
		if filepath.Base(manifestPath) != "release-manifest.json" {
			t.Errorf("manifest path = %q, want release-manifest.json", filepath.Base(manifestPath))
		}
		if sigPath != "" {
			t.Errorf("no-signer run returned a sig path %q — no signature must be emitted", sigPath)
		}

		m, _ := readManifest(t, dist)
		if m.Version != "1.2.3" {
			t.Errorf("version = %q, want 1.2.3", m.Version)
		}
		if m.Channel != "stable" {
			t.Errorf("channel = %q, want default stable", m.Channel)
		}
		// Sorted by goos then goarch even though the input was shuffled.
		var got []string
		for _, p := range m.Platforms {
			got = append(got, p.GOOS+"/"+p.GOARCH)
		}
		want := []string{"linux/amd64", "linux/arm64", "windows/amd64"}
		if strings.Join(got, ", ") != strings.Join(want, ", ") {
			t.Errorf("platforms = %v, want sorted %v (deterministic order)", got, want)
		}
		for i, p := range m.Platforms {
			wantZip := fmt.Sprintf("glyphux-1.2.3-%s-%s.zip", p.GOOS, p.GOARCH)
			if p.Zip != wantZip {
				t.Errorf("platform %d zip = %q, want %q", i, p.Zip, wantZip)
			}
			if !hexRe.MatchString(p.SHA256) {
				t.Errorf("platform %d sha256 = %q is not 64-char hex", i, p.SHA256)
			}
			stat, err := os.Stat(filepath.Join(dist, p.Zip))
			if err != nil {
				t.Fatalf("stat %s: %v", p.Zip, err)
			}
			if p.Size != stat.Size() {
				t.Errorf("platform %d size = %d, want %d (byte-exact)", i, p.Size, stat.Size())
			}
			if p.SHA256 != sha256hex(t, filepath.Join(dist, p.Zip)) {
				t.Errorf("platform %d manifest sha256 != re-hash of %s", i, p.Zip)
			}
		}
		if len(m.Platforms) != 3 {
			t.Errorf("manifest lists %d platforms, want 3", len(m.Platforms))
		}
	})

	t.Run("Given identical inputs in two independent dirs, When WriteReleaseManifest runs twice, Then the manifest bytes are byte-identical (no timestamps/randomness)", func(t *testing.T) {
		dist1, v1 := seedManifest(t)
		if _, _, err := WriteReleaseManifest(context.Background(), ManifestOptions{OutDir: dist1, VersionFile: v1}); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		dist2, v2 := seedManifest(t)
		if _, _, err := WriteReleaseManifest(context.Background(), ManifestOptions{OutDir: dist2, VersionFile: v2}); err != nil {
			t.Fatalf("run 2: %v", err)
		}
		b1, _ := os.ReadFile(filepath.Join(dist1, "release-manifest.json"))
		b2, _ := os.ReadFile(filepath.Join(dist2, "release-manifest.json"))
		if string(b1) != string(b2) {
			t.Errorf("manifest differs across runs — must be deterministic (no timestamps/randomness)\nrun1:\n%s\nrun2:\n%s", b1, b2)
		}
		t.Logf("PASS: deterministic manifest, %d bytes identical across two independent runs", len(b1))
	})
}

// S2: manifest <-> SHA256SUMS consistency — one source of truth.
func TestWriteReleaseManifest_ConsistentWithSHA256SUMS(t *testing.T) {
	dist, versionFile := seedManifest(t)
	if _, _, err := WriteReleaseManifest(context.Background(), ManifestOptions{OutDir: dist, VersionFile: versionFile}); err != nil {
		t.Fatalf("WriteReleaseManifest: %v", err)
	}
	sumsRaw, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	sumsMap := map[string]string{}
	for _, ln := range strings.Split(strings.TrimSpace(string(sumsRaw)), "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 {
			sumsMap[f[1]] = f[0]
		}
	}
	m, _ := readManifest(t, dist)
	for _, p := range m.Platforms {
		entry, ok := sumsMap[p.Zip]
		if !ok {
			t.Errorf("manifest lists %s but SHA256SUMS does not", p.Zip)
			continue
		}
		if entry != p.SHA256 {
			t.Errorf("manifest sha256 for %s = %s, SHA256SUMS = %s — must be identical (one source of truth)", p.Zip, p.SHA256, entry)
		}
	}
	t.Log("PASS: every manifest sha256 equals its SHA256SUMS entry")
}

// S3: signing hook.
func TestWriteReleaseManifest_Signing(t *testing.T) {
	t.Run("Given a configured signer, When WriteReleaseManifest runs, Then it writes release-manifest.json.sig sealing the EXACT manifest bytes", func(t *testing.T) {
		dist, versionFile := seedManifest(t)
		s := &capturingSigner{out: []byte("FIXED-MOCK-SIGNATURE-BYTES")}
		_, sigPath, err := WriteReleaseManifest(context.Background(), ManifestOptions{
			OutDir:      dist,
			VersionFile: versionFile,
			Signer:      s,
		})
		if err != nil {
			t.Fatalf("WriteReleaseManifest: %v", err)
		}
		if filepath.Base(sigPath) != "release-manifest.json.sig" {
			t.Errorf("sig path = %q, want release-manifest.json.sig", filepath.Base(sigPath))
		}
		// The signature file contains exactly the signer's output.
		sigRaw, err := os.ReadFile(sigPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(sigRaw) != "FIXED-MOCK-SIGNATURE-BYTES" {
			t.Errorf("sig file = %q, want the signer's output verbatim", sigRaw)
		}
		// The signer received EXACTLY the manifest bytes that are on disk.
		manifestRaw, _ := os.ReadFile(filepath.Join(dist, "release-manifest.json"))
		if !s.saw(manifestRaw) {
			t.Errorf("signer received %q, want exactly the %d manifest bytes on disk", s.got, len(manifestRaw))
		}
		t.Logf("PASS: .sig seals the exact %d manifest bytes (recorded by the mock)", len(manifestRaw))
	})

	t.Run("Given NO signer configured, When WriteReleaseManifest runs, Then no .sig file is produced (dev mode) and the manifest still is", func(t *testing.T) {
		dist, versionFile := seedManifest(t)
		manifestPath, sigPath, err := WriteReleaseManifest(context.Background(), ManifestOptions{
			OutDir:      dist,
			VersionFile: versionFile,
		})
		if err != nil {
			t.Fatalf("WriteReleaseManifest: %v", err)
		}
		if sigPath != "" {
			t.Errorf("no-signer run produced a sig path %q", sigPath)
		}
		if _, err := os.Stat(manifestPath); err != nil {
			t.Errorf("manifest must still be emitted without a signer: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dist, "release-manifest.json.sig")); !os.IsNotExist(err) {
			t.Errorf("release-manifest.json.sig exists (err %v) — no .sig must be written in dev mode", err)
		}
		t.Log("PASS: dev mode emits the manifest but no .sig")
	})
}

// S4: failure modes — fail-closed, never an inconsistent manifest.
func TestWriteReleaseManifest_FailureModes(t *testing.T) {
	t.Run("Given a platform zip deleted after SHA256SUMS, When WriteReleaseManifest runs, Then it errors naming the missing zip", func(t *testing.T) {
		dist, versionFile := seedManifest(t)
		missing := filepath.Join(dist, "glyphux-1.2.3-windows-amd64.zip")
		if err := os.Remove(missing); err != nil {
			t.Fatal(err)
		}
		_, _, err := WriteReleaseManifest(context.Background(), ManifestOptions{OutDir: dist, VersionFile: versionFile})
		if err == nil {
			t.Fatal("WriteReleaseManifest must fail when a platform zip is missing")
		}
		if !strings.Contains(err.Error(), "glyphux-1.2.3-windows-amd64.zip") {
			t.Errorf("error %v does not name the missing zip", err)
		}
		t.Logf("PASS: missing zip -> error naming it: %v", err)
	})

	t.Run("Given a zip whose bytes changed after SHA256SUMS, When WriteReleaseManifest runs, Then it errors (manifest must never diverge from SHA256SUMS)", func(t *testing.T) {
		dist, versionFile := seedManifest(t)
		tampered := filepath.Join(dist, "glyphux-1.2.3-linux-amd64.zip")
		if err := os.WriteFile(tampered, []byte("tampered bytes — no longer matching SHA256SUMS"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := WriteReleaseManifest(context.Background(), ManifestOptions{OutDir: dist, VersionFile: versionFile})
		if err == nil {
			t.Fatal("WriteReleaseManifest must fail when a zip's sha256 disagrees with SHA256SUMS")
		}
		if !strings.Contains(err.Error(), "glyphux-1.2.3-linux-amd64.zip") {
			t.Errorf("error %v does not name the diverging zip", err)
		}
		t.Logf("PASS: diverging zip -> fail-closed error: %v", err)
	})

	t.Run("Given no SHA256SUMS (or a missing entry), When WriteReleaseManifest runs, Then it errors — hashes must come from the verified listing", func(t *testing.T) {
		dist, versionFile := seedManifest(t)
		if err := os.Remove(filepath.Join(dist, "SHA256SUMS")); err != nil {
			t.Fatal(err)
		}
		if _, _, err := WriteReleaseManifest(context.Background(), ManifestOptions{OutDir: dist, VersionFile: versionFile}); err == nil {
			t.Fatal("WriteReleaseManifest must fail without SHA256SUMS (no trusted hash source)")
		} else {
			t.Logf("PASS: missing SHA256SUMS -> error: %v", err)
		}
	})

	t.Run("Given a manifest entry whose sha256 is not in SHA256SUMS, When WriteReleaseManifest runs, Then it errors (a zip present but unlisted is a contract violation)", func(t *testing.T) {
		dist, versionFile := seedManifest(t)
		// Append a stray zip AFTER SHA256SUMS was written: the zip exists
		// on disk but has no trusted hash — the manifest must refuse it.
		stray := filepath.Join(dist, "glyphux-1.2.3-darwin-arm64.zip")
		if err := os.WriteFile(stray, []byte("stray"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := WriteReleaseManifest(context.Background(), ManifestOptions{OutDir: dist, VersionFile: versionFile}); err == nil {
			t.Fatal("WriteReleaseManifest must fail for a zip with no SHA256SUMS entry")
		} else if !strings.Contains(err.Error(), "glyphux-1.2.3-darwin-arm64.zip") {
			t.Errorf("error %v does not name the unlisted zip", err)
		} else {
			t.Logf("PASS: unlisted zip -> fail-closed error: %v", err)
		}
	})
}
