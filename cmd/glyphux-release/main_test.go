// I4-core — cmd/glyphux-release wiring, behavior-first. Hermetic: fake
// binaries for the full 5-platform matrix in a temp dist; no real builds,
// no GitHub, no gpg (a mock signer records the exact manifest bytes).
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/release"
	"github.com/glyphux/glyphux/internal/sign"
)

// testSigner implements sign.Signer hermetically, recording the exact
// bytes handed over.
type testSigner struct {
	got []byte
	out []byte
}

func (s *testSigner) Sign(data []byte) ([]byte, error) {
	s.got = append([]byte(nil), data...)
	return append([]byte(nil), s.out...), nil
}

// seedBinaries writes a fake glyphux/glyphuxd per matrix platform (the
// flat ci-style names the workflow's build job produces).
func seedBinaries(t *testing.T, dist string) {
	t.Helper()
	for _, p := range matrix {
		ext := ""
		if p.goos == "windows" {
			ext = ".exe"
		}
		for _, b := range []string{"glyphux", "glyphuxd"} {
			if err := os.WriteFile(filepath.Join(dist, b+"-"+p.goos+"-"+p.goarch+ext),
				[]byte("fake "+b+" "+p.goos+"/"+p.goarch), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func seedDist(t *testing.T) (dist, versionFile string) {
	t.Helper()
	dir := t.TempDir()
	dist = filepath.Join(dir, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	seedBinaries(t, dist)
	versionFile = filepath.Join(dir, "VERSION")
	if err := os.WriteFile(versionFile, []byte("0.2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dist, versionFile
}

// S5a: the command packages the matrix and emits SHA256SUMS + the signed
// release manifest after every platform is packaged.
func TestBuildEmitsManifestAfterSHA256SUMS(t *testing.T) {
	dist, versionFile := seedDist(t)
	if err := build(context.Background(), dist, versionFile, "stable", nil); err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{"SHA256SUMS", "release-manifest.json"} {
		if _, err := os.Stat(filepath.Join(dist, want)); err != nil {
			t.Errorf("missing %s after build: %v", want, err)
		}
	}
	zips, _ := filepath.Glob(filepath.Join(dist, "*.zip"))
	if len(zips) != len(matrix) {
		t.Errorf("dist has %d zips, want %d (one per matrix platform)", len(zips), len(matrix))
	}
	raw, err := os.ReadFile(filepath.Join(dist, "release-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m release.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.Version != "0.2.0" {
		t.Errorf("manifest version = %q, want 0.2.0 (from the VERSION file)", m.Version)
	}
	if m.Channel != "stable" {
		t.Errorf("manifest channel = %q, want stable", m.Channel)
	}
	if len(m.Platforms) != len(matrix) {
		t.Errorf("manifest lists %d platforms, want %d", len(m.Platforms), len(matrix))
	}
	t.Logf("PASS: build emits SHA256SUMS + release-manifest.json (%d platforms)", len(m.Platforms))
}

// S5b: a configured signer produces release-manifest.json.sig sealing the
// exact manifest bytes the command wrote.
func TestBuildSignsManifestWhenSignerConfigured(t *testing.T) {
	dist, versionFile := seedDist(t)
	s := &testSigner{out: []byte("ARMORED-MOCK-SIGNATURE")}
	if err := build(context.Background(), dist, versionFile, "stable", s); err != nil {
		t.Fatalf("build: %v", err)
	}
	sigRaw, err := os.ReadFile(filepath.Join(dist, "release-manifest.json.sig"))
	if err != nil {
		t.Fatalf("missing .sig: %v", err)
	}
	if string(sigRaw) != "ARMORED-MOCK-SIGNATURE" {
		t.Errorf("sig file = %q, want the signer output verbatim", sigRaw)
	}
	manifestRaw, _ := os.ReadFile(filepath.Join(dist, "release-manifest.json"))
	if string(s.got) != string(manifestRaw) {
		t.Errorf("signer received %d bytes, want exactly the %d manifest bytes on disk", len(s.got), len(manifestRaw))
	}
	t.Logf("PASS: .sig written and seals the exact %d manifest bytes", len(manifestRaw))
}

// S5c: the command fails fast when a matrix binary is missing — no partial
// release, no manifest for a broken artifact set.
func TestBuildFailsClosedOnMissingBinary(t *testing.T) {
	dist, versionFile := seedDist(t)
	if err := os.Remove(filepath.Join(dist, "glyphuxd-windows-amd64.exe")); err != nil {
		t.Fatal(err)
	}
	err := build(context.Background(), dist, versionFile, "stable", nil)
	if err == nil {
		t.Fatal("build must fail when a matrix binary is missing")
	}
	if !strings.Contains(err.Error(), "windows") {
		t.Errorf("error %v does not identify the failing platform", err)
	}
	if _, err := os.Stat(filepath.Join(dist, "release-manifest.json")); !os.IsNotExist(err) {
		t.Errorf("release-manifest.json must NOT be written for a failed build (err %v)", err)
	}
	t.Logf("PASS: missing binary -> fail-closed, no manifest emitted: %v", err)
}

// S5d: the signer config wiring — env key / GNUPGHOME / -sign flag each
// enable signing; with none set, dev mode (no signer).
func TestResolveSignerHonorsEnvAndFlag(t *testing.T) {
	t.Setenv("GLYPHUX_RELEASE_GPG_KEY", "")
	t.Setenv("GLYPHUX_RELEASE_GPG_HOME", "")
	if s := resolveSigner(false); s != nil {
		t.Errorf("no config -> resolveSigner(false) = %v, want nil (dev mode)", s)
	}
	t.Setenv("GLYPHUX_RELEASE_GPG_KEY", "0xFAKEKEY1234")
	if s := resolveSigner(false); s == nil {
		t.Error("GLYPHUX_RELEASE_GPG_KEY set -> resolveSigner(false) must return a signer")
	}
	t.Setenv("GLYPHUX_RELEASE_GPG_KEY", "")
	t.Setenv("GLYPHUX_RELEASE_GPG_HOME", "/tmp/gnupg")
	if s := resolveSigner(false); s == nil {
		t.Error("GLYPHUX_RELEASE_GPG_HOME set -> resolveSigner(false) must return a signer")
	}
	if s := resolveSigner(true); s == nil {
		t.Error("-sign flag -> resolveSigner(true) must return a signer")
	}
	t.Log("PASS: signing enabled by env key, GNUPGHOME, or -sign; dev mode when none set")

	// The resolved signer must be the gpg-backed one (fail-closed on a
	// missing gpg binary — the real-key path is never silently skipped).
	g, ok := resolveSigner(false).(sign.GPG)
	if !ok {
		t.Fatalf("resolved signer type = %T, want sign.GPG", resolveSigner(false))
	}
	g.Bin = filepath.Join(t.TempDir(), "no-gpg")
	if _, err := g.Sign([]byte("manifest")); err == nil {
		t.Error("gpg signer must fail-closed when the gpg binary is missing")
	}
	t.Log("PASS: gpg-backed signer fails closed on missing binary")
}
