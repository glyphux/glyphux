// RED tests for Ticket T8 (gap 8 — marketplace surface), the locked
// package-format decision: .gxp/.gxt/.gxb are deterministic ZIP containers
// NOW, established by pkg/packagefmt — NOT JSON-first. SignedPackage is the
// verified in-memory/internal representation only; it is never a public
// distribution format (ADR-0001 §7-8 + docs/mono-multi-repo-parens.md §2).
//
// Pinned contract (interpretation, flagged for owner where noted):
//
//	type SignedPackage struct {
//	    Manifest   Manifest              // glyphux-package/v1
//	    Files      []PackageFile         // payload/ paths + sha256 + verified content
//	    Checksums  map[string]string     // path -> sha256 (checksums.json)
//	    Signatures []Signature           // key_id + algorithm + value
//	}
//	type PackageFile struct { Path, SHA256 string; Data []byte }
//	func Encode(sp SignedPackage, priv ed25519.PrivateKey, keyID string) ([]byte, error)
//	func Decode(data []byte, keys map[string]ed25519.PublicKey) (*SignedPackage, error)
//
// Container layout (v1): manifest.json, payload/*, checksums.json,
// signatures/package.sig. Decode validates: container shape + schema
// version -> manifest -> integrity (every payload file matches its
// checksum) -> signature (by key ID against the trust set). Signature
// verification is key-ID-aware: Decode resolves Signature.KeyID in the keys
// map and fails with ErrUntrustedKey when the id is unknown.
//
// Determinism: Encode of equal inputs must produce byte-identical output
// (fixed entry order, zero timestamps, stored (uncompressed) entries).
package packagefmt_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/glyphux/glyphux/pkg/packagefmt"
)

const payloadMarker = "GLYPHUX-PRESET-FIXTURE-DATA"

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// buildPackage encodes a signed preset-kind (.gxt) package over a
// distinctive payload marker, returning the container bytes and the
// SignedPackage they encode. schemaVersion is passed through so tests can
// exercise the schema gate without post-hoc byte surgery.
func buildPackage(t *testing.T, priv ed25519.PrivateKey, keyID, name, requiresCore, schemaVersion string) ([]byte, packagefmt.SignedPackage) {
	t.Helper()
	payload := []byte(payloadMarker + "-" + name)
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: schemaVersion,
			Name:          name,
			Version:       "1.0.0",
			Kind:          packagefmt.KindPreset,
			License:       "free",
			RequiresCore:  requiresCore,
		},
		Files: []packagefmt.PackageFile{
			{Path: "preset.json", SHA256: sha256Hex(payload), Data: payload},
		},
		Checksums: map[string]string{"preset.json": sha256Hex(payload)},
	}
	data, err := packagefmt.Encode(sp, priv, keyID)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return data, sp
}

func buildV1(t *testing.T, priv ed25519.PrivateKey, keyID, name, requiresCore string) ([]byte, packagefmt.SignedPackage) {
	t.Helper()
	return buildPackage(t, priv, keyID, name, requiresCore, packagefmt.SchemaVersionV1)
}

// TestEncodeIsDeterministic: GIVEN the same SignedPackage and key, WHEN
// Encode runs twice, THEN the outputs are byte-identical — the container is
// reproducible (the registry/publish pipeline depends on it).
func TestEncodeIsDeterministic(t *testing.T) {
	_, priv := testKeys(t)
	first, _ := buildV1(t, priv, "glyphux-dev-2026-01", "theme-a", ">=0.1.0")
	second, _ := buildV1(t, priv, "glyphux-dev-2026-01", "theme-a", ">=0.1.0")
	if !bytes.Equal(first, second) {
		t.Error("Encode is not deterministic: two encodes of the same package differ")
	}
}

// TestDecodeRoundTripRestoresVerifiedPackage: GIVEN a container signed by a
// trusted key, WHEN Decode runs against the trust set, THEN the verified
// internal SignedPackage matches the input (manifest, files with content,
// checksums, signature key id) and no error is returned.
func TestDecodeRoundTripRestoresVerifiedPackage(t *testing.T) {
	pub, priv := testKeys(t)
	data, want := buildV1(t, priv, "glyphux-dev-2026-01", "theme-a", ">=0.1.0")

	got, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pub})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Manifest.Name != want.Manifest.Name || got.Manifest.SchemaVersion != packagefmt.SchemaVersionV1 ||
		got.Manifest.Kind != packagefmt.KindPreset || got.Manifest.RequiresCore != ">=0.1.0" {
		t.Errorf("manifest = %+v, want name=%s schema=%s kind=theme requires.core=%s",
			got.Manifest, want.Manifest.Name, packagefmt.SchemaVersionV1, ">=0.1.0")
	}
	if len(got.Signatures) != 1 || got.Signatures[0].KeyID != "glyphux-dev-2026-01" ||
		got.Signatures[0].Algorithm != "ed25519" || len(got.Signatures[0].Value) != ed25519.SignatureSize {
		t.Errorf("signatures = %+v, want one ed25519 sig under glyphux-dev-2026-01", got.Signatures)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "preset.json" || !bytes.Equal(got.Files[0].Data, want.Files[0].Data) {
		t.Errorf("files = %+v, want payload content restored", got.Files)
	}
	if got.Checksums["preset.json"] != want.Checksums["preset.json"] {
		t.Errorf("checksums = %v, want %v", got.Checksums, want.Checksums)
	}
}

// TestDecodeRejectsTamperedPayload: GIVEN a valid container whose payload
// byte was flipped, WHEN Decode runs, THEN integrity verification fails —
// the tamper is caught by checksums.json, before signature check.
func TestDecodeRejectsTamperedPayload(t *testing.T) {
	pub, priv := testKeys(t)
	data, _ := buildV1(t, priv, "glyphux-dev-2026-01", "theme-a", ">=0.1.0")

	idx := bytes.Index(data, []byte(payloadMarker))
	if idx < 0 {
		t.Fatal("payload marker not found in container")
	}
	data[idx] ^= 0xff

	if _, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pub}); err == nil {
		t.Fatal("Decode accepted a tampered payload")
	}
}

// TestDecodeRejectsTamperedManifest: GIVEN a valid container whose
// manifest.json byte was flipped, WHEN Decode runs, THEN signature
// verification fails (the manifest is part of the signed payload).
func TestDecodeRejectsTamperedManifest(t *testing.T) {
	pub, priv := testKeys(t)
	data, _ := buildV1(t, priv, "glyphux-dev-2026-01", "theme-a", ">=0.1.0")

	needle := []byte(`"name":"theme-a"`)
	idx := bytes.Index(data, needle)
	if idx < 0 {
		t.Fatalf("manifest name %q not found in container", needle)
	}
	data[idx+len(needle)-1] ^= 0xff

	if _, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pub}); err == nil {
		t.Fatal("Decode accepted a tampered manifest")
	}
}

// TestDecodeRejectsWrongSigningKey: GIVEN a container signed by key A, WHEN
// Decode runs with only key B trusted, THEN the signature does not verify.
func TestDecodeRejectsWrongSigningKey(t *testing.T) {
	_, privA := testKeys(t)
	pubB, _ := testKeys(t)
	data, _ := buildV1(t, privA, "glyphux-dev-2026-01", "theme-a", ">=0.1.0")

	if _, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pubB}); !errors.Is(err, packagefmt.ErrInvalidSignature) {
		t.Errorf("Decode with wrong key = %v, want ErrInvalidSignature", err)
	}
}

// TestDecodeRejectsUnknownKeyID: GIVEN a container whose signature names a
// key id absent from the trust set, WHEN Decode runs, THEN it fails with
// ErrUntrustedKey — the key-ID-aware trust gate.
func TestDecodeRejectsUnknownKeyID(t *testing.T) {
	_, priv := testKeys(t)
	data, _ := buildV1(t, priv, "glyphux-dev-2026-01", "theme-a", ">=0.1.0")

	if _, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{}); !errors.Is(err, packagefmt.ErrUntrustedKey) {
		t.Errorf("Decode with empty trust set = %v, want ErrUntrustedKey", err)
	}
}

// TestDecodeRejectsForeignSchemaVersion: GIVEN a correctly signed container
// whose manifest declares an unknown schema version, WHEN Decode runs, THEN
// manifest validation rejects it — schema versions are independent of repo
// versions (glyphux-package/v1, not "1.2.0"). The package is properly
// signed: only the schema gate can reject it.
func TestDecodeRejectsForeignSchemaVersion(t *testing.T) {
	pub, priv := testKeys(t)
	data, _ := buildPackage(t, priv, "glyphux-dev-2026-01", "theme-a", ">=0.1.0", "glyphux-package/v2")

	if _, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pub}); err == nil {
		t.Fatal("Decode accepted a foreign schema version")
	}
}

// --- Fix C (red-team hardening): Decode must reject zip-slip payload paths
// (.. segments, absolute paths, backslashes) and cap per-entry size before
// the signature gate, so a hostile-but-unsigned container cannot be a
// zip bomb and cannot carry paths a future consumer would resolve outside
// the package. Encode intentionally does NOT validate paths (Decode is the
// gate — the v2-schema RED test relies on Encode accepting what Decode
// rejects) — these tests build the containers directly through Encode. ---

// encodePayload encodes a signed container whose single payload file is
// path with content payload. Encode trusts the caller; Decode must not.
func encodePayload(t *testing.T, priv ed25519.PrivateKey, keyID, path string, payload []byte) []byte {
	t.Helper()
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: packagefmt.SchemaVersionV1,
			Name:          "evil",
			Version:       "1.0.0",
			Kind:          packagefmt.KindPreset,
			License:       "free",
			RequiresCore:  ">=0.1.0",
		},
		Files: []packagefmt.PackageFile{
			{Path: path, SHA256: sha256Hex(payload), Data: payload},
		},
		Checksums: map[string]string{path: sha256Hex(payload)},
	}
	data, err := packagefmt.Encode(sp, priv, keyID)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return data
}

// TestDecodeRejectsZipSlipPayloadPaths: GIVEN signed containers whose
// payload path escapes the payload/ dir (parent traversal, absolute, or
// Windows separator), WHEN Decode runs against the trust set, THEN every
// one is rejected — the format must not carry a path a future consumer
// could resolve outside the package.
func TestDecodeRejectsZipSlipPayloadPaths(t *testing.T) {
	pub, priv := testKeys(t)
	trust := map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pub}

	// Sanity: a benign relative path still round-trips.
	good := encodePayload(t, priv, "glyphux-dev-2026-01", "preset.json", []byte("fine"))
	if _, err := packagefmt.Decode(good, trust); err != nil {
		t.Fatalf("benign payload path must still decode: %v", err)
	}

	for _, p := range []string{
		"../../evil.json",
		"a/../../evil.json",
		"/etc/evil.json",
		"..\\..\\evil.json",
	} {
		data := encodePayload(t, priv, "glyphux-dev-2026-01", p, []byte("evil"))
		if _, err := packagefmt.Decode(data, trust); err == nil {
			t.Errorf("path %q: Decode must reject a zip-slip payload path", p)
		}
	}
}

// TestDecodeRejectsOversizedEntry: GIVEN a container whose payload exceeds
// MaxEntrySize, WHEN Decode runs, THEN the read is refused with
// ErrEntryTooLarge — before any of the oversized content is materialized
// (and, being the pre-signature integrity pass, before the signature gate).
func TestDecodeRejectsOversizedEntry(t *testing.T) {
	pub, priv := testKeys(t)
	big := bytes.Repeat([]byte{'x'}, packagefmt.MaxEntrySize+1)
	data := encodePayload(t, priv, "glyphux-dev-2026-01", "preset.json", big)

	_, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pub})
	if !errors.Is(err, packagefmt.ErrEntryTooLarge) {
		t.Fatalf("Decode error = %v, want ErrEntryTooLarge (per-entry size cap)", err)
	}
}
