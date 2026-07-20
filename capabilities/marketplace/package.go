package marketplace

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// Package is a distributable unit of a plugin, theme, or (Ticket P4.7, PRD
// §13.4) composition preset/bundle — "the same package machinery serves
// [every artifact kind]; only the registry namespace and artifact shape
// differ" (generalizing PRD §12.1's original "plugins and themes" framing
// now that presets/bundles flow through it too). Built directly on
// pkg/sdk.Manifest for the plugin/theme case — the existing
// extension-contract manifest type, not a parallel reinvention of it — and
// on pkg/contract.Manifest (embedded inside the preset/bundle artifact
// itself) for the composition-artifact case; see Kind's doc comment and
// this package's composition.go for the full reasoning.
type Package struct {
	// Name is the package's registry name. This is deliberately separate
	// from Manifest.Name: the manifest's name is the plugin's own declared
	// identity checked at HostAPI-construction time (pkg/sdk), while a
	// package's registry name is the marketplace listing identity — the
	// same plugin could in principle be re-listed, but in the common case
	// the two match and callers are expected to keep them in sync.
	Name string
	// Version is the package's own release version (semver), independent
	// of Manifest.Version for the same reason as Name above.
	Version string
	// Kind discriminates what Artifact decodes to (added by Ticket P4.7;
	// zero value "" is the original plugin/theme shape predating Kind's
	// introduction — treated identically to KindPlugin). See ArtifactKind's
	// doc comment (artifact_kind.go) for the full per-kind breakdown,
	// including which kinds are exercised by real code today vs. documented
	// as a structurally-supported-but-not-yet-wired extension point.
	Kind ArtifactKind
	// License is the artifact's declared license (PRD §13.4: "each carries
	// a manifest with license..." — e.g. an SPDX identifier, or
	// "proprietary" for a paid artifact with no open license). This package
	// does not interpret or enforce License in any way beyond including it
	// in the signed payload (tamper-evident, like every other field) — it
	// is descriptive metadata for a future marketplace listing/admin view,
	// not a gate this package's own logic branches on. Paid-artifact
	// protection is entirely the entitlement-token mechanism
	// (entitlement.go), independent of what License says.
	License string
	// RequiresCore is the running kernel version constraint this artifact
	// declares (PRD §13.4: "requires.core"), in the same operator-prefixed
	// semver format as pkg/sdk.Requires.Core (">=1.0.0", exact-match if no
	// operator prefix — see CoreConstraintSatisfied). Checked against the
	// installing host's kernel version at import time (PRD §13.4: "checked
	// at publish and import"); empty means no declared constraint.
	//
	// This is deliberately a package-level field, not folded into
	// Manifest/sdk.Manifest.Requires: for a preset/bundle (Kind ==
	// KindPreset/KindBundle) there is no sdk.Manifest at all (see Kind's
	// doc comment), so requires.core needs a home that exists for every
	// kind uniformly. requires.contract, by contrast, has no such problem —
	// pkg/contract.Manifest.RequiresContract (embedded in every
	// CompositionPreset/CompositionBundle artifact already, since slice
	// 4.6) already carries it for the composition-artifact kinds, and
	// sdk.Manifest.Requires.Contract already carries it for the
	// plugin/theme kind — so requires.contract is read from whichever of
	// those two places Kind says to look, never duplicated here.
	RequiresCore string
	// Manifest is the plugin/theme's extension-contract manifest (PRD
	// §7.3), included in the signed payload so a tampered manifest (e.g.
	// smuggling in an extra Permission after review) is detected the same
	// way a tampered artifact is. Only meaningful when Kind is KindPlugin
	// or "" (legacy); zero-value and ignored for KindPreset/KindBundle,
	// whose own compatibility-contract manifest instead lives inside the
	// artifact bytes (see composition.go).
	Manifest sdk.Manifest
	// Artifact is the package's actual payload bytes: the compiled WASM
	// module or RPC plugin binary (Kind == KindPlugin), or the JSON-encoded
	// pkg/contract.CompositionPreset/CompositionBundle document (Kind ==
	// KindPreset/KindBundle — see composition.go's PackagePreset/
	// PackageBundle). Only its SHA-256 hash is included in the signed
	// payload (not the full bytes), so signing/verification cost stays
	// proportional to a fixed-size digest rather than the artifact's own
	// size — the artifact bytes still travel alongside the signature, but
	// the signature itself commits to the hash.
	Artifact []byte
}

// ErrInvalidSignature is returned by Verify when a package's signature does
// not match its (name, version, manifest, artifact-hash) payload under the
// given public key — whether because the payload was tampered with after
// signing, or because it was signed with a different key entirely. Ed25519
// verification does not distinguish those two causes (nor can it: a
// signature computed under key A over payload P and a signature computed
// under key B over a tampered payload P' are both simply "does not verify
// under this public key"), so both collapse to this one error.
var ErrInvalidSignature = errors.New("marketplace: invalid package signature")

// SignedPackage pairs a Package with its Ed25519 signature over
// signingPayload(pkg).
type SignedPackage struct {
	Package   Package
	Signature []byte
}

// Sign signs pkg with priv (an Ed25519 private key — PRD §12.4/§12.5
// mechanism 1, "every package is signed"). This is the marketplace
// operator's own signing step, run at publish-time review (§12.4), not
// something a host or buyer ever does.
func Sign(priv ed25519.PrivateKey, pkg Package) (SignedPackage, error) {
	payload, err := signingPayload(pkg)
	if err != nil {
		return SignedPackage{}, fmt.Errorf("marketplace: build signing payload: %w", err)
	}
	sig := ed25519.Sign(priv, payload)
	return SignedPackage{Package: pkg, Signature: sig}, nil
}

// Verify reports whether sp's signature is valid over its own Package
// content under pub (an Ed25519 public key — the marketplace's public key,
// baked into the verifying host, per §12.4's "signing verification" and
// §12.5's mechanism 1). Returns ErrInvalidSignature if sp.Package has been
// tampered with in any way since signing (any field, including a single
// changed artifact byte) or if sp was signed under a different key.
func Verify(pub ed25519.PublicKey, sp SignedPackage) error {
	payload, err := signingPayload(sp.Package)
	if err != nil {
		return fmt.Errorf("marketplace: build signing payload: %w", err)
	}
	if !ed25519.Verify(pub, payload, sp.Signature) {
		return ErrInvalidSignature
	}
	return nil
}

// signingPayload builds the canonical byte payload signed/verified for pkg:
// name, version, kind, license, requires-core, the manifest (JSON-encoded),
// and the artifact's SHA-256 hash (hex-encoded) — never the raw artifact
// bytes themselves, so payload size stays bounded regardless of artifact
// size. Kind/License/RequiresCore were added by Ticket P4.7 alongside Name/
// Version/Manifest/ArtifactHash's original fields — included in the signed
// envelope for the same reason those are: a tampered field (e.g. quietly
// changing a paid artifact's License to "free", or loosening RequiresCore
// after review) must be caught by Verify exactly like a tampered artifact
// byte or a tampered Permission already are.
func signingPayload(pkg Package) ([]byte, error) {
	manifestJSON, err := json.Marshal(pkg.Manifest)
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	hash := sha256.Sum256(pkg.Artifact)
	envelope := struct {
		Name         string          `json:"name"`
		Version      string          `json:"version"`
		Kind         ArtifactKind    `json:"kind"`
		License      string          `json:"license"`
		RequiresCore string          `json:"requires_core"`
		Manifest     json.RawMessage `json:"manifest"`
		ArtifactHash string          `json:"artifact_hash"`
	}{
		Name:         pkg.Name,
		Version:      pkg.Version,
		Kind:         pkg.Kind,
		License:      pkg.License,
		RequiresCore: pkg.RequiresCore,
		Manifest:     manifestJSON,
		ArtifactHash: fmt.Sprintf("%x", hash),
	}
	return json.Marshal(envelope)
}
