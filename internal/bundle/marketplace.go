package bundle

import (
	"context"
	"errors"
	"fmt"

	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/packagefmt"
)

// ErrRequiresCoreNotSatisfied mirrors internal/preset's error of the same
// name (see its doc comment for the full reasoning) — this is bundle's own
// copy, kept in the bundle package rather than shared, matching how
// ErrNotFound is independently declared in both packages rather than a
// shared error type.
var ErrRequiresCoreNotSatisfied = errors.New("bundle: package's requires.core is not satisfied by this host's kernel version")

// InstallFromPackage is internal/preset.Store.InstallFromPackage's
// bundle-shaped counterpart — checks RequiresCore against kernelVersion,
// decodes the bundle artifact, and persists it locally under a fresh ID
// without running Save's live-registry compat gate. See
// internal/preset.Store.InstallFromPackage's doc comment for the full
// reasoning (why no compat gate here, why no entitlement check, why this
// is the "wire marketplace import through the existing Import path"
// instruction applied literally — Import itself, below, is untouched by
// this file).
//
// Signature verification is deliberately NOT repeated here, matching the
// preset store: sp must be the output of pkg/packagefmt.Decode — the
// verified in-memory representation (integrity + key-ID-aware signature
// already checked against the host's trust set, per the locked T8 format
// decision). The verification gate lives in one place (packagefmt.Decode),
// never spread across callers.
func (s *Store) InstallFromPackage(ctx context.Context, principal *permission.Principal, sp packagefmt.SignedPackage, kernelVersion string) (*Record, error) {
	if !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		return nil, permission.ErrDenied
	}
	if !marketplace.CoreConstraintSatisfied(sp.Manifest.RequiresCore, kernelVersion) {
		return nil, fmt.Errorf("%w: package %q requires core %q, host is %q", ErrRequiresCoreNotSatisfied, sp.Manifest.Name, sp.Manifest.RequiresCore, kernelVersion)
	}
	payload, err := sp.Artifact()
	if err != nil {
		return nil, fmt.Errorf("install bundle package %q: %w", sp.Manifest.Name, err)
	}
	capPkg := marketplace.Package{
		Name:         sp.Manifest.Name,
		Version:      sp.Manifest.Version,
		Kind:         marketplace.KindBundle,
		License:      sp.Manifest.License,
		RequiresCore: sp.Manifest.RequiresCore,
		Artifact:     payload,
	}
	b, err := marketplace.DecodeBundle(capPkg)
	if err != nil {
		return nil, fmt.Errorf("install bundle package %q: %w", sp.Manifest.Name, err)
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}

	return s.insert(ctx, &b)
}
