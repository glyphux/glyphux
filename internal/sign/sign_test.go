// I4 signing — behavior-first. Tests the sign (interface + gpg impl) with a
// fake gpg executable so the suite stays hermetic (no real keyring, no
// network, no timestamps): we assert gpg is invoked with the de-/armored
// detached-sign contract and that the EXACT manifest bytes reach gpg's
// stdin, and that a missing binary / failing key errors (fail-closed).
package sign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGPG writes a tiny `gpg` shell script that captures its stdin to a
// file and echoes an ASCII-armored detached-signature banner to stdout,
// emulating `gpg --detach-sign --armor` without a real gpg.
func fakeGPG(t *testing.T, dir string) (gpgBin, captureFile string) {
	t.Helper()
	captureFile = filepath.Join(dir, "gpg-captured.bin")
	gpgBin = filepath.Join(dir, "gpg")
	script := "#!/bin/sh\n" +
		"cat > " + captureFile + "\n" +
		"echo '-----BEGIN PGP SIGNATURE-----'\n" +
		"echo 'fake-armored-signature'\n" +
		"echo '-----END PGP SIGNATURE-----'\n"
	if err := os.WriteFile(gpgBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return gpgBin, captureFile
}

// fakeFailingGPG writes a gpg that always exits nonzero (e.g. "no secret
// key" / missing keyring) so we can prove the signer surfaces the failure
// instead of silently succeeding.
func fakeFailingGPG(t *testing.T, dir string) string {
	t.Helper()
	gpgBin := filepath.Join(dir, "gpg-failing")
	script := "#!/bin/sh\necho 'gpg: signing failed: No secret key' >&2\nexit 2\n"
	if err := os.WriteFile(gpgBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return gpgBin
}

func TestGPGSignSeals_ExactInputBytes(t *testing.T) {
	// Given a configured signing key and a fake gpg,
	// When Sign() signs the manifest bytes,
	// Then the armored signature is returned AND gpg received the exact
	// input on stdin (nothing transformed, no leading/trailing bytes).
	gpgBin, capture := fakeGPG(t, t.TempDir())
	input := []byte("release-manifest.json exact payload")
	g := GPG{Key: "0xFAKE1234", Bin: gpgBin}

	sig, err := g.Sign(input)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(string(sig), "BEGIN PGP SIGNATURE") {
		t.Errorf("signature %q is not ASCII-armored (want the gpg --armor banner)", sig)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(got) != string(input) {
		t.Errorf("gpg received %q on stdin, want exactly %q — signer must sign the exact manifest bytes, unmodified", got, input)
	}
	t.Logf("PASS: gpg signed exactly the %d manifest bytes (stdin pass-through verified)", len(input))
}

func TestSignedFailsToAdorn_WhenGPGMissing(t *testing.T) {
	// Given a configured signing key but no gpg binary on PATH,
	// When Sign() runs,
	// Then it fails-closed with a clear error (never silently skips).
	g := GPG{Key: "0xFFF1234", Bin: filepath.Join(t.TempDir(), "no-such-gpg")}
	_, err := g.Sign([]byte("manifest"))
	if err == nil {
		t.Fatal("Sign must fail-closed when the gpg binary is missing")
	}
	if !strings.Contains(err.Error(), "gpg") {
		t.Errorf("fail-closed error %v does not name gpg", err)
	}
	t.Logf("PASS: missing gpg -> fail-closed error: %v", err)
}

// CORE-LOW-1: the gpg subprocess must see EXACTLY ONE GNUPGHOME. If the
// signer configures HomeDir while the parent environment already carries a
// GNUPGHOME, appending blindly would hand gpg two GNUPGHOME entries, making
// the keyring lookup depend on how the exec layer resolves duplicates. The
// inherited value must be filtered so the configured HomeDir is the sole one.
func TestGPGSignSetsSingleGNUPGHOME(t *testing.T) {
	dir := t.TempDir()
	gpgBin := filepath.Join(dir, "gpg")
	envDump := filepath.Join(dir, "gpg-env.txt")
	script := "#!/bin/sh\nenv > " + envDump + "\necho '-----BEGIN PGP SIGNATURE-----'\necho 'fake'\necho '-----END PGP SIGNATURE-----'\n"
	if err := os.WriteFile(gpgBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate an inherited GNUPGHOME that must NOT leak into nor duplicate
	// the configured one.
	t.Setenv("GNUPGHOME", "/inherited/gnupg")
	g := GPG{Key: "k", Bin: gpgBin, HomeDir: "/configured/gnupg"}

	if _, err := g.Sign([]byte("manifest")); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	raw, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatalf("read env dump: %v", err)
	}
	var homes []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "GNUPGHOME=") {
			homes = append(homes, strings.TrimPrefix(line, "GNUPGHOME="))
		}
	}
	if len(homes) != 1 {
		t.Errorf("FAIL: gpg subprocess saw %d GNUPGHOME entries (%v) — want exactly 1 (no duplicate env keys)", len(homes), homes)
	} else if homes[0] != "/configured/gnupg" {
		t.Errorf("FAIL: GNUPGHOME = %q, want the configured HomeDir %q (inherited %q must be filtered)", homes[0], "/configured/gnupg", "/inherited/gnupg")
	} else {
		t.Logf("PASS: exactly one GNUPGHOME=%s reaches gpg (inherited %s filtered, no duplicates)", homes[0], "/inherited/gnupg")
	}
	if !strings.Contains(string(raw), "PATH=") {
		t.Errorf("FAIL: child env lost PATH — only GNUPGHOME should be replaced/filtered, the rest preserved")
	} else {
		t.Logf("PASS: non-GNUPGHOME environment preserved (PATH etc.)")
	}
}

// CORE-LOW-1 (source-level): the helper the signer uses to build the gpg
// subprocess env must remove EVERY existing GNUPGHOME entry (any casing),
// so the caller can append exactly one and never hand the subprocess
// duplicate keys that would make keyring resolution depend on exec-layer
// dedup semantics.
func TestEnvWithoutGNUPGHOMEFiltersAll(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"GNUPGHOME=/inherited/gnupg",
		"GNUPGHOME=/second/duplicate",
		"gnupghome=/lowercase/dup",
		"HOME=/home/x",
	}
	got := envWithoutGNUPGHOME(input)
	for _, kv := range got {
		key, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(key, "GNUPGHOME") {
			t.Errorf("FAIL: filtered env still carries GNUPGHOME entry %q", kv)
		}
	}
	if len(got) != 2 {
		t.Errorf("FAIL: envWithoutGNUPGHOME returned %d entries %v, want 2 (PATH, HOME) — every GNUPGHOME (any case) must be removed", len(got), got)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "HOME=/home/x") {
		t.Errorf("FAIL: non-GNUPGHOME entries must be preserved untouched, got %v", got)
	} else {
		t.Logf("PASS: all %d GNUPGHOME entries removed (mixed case), non-GNUPGHOME preserved: %v", len(input)-len(got), got)
	}
}

func TestGPGSignFailsClosed_WhenKeyringEmpty(t *testing.T) {
	// Given a working gpg but an empty keyring (no secret key intIt),
	// When Sign() runs Then gpg's nonzero exit is surfaced as an error —
	// a key was requested explicitly, so we fail rather than emit an
	// unsigned/self-signed artifact.
	gpgBin := fakeFailingGPG(t, t.TempDir())
	g := GPG{Key: "k", Bin: gpgBin}
	if _, err := g.Sign([]byte("manifest")); err == nil {
		t.Fatal("Sign must fail when gpg reports 'No secret key'")
	} else {
		t.Logf("PASS: key-absent fail-closed error: %v", err)
	}
}
