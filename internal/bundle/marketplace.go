package bundle

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/permission"
)

// ErrRequiresCoreNotSatisfied mirrors internal/preset's error of the same
// name (see its doc comment for the full reasoning) — this is bundle's own
// copy, kept in the bundle package rather than shared, matching how
// ErrNotFound is independently declared in both packages rather than a
// shared error type.
var ErrRequiresCoreNotSatisfied = errors.New("bundle: package's requires.core is not satisfied by this host's kernel version")

// InstallFromPackage is internal/preset.Store.InstallFromPackage's
// bundle-shaped counterpart — verifies sp's signature under pub, checks
// RequiresCore against kernelVersion, decodes the bundle artifact, and
// persists it locally under a fresh ID without running Save's live-registry
// compat gate. See internal/preset.Store.InstallFromPackage's doc comment
// for the full reasoning (why no compat gate here, why no entitlement
// check, why this is the "wire marketplace import through the existing
// Import path" instruction applied literally — Import itself, below, is
// untouched by this file).
func (s *Store) InstallFromPackage(ctx context.Context, principal *permission.Principal, sp marketplace.SignedPackage, pub ed25519.PublicKey, kernelVersion string) (*Record, error) {
	if !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		return nil, permission.ErrDenied
	}
	if err := marketplace.Verify(pub, sp); err != nil {
		return nil, fmt.Errorf("install bundle package %q: %w", sp.Package.Name, err)
	}
	if !marketplace.CoreConstraintSatisfied(sp.Package.RequiresCore, kernelVersion) {
		return nil, fmt.Errorf("%w: package %q requires core %q, host is %q", ErrRequiresCoreNotSatisfied, sp.Package.Name, sp.Package.RequiresCore, kernelVersion)
	}
	b, err := marketplace.DecodeBundle(sp.Package)
	if err != nil {
		return nil, fmt.Errorf("install bundle package %q: %w", sp.Package.Name, err)
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}

	return s.insert(ctx, &b)
}
