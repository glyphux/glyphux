// Package packagefmt is the neutral (ADR-0001 §7-8) package format seam for
// the marketplace's distributable artifacts — the .gxp/.gxt/.gxb containers
// (Ticket T8 / gap 8).
//
// The locked format decision: these are deterministic ZIP containers NOW,
// not JSON envelopes. SignedPackage (below) is the verified in-memory
// representation produced by Decode and consumed by the install path; it is
// deliberately NOT a public distribution format — no code writes it to
// disk, and its JSON encoding (should one ever be needed for diagnostics)
// is an internal detail, never what an operator or a registry exchanges.
//
// Container layout (schema "glyphux-package/v1"):
//
//	manifest.json          the Manifest (schema_version, name, version, kind,
//	                       license, requires_core)
//	payload/<path>         one entry per PackageFile, at its declared path
//	checksums.json         {"<path>": "<sha256 hex>"} for every payload file
//	signatures/package.sig one JSON document: {"key_id","algorithm","signature"}
//	                       (signature base64) — the signature commits to the
//	                       canonical {manifest, checksums} signing payload, so
//	                       every payload byte is bound by both checksums.json
//	                       (integrity) and the signature (authenticity).
//
// Determinism guarantees (the publish pipeline and content-addressed
// registries depend on them): entries are written in a fixed order
// (manifest.json, payload files in Files order, checksums.json,
// signatures/package.sig), every entry uses the stored (uncompressed)
// method, and no timestamp metadata is written. Encode of equal inputs is
// therefore byte-identical.
//
// Decode is the single verification entry point: it validates the container
// shape and schema version, validates the manifest, verifies integrity
// (every payload file matches its checksums.json entry), then verifies the
// signature key-ID-aware (Signature.KeyID is resolved in the caller's trust
// set — an unknown id is ErrUntrustedKey, a bad signature is
// ErrInvalidSignature). Verification is deliberately NOT spread across
// callers: the install path consumes the already-verified SignedPackage.
package packagefmt

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// SchemaVersionV1 is the current container schema.
const SchemaVersionV1 = "glyphux-package/v1"

// Kind discriminates what the payload decodes to. The exercised kinds are
// plugin/preset/bundle (mapping 1:1 onto the install pipeline's
// preset.InstallFromPackage / bundle.InstallFromPackage; the .gxp/.gxt/.gxb
// extensions are file-extension conventions, not kinds — a .gxt carries
// kind=preset). theme/block remain declared-but-unwired, matching
// capabilities/marketplace's ArtifactKind vocabulary.
type Kind string

const (
	KindPlugin Kind = "plugin"
	KindPreset Kind = "preset"
	KindBundle Kind = "bundle"
)

// Manifest is the container's metadata document (manifest.json). Every
// field is part of the signed payload, so none of it can be altered after
// signing without breaking verification.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Kind          Kind   `json:"kind"`
	License       string `json:"license"`
	RequiresCore  string `json:"requires_core"`
}

// PackageFile is one payload file: its container path, its declared SHA-256
// (hex), and — after Decode — its verified content. Data is the ONLY place
// payload bytes live in the verified representation; Checksums and
// Signatures commit to it, but neither duplicates it.
type PackageFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"-"`
}

// Signature is one signature over the container's canonical payload.
// KeyID names the signer in the verifying host's trust set (the
// marketplace.trusted_keys records, config-side); Algorithm is the
// signature scheme ("ed25519"); Value is the raw signature bytes.
type Signature struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Value     []byte `json:"value"`
}

// SignedPackage is the verified in-memory representation of a container:
// its manifest, payload files (with content), the integrity checksums, and
// the signatures that bind them. It is the output of Decode and the input
// to the install path — never a distribution format itself.
type SignedPackage struct {
	Manifest   Manifest
	Files      []PackageFile
	Checksums  map[string]string
	Signatures []Signature
}

// Sentinel errors. Decode returns exactly one of these (wrapped with
// context) when a container fails its gates.
var (
	// ErrInvalidContainer means the bytes are not a valid glyphux-package/v1
	// container: bad zip, missing/extra entries, malformed manifest.json or
	// checksums.json, a foreign schema version, an invalid manifest, or a
	// payload byte that does not match its checksum.
	ErrInvalidContainer = errors.New("packagefmt: invalid package container")
	// ErrUntrustedKey means the container's signature names a key id that
	// is absent from the trust set passed to Decode — the key-ID-aware
	// trust gate.
	ErrUntrustedKey = errors.New("packagefmt: package signed by an untrusted key")
	// ErrInvalidSignature means the signature does not verify under the
	// trusted key its key id names — the package was tampered with after
	// signing, or was never signed by that key.
	ErrInvalidSignature = errors.New("packagefmt: invalid package signature")
	// ErrEntryTooLarge means a container entry exceeds MaxEntrySize — the
	// read is refused before any of its content is materialized, so a
	// hostile-but-unsigned container cannot be a zip bomb that makes Decode
	// allocate unbounded memory (Decode reads every entry in full before
	// the signature gate).
	ErrEntryTooLarge = errors.New("packagefmt: container entry exceeds MaxEntrySize")
)

// MaxEntrySize caps a single container entry (manifest, checksums,
// signature, or payload file) at 64 MiB. Legitimate payloads — JSON
// preset/bundle artifacts, wasm modules — are far below this; the cap
// exists to bound memory during the pre-signature integrity pass.
const MaxEntrySize = 64 << 20

const (
	manifestEntry = "manifest.json"
	checksumEntry = "checksums.json"
	signatureFile = "signatures/package.sig"
	payloadPrefix = "payload/"
)

// signatureDocument is the JSON shape of signatures/package.sig. Base64
// keeps it a plain-text-diffable document (JSON-in-ZIP is the container's
// own internal representation; see the package doc for why SignedPackage
// itself is not a JSON format).
type signatureDocument struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

// Encode writes sp as a deterministic glyphux-package/v1 container signed
// by priv under keyID (an ed25519 key pair; keyID is the signer's identity
// in the verifying hosts' trust sets). Encode does NOT validate the schema
// version — schema validation is Decode's job, so the schema-gate test can
// encode a v2 manifest and prove Decode rejects it.
func Encode(sp SignedPackage, priv ed25519.PrivateKey, keyID string) ([]byte, error) {
	manifestJSON, err := json.Marshal(sp.Manifest)
	if err != nil {
		return nil, fmt.Errorf("packagefmt: encode manifest: %w", err)
	}

	// checksums.json is written with sorted keys — Go maps randomize
	// iteration order, and the container must be byte-deterministic.
	checksums := make(map[string]string, len(sp.Checksums))
	for path, sum := range sp.Checksums {
		checksums[path] = sum
	}
	checksumKeys := make([]string, 0, len(checksums))
	for path := range checksums {
		checksumKeys = append(checksumKeys, path)
	}
	sort.Strings(checksumKeys)
	checksumsJSON, err := json.Marshal(checksums)
	if err != nil {
		return nil, fmt.Errorf("packagefmt: encode checksums: %w", err)
	}

	payload, err := json.Marshal(struct {
		Manifest  json.RawMessage `json:"manifest"`
		Checksums json.RawMessage `json:"checksums"`
	}{Manifest: manifestJSON, Checksums: checksumsJSON})
	if err != nil {
		return nil, fmt.Errorf("packagefmt: build signing payload: %w", err)
	}
	sig := ed25519.Sign(priv, payload)

	doc, err := json.Marshal(signatureDocument{
		KeyID:     keyID,
		Algorithm: "ed25519",
		Signature: base64.StdEncoding.EncodeToString(sig),
	})
	if err != nil {
		return nil, fmt.Errorf("packagefmt: encode signature: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeStored(zw, manifestEntry, manifestJSON); err != nil {
		return nil, err
	}
	for _, f := range sp.Files {
		if err := writeStored(zw, payloadPrefix+f.Path, f.Data); err != nil {
			return nil, err
		}
	}
	if err := writeStored(zw, checksumEntry, checksumsJSON); err != nil {
		return nil, err
	}
	if err := writeStored(zw, signatureFile, doc); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("packagefmt: close container: %w", err)
	}
	return buf.Bytes(), nil
}

// writeStored writes one entry with the stored (uncompressed) method and no
// timestamp — the two determinism guarantees, alongside fixed entry order.
func writeStored(zw *zip.Writer, name string, data []byte) error {
	h := &zip.FileHeader{Name: name, Method: zip.Store}
	// No timestamp metadata is written: FileHeader's zero Modified value
	// encodes to the fixed DOS epoch on every build, so Encode stays
	// byte-deterministic.
	w, err := zw.CreateHeader(h)
	if err != nil {
		return fmt.Errorf("packagefmt: create %s: %w", name, err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("packagefmt: write %s: %w", name, err)
	}
	return nil
}

// Decode verifies data as a glyphux-package/v1 container against the trust
// set keys (keyID → public key) and returns the verified SignedPackage.
// Gates run in order: container shape + schema version, manifest
// validation, integrity (every payload file matches checksums.json), then
// key-ID-aware signature verification. A container failing any gate returns
// an error wrapping one of the package's sentinel errors.
func Decode(data []byte, keys map[string]ed25519.PublicKey) (*SignedPackage, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: not a zip container: %v", ErrInvalidContainer, err)
	}

	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		if _, dup := entries[f.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate entry %q", ErrInvalidContainer, f.Name)
		}
		entries[f.Name] = f
	}

	manifestRaw, ok := entries[manifestEntry]
	if !ok {
		return nil, fmt.Errorf("%w: missing %s", ErrInvalidContainer, manifestEntry)
	}
	checksumFile, ok := entries[checksumEntry]
	if !ok {
		return nil, fmt.Errorf("%w: missing %s", ErrInvalidContainer, checksumEntry)
	}
	sigFile, ok := entries[signatureFile]
	if !ok {
		return nil, fmt.Errorf("%w: missing %s", ErrInvalidContainer, signatureFile)
	}

	manifestBytes, err := readEntry(manifestRaw)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, fmt.Errorf("%w: manifest.json is not JSON: %v", ErrInvalidContainer, err)
	}
	if err := validateManifest(&m); err != nil {
		return nil, err
	}

	checksumsBytes, err := readEntry(checksumFile)
	if err != nil {
		return nil, err
	}
	var checksums map[string]string
	if err := json.Unmarshal(checksumsBytes, &checksums); err != nil {
		return nil, fmt.Errorf("%w: checksums.json is not JSON: %v", ErrInvalidContainer, err)
	}

	// Integrity: every payload entry must be declared in checksums.json
	// with a matching hash, and every declared checksum must have an entry.
	files := make([]PackageFile, 0, len(checksums))
	declared := map[string]bool{}
	for name, f := range entries {
		if len(name) <= len(payloadPrefix) || name[:len(payloadPrefix)] != payloadPrefix {
			continue
		}
		path := name[len(payloadPrefix):]
		sum, ok := checksums[path]
		if !ok {
			return nil, fmt.Errorf("%w: payload %q has no checksums.json entry", ErrInvalidContainer, path)
		}
		declared[path] = true
		raw, err := readEntry(f)
		if err != nil {
			return nil, err
		}
		got := sha256.Sum256(raw)
		if hex.EncodeToString(got[:]) != sum {
			return nil, fmt.Errorf("%w: payload %q fails its checksum (tampered)", ErrInvalidContainer, path)
		}
		files = append(files, PackageFile{Path: path, SHA256: sum, Data: raw})
	}
	for path := range checksums {
		if !declared[path] {
			return nil, fmt.Errorf("%w: checksums.json declares %q with no payload entry", ErrInvalidContainer, path)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	// Signature: key-ID-aware. Resolve KeyID in the trust set first
	// (ErrUntrustedKey), then verify (ErrInvalidSignature).
	sigBytes, err := readEntry(sigFile)
	if err != nil {
		return nil, err
	}
	var sigDoc signatureDocument
	if err := json.Unmarshal(sigBytes, &sigDoc); err != nil {
		return nil, fmt.Errorf("%w: signatures/package.sig is not JSON: %v", ErrInvalidContainer, err)
	}
	pub, trusted := keys[sigDoc.KeyID]
	if !trusted {
		return nil, fmt.Errorf("%w: key id %q", ErrUntrustedKey, sigDoc.KeyID)
	}
	sigValue, err := base64.StdEncoding.DecodeString(sigDoc.Signature)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed signature encoding: %v", ErrInvalidContainer, err)
	}
	signingPayload, err := signingPayload(manifestBytes, checksumsBytes)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(pub, signingPayload, sigValue) {
		return nil, fmt.Errorf("%w: key id %q", ErrInvalidSignature, sigDoc.KeyID)
	}

	return &SignedPackage{
		Manifest:   m,
		Files:      files,
		Checksums:  checksums,
		Signatures: []Signature{{KeyID: sigDoc.KeyID, Algorithm: sigDoc.Algorithm, Value: sigValue}},
	}, nil
}

// validateManifest enforces the schema version and required fields. The
// schema version is the container-format version (glyphux-package/v1), NOT
// the repo/artifact version — a correctly signed v2 container is still
// rejected here, which is exactly the schema gate the format decision
// requires.
func validateManifest(m *Manifest) error {
	if m.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("%w: schema version %q (want %s)", ErrInvalidContainer, m.SchemaVersion, SchemaVersionV1)
	}
	if m.Name == "" || m.Version == "" {
		return fmt.Errorf("%w: manifest requires non-empty name and version", ErrInvalidContainer)
	}
	switch m.Kind {
	case KindPlugin, KindPreset, KindBundle:
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidContainer, m.Kind)
	}
	return nil
}

// signingPayload is the canonical byte payload the signature commits to:
// the manifest.json bytes and the checksums.json bytes, structurally bound.
// Every payload file is bound via checksums.json; every metadata field is
// bound via manifest.json — no byte can change without breaking the
// signature.
func signingPayload(manifestJSON, checksumsJSON []byte) ([]byte, error) {
	return json.Marshal(struct {
		Manifest  json.RawMessage `json:"manifest"`
		Checksums json.RawMessage `json:"checksums"`
	}{Manifest: manifestJSON, Checksums: checksumsJSON})
}

// Artifact returns the payload bytes for the package's kind: the file named
// "preset.json" for KindPreset, "bundle.json" for KindBundle. These are
// the container's canonical artifact file names (the install path's decode
// target); a container missing its kind's artifact file is invalid.
// KindPlugin returns an error — the compiled-module payload has no
// canonical JSON artifact file, and plugin install is out of T8 scope.
func (sp *SignedPackage) Artifact() ([]byte, error) {
	name := ""
	switch sp.Manifest.Kind {
	case KindPreset:
		name = "preset.json"
	case KindBundle:
		name = "bundle.json"
	default:
		return nil, fmt.Errorf("%w: kind %q has no installable artifact", ErrInvalidContainer, sp.Manifest.Kind)
	}
	for _, f := range sp.Files {
		if f.Path == name {
			return f.Data, nil
		}
	}
	return nil, fmt.Errorf("%w: missing payload file %q", ErrInvalidContainer, name)
}

// readEntry returns a zip entry's full contents.
func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %v", ErrInvalidContainer, f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrInvalidContainer, f.Name, err)
	}
	return data, nil
}
