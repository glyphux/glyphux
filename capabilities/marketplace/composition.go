package marketplace

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/compat"
	"github.com/glyphux/glyphux/pkg/contract"
	"golang.org/x/mod/semver"
)

// This file is Ticket P4.7's (PRD §13.4) extension of the existing
// marketplace Package/Sign/Verify machinery to the two artifact kinds that
// are pure data and therefore straightforwardly packageable today:
// composition presets and composition bundles (pkg/contract's slice-4.6
// types). See ArtifactKind's doc comment for the blocks/themes scope
// boundary this ticket draws instead of forcing them through the same path.
//
// The compatibility contract (pkg/compat, slice 4.6) is reused, not
// reinvented, for the "publish-time" half of PRD §13.4's "checked at
// publish and import": PackagePreset/PackageBundle refuse to build (and
// therefore refuse to sign) an artifact whose own composition tree uses a
// block type or region its own Manifest doesn't declare — self-consistency
// of the artifact against its OWN stated dependencies, checked with a
// synthetic *blocks.Registry built from nothing but those declared
// dependencies (no live host registry is consulted here; that is
// necessarily a different, later check — see the "import-time" half below).
// The "import-time" half of the same requirement — does the DESTINATION
// host actually have what's declared — is deliberately NOT reimplemented
// here: internal/preset.Store.InstallFromPackage and
// internal/bundle.Store.InstallFromPackage decode a verified package via
// DecodePreset/DecodeBundle and then hand off to those packages' own
// pre-existing Store.Import, which already runs compat.CheckPreset/
// CheckBundle against the live registry and surfaces a structured
// compat.Result rather than hard-erroring (slice 4.6). This file only ever
// builds a registry FROM an artifact's own declared Manifest.Blocks, never
// from a real running one.

// ErrArtifactKindMismatch is returned by DecodePreset/DecodeBundle when
// called on a Package whose Kind is not the one the decoder expects (e.g.
// calling DecodePreset on a Package built by PackageBundle).
var ErrArtifactKindMismatch = errors.New("marketplace: package kind does not match the requested artifact type")

// PackagePreset builds an unsigned preset Package for preset: name/version
// are the marketplace listing identity (see Package.Name/Version's own doc
// comment), license is the declared license (PRD §13.4, e.g. an SPDX
// identifier or "proprietary"), and requiresCore is the operator-prefixed
// kernel-version constraint (PRD §13.4's "requires.core"; see
// CoreConstraintSatisfied for its format) — both carried at the Package
// level since a preset artifact has no sdk.Manifest of its own to hold them
// (see Package.RequiresCore's doc comment).
//
// preset.Validate() is run first (a structurally malformed preset is never
// packageable), then compat.CheckPreset against a registry synthesized
// purely from preset.Manifest.Blocks (never a live/real registry — see this
// file's own doc comment) and preset.Manifest.Slots as the theme-region
// restriction: this is the "publish-time" self-consistency half of PRD
// §13.4's "checked at publish and import" requirement. A preset whose
// composition tree references a block type or region its own Manifest
// doesn't declare fails this check and PackagePreset returns the
// compat.Result alongside a non-nil error — the caller (the marketplace
// operator's own publish pipeline) never gets a Package to sign for such a
// preset.
func PackagePreset(name, version, license, requiresCore string, preset contract.CompositionPreset) (Package, compat.Result, error) {
	if err := preset.Validate(); err != nil {
		return Package{}, compat.Result{}, err
	}
	registry := syntheticRegistry(preset.Manifest.Blocks)
	result := compat.CheckPreset(&preset, registry, preset.Manifest.Slots)
	if !result.Compatible {
		return Package{}, result, fmt.Errorf("marketplace: preset %q is not self-consistent with its own declared manifest: %w", preset.Name, result.AsValidationErrors())
	}

	artifact, err := json.Marshal(preset)
	if err != nil {
		return Package{}, result, fmt.Errorf("marketplace: encode preset artifact: %w", err)
	}
	return Package{
		Name:         name,
		Version:      version,
		Kind:         KindPreset,
		License:      license,
		RequiresCore: requiresCore,
		Artifact:     artifact,
	}, result, nil
}

// DecodePreset decodes pkg's Artifact back into a contract.CompositionPreset.
// Returns ErrArtifactKindMismatch if pkg.Kind is not KindPreset. Callers are
// expected to have already called Verify on the SignedPackage pkg came from
// — DecodePreset performs no signature check of its own, matching this
// package's existing precedent (Verify and the entitlement/gate functions
// are each single-purpose; decoding is a separate, later step that assumes
// verification already happened, exactly like internal/preset.Store.Get
// trusts its own already-validated storage).
func DecodePreset(pkg Package) (contract.CompositionPreset, error) {
	if pkg.Kind != KindPreset {
		return contract.CompositionPreset{}, fmt.Errorf("%w: got %q, want %q", ErrArtifactKindMismatch, pkg.Kind, KindPreset)
	}
	var p contract.CompositionPreset
	if err := json.Unmarshal(pkg.Artifact, &p); err != nil {
		return contract.CompositionPreset{}, fmt.Errorf("marketplace: decode preset artifact: %w", err)
	}
	return p, nil
}

// PackageBundle is PackagePreset's bundle-shaped counterpart: builds an
// unsigned bundle Package for bundle, checked for publish-time
// self-consistency via compat.CheckBundle against a registry synthesized
// from bundle.Manifest.Blocks (which compat.CheckBundle itself unions with
// every included preset's own declared/used blocks — see pkg/compat's
// mergeResults doc comment — so a bundle-level manifest that under-declares
// what a nested preset needs is caught here too, not just what the bundle's
// own top-level pages use).
func PackageBundle(name, version, license, requiresCore string, bundle contract.CompositionBundle) (Package, compat.Result, error) {
	if err := bundle.Validate(); err != nil {
		return Package{}, compat.Result{}, err
	}
	registry := syntheticRegistry(bundle.Manifest.Blocks)
	for _, p := range bundle.Presets {
		registry = mergeRegistry(registry, p.Manifest.Blocks)
	}
	result := compat.CheckBundle(&bundle, registry, bundle.Manifest.Slots)
	if !result.Compatible {
		return Package{}, result, fmt.Errorf("marketplace: bundle %q is not self-consistent with its own declared manifest: %w", bundle.Name, result.AsValidationErrors())
	}

	artifact, err := json.Marshal(bundle)
	if err != nil {
		return Package{}, result, fmt.Errorf("marketplace: encode bundle artifact: %w", err)
	}
	return Package{
		Name:         name,
		Version:      version,
		Kind:         KindBundle,
		License:      license,
		RequiresCore: requiresCore,
		Artifact:     artifact,
	}, result, nil
}

// DecodeBundle decodes pkg's Artifact back into a
// contract.CompositionBundle. Returns ErrArtifactKindMismatch if pkg.Kind is
// not KindBundle. See DecodePreset's doc comment for why no signature check
// happens here.
func DecodeBundle(pkg Package) (contract.CompositionBundle, error) {
	if pkg.Kind != KindBundle {
		return contract.CompositionBundle{}, fmt.Errorf("%w: got %q, want %q", ErrArtifactKindMismatch, pkg.Kind, KindBundle)
	}
	var b contract.CompositionBundle
	if err := json.Unmarshal(pkg.Artifact, &b); err != nil {
		return contract.CompositionBundle{}, fmt.Errorf("marketplace: decode bundle artifact: %w", err)
	}
	return b, nil
}

// syntheticRegistry builds a *blocks.Registry containing exactly the named
// block types, each with an otherwise-empty Definition — never a live,
// running registry. Used only to check an artifact's composition tree
// against its OWN declared dependencies (see this file's top doc comment);
// a real destination host's actual registered block schemas are
// deliberately never consulted here.
func syntheticRegistry(names []string) *blocks.Registry {
	r := blocks.New()
	for _, name := range names {
		_ = r.Register(blocks.Definition{Name: name}) // duplicate/empty names are not this check's concern
	}
	return r
}

// mergeRegistry adds names into an existing synthetic registry, skipping
// any already registered (blocks.Registry.Register errors on a duplicate
// name; PackageBundle may see the same block type declared by both the
// bundle's own manifest and a nested preset's manifest).
func mergeRegistry(r *blocks.Registry, names []string) *blocks.Registry {
	for _, name := range names {
		if _, ok := r.Get(name); ok {
			continue
		}
		_ = r.Register(blocks.Definition{Name: name})
	}
	return r
}

// coreConstraintOperators mirrors pkg/sdk's unexported constant of the same
// name/purpose (pkg/sdk/manifest.go) — duplicated in miniature rather than
// imported, since pkg/sdk does not export it, following this package's own
// P3.5 precedent (canonicalSemver/versionInRange in marketplace.go) of
// small local semver helpers over reaching into pkg/sdk's internals.
var coreConstraintOperators = []string{">=", "<=", "==", ">", "<"}

// CoreConstraintSatisfied reports whether kernelVersion satisfies
// constraint, an operator-prefixed semver constraint string (">=1.0.0",
// "<2.0.0", "==1.2.3", or a bare version meaning exact match) — the exact
// format PRD §13.4's "requires.core" uses, matching pkg/sdk.Requires.Core's
// own documented format so an author who has already learned that format
// for a plugin manifest does not need to learn a second one for a preset/
// bundle package's RequiresCore. An invalid constraint or kernelVersion
// never satisfies (fails closed).
func CoreConstraintSatisfied(constraint, kernelVersion string) bool {
	if constraint == "" {
		return true
	}
	kv := canonicalSemver(kernelVersion)
	if !semver.IsValid(kv) {
		return false
	}
	for _, op := range coreConstraintOperators {
		if rest, ok := strings.CutPrefix(constraint, op); ok {
			c := canonicalSemver(rest)
			if !semver.IsValid(c) {
				return false
			}
			cmp := semver.Compare(kv, c)
			switch op {
			case ">=":
				return cmp >= 0
			case "<=":
				return cmp <= 0
			case "==":
				return cmp == 0
			case ">":
				return cmp > 0
			case "<":
				return cmp < 0
			}
		}
	}
	c := canonicalSemver(constraint)
	if !semver.IsValid(c) {
		return false
	}
	return semver.Compare(kv, c) == 0
}
