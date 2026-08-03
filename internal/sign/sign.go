// Package sign signs release metadata (I4): the release packager emits a
// release-manifest.json plus, when a signing key is configured, a detached
// ASCII-armored signature of the exact manifest bytes. The installer (next
// ticket) verifies that signature before trusting the manifest's
// SHA256SUMS/zip hashes.
//
// Signer is deliberately minimal — Sign([]byte) ([]byte, error) — so any
// backend (gpg, age, a future TPM/HSM proxy) can plug in; the caller
// persists the returned bytes verbatim as the .sig file.
package sign

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Signer seals release metadata. Implementations must sign the exact input
// bytes (detached — no wrapping preamble of the caller's data is added) and
// return the signature bytes for the caller to persist. Errors are fatal:
// a configured signer that cannot produce a signature must fail the build,
// never silently emit an unsigned artifact.
type Signer interface {
	Sign(data []byte) ([]byte, error)
}

// GPG signs with the system gpg, emulating:
//
//	gpg --detach-sign --armor [--local-user <key>] < manifest
//
// The manifest bytes are fed to gpg on stdin untouched; the ASCII-armored
// detached signature is read from stdout. Key selects the signing key (gpg
// key id / email); HomeDir (if set) becomes GNUPGHOME for the subprocess.
// Fails closed: a missing gpg binary, a failing keyring lookup, or any
// nonzero gpg exit is surfaced as an error — signing was requested, so a
// silent skip would ship an unsigned manifest as if it were signed.
type GPG struct {
	// Key is the gpg key id/email to sign with. Empty uses gpg's default
	// secret key (which itself fails if none exists).
	Key string
	// Bin is the gpg executable; empty defaults to "gpg".
	Bin string
	// HomeDir, if set, is exported as GNUPGHOME for the subprocess.
	HomeDir string
}

// Sign implements Signer via gpg --detach-sign --armor.
func (g GPG) Sign(data []byte) ([]byte, error) {
	bin := g.Bin
	if bin == "" {
		bin = "gpg"
	}
	args := []string{"--detach-sign", "--armor", "--output", "-"}
	if g.Key != "" {
		args = append(args, "--local-user", g.Key)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if g.HomeDir != "" {
		// CORE-LOW-1: never hand gpg duplicate GNUPGHOME entries. Filter any
		// inherited GNUPGHOME (any casing) first, then append the ONE
		// configured value, so keyring resolution does not depend on how the
		// exec layer dedupes duplicate keys.
		cmd.Env = append(envWithoutGNUPGHOME(os.Environ()), "GNUPGHOME="+g.HomeDir)
	}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("sign: gpg %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// envWithoutGNUPGHOME returns env with every existing GNUPGHOME entry
// (any casing) removed, preserving everything else — the caller appends
// exactly one GNUPGHOME, so the subprocess can never see a duplicate.
func envWithoutGNUPGHOME(env []string) []string {
	out := env[:0]
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(key, "GNUPGHOME") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
