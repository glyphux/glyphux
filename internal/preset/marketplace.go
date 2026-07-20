package preset

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/permission"
)

// ErrRequiresCoreNotSatisfied is returned by InstallFromPackage when sp's
// declared marketplace.Package.RequiresCore constraint does not admit
// kernelVersion (PRD §13.4: "requires.core... checked at publish and
// import"). This is a hard install-time gate, unlike the compatibility
// contract's block/slot mismatch (which InstallFromPackage tolerates and
// leaves for the caller's subsequent Import to explain, per slice 4.6's own
// "explainable, not silently rejected" requirement) — an artifact declaring
// it needs a newer kernel than this host runs cannot be evaluated for
// block/slot compatibility at all, since the compatibility-contract types
// and semantics it was authored against may themselves have changed.
var ErrRequiresCoreNotSatisfied = errors.New("preset: package's requires.core is not satisfied by this host's kernel version")

// InstallFromPackage verifies sp's signature under pub (the marketplace's
// public key, baked into this host — PRD §12.4/§12.5), checks its declared
// RequiresCore against kernelVersion, decodes its artifact into a
// contract.CompositionPreset, and persists it locally under a freshly
// generated ID — WITHOUT running Save's own compat.CheckPreset gate against
// the live registry.
//
// This is deliberate, not an oversight: Save's doc comment explains it
// requires full live-registry compatibility because a preset saved through
// it is "authored locally, from blocks the author actually has" — a
// marketplace package is the opposite case Save's own doc flags as
// needing different handling: "built somewhere else, must tolerate (and
// explain, not silently drop) a preset built against a block this host
// lacks." InstallFromPackage is that different handling: it persists the
// artifact unconditionally (once its signature and requires.core have
// cleared) and leaves the live-registry/theme-region compatibility check to
// the caller's own subsequent call to the existing, unmodified Import(id,
// targetRoute) — which already surfaces a structured, non-fatal
// compat.Result instead of hard-erroring (slice 4.6). This is the "wire
// marketplace package import through that same path, don't reimplement it"
// instruction from this ticket (P4.7) applied literally: Import itself is
// untouched by this file.
//
// principal must hold permission.PresetsManage, matching Save/Import's own
// gate — installing a marketplace package is an authoring action, not a
// public read.
//
// No entitlement-token check happens here. Per PRD §12.5's firm rule
// ("licensing gates updates and support, never execution of already-
// installed code") and this repo's own capabilities/marketplace.CanExecute
// precedent (unconditional, no token parameter at all), installing an
// already-signed, already-obtained package is treated as the same category
// of act as running already-installed code, not as a fresh purchase check —
// the entitlement/CanFetchUpdate machinery gates whether a future UPDATE
// may be fetched, never whether bytes already in hand may be installed and
// run. A future "installed packages" admin view is the natural place to
// surface entitlement/expiry status (via marketplace.DescribeExpiry) for
// display, exactly as P3.5's own tracking doc anticipated — this file does
// not add such a view, per this ticket's own scope.
func (s *Store) InstallFromPackage(ctx context.Context, principal *permission.Principal, sp marketplace.SignedPackage, pub ed25519.PublicKey, kernelVersion string) (*Record, error) {
	if !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		return nil, permission.ErrDenied
	}
	if err := marketplace.Verify(pub, sp); err != nil {
		return nil, fmt.Errorf("install preset package %q: %w", sp.Package.Name, err)
	}
	if !marketplace.CoreConstraintSatisfied(sp.Package.RequiresCore, kernelVersion) {
		return nil, fmt.Errorf("%w: package %q requires core %q, host is %q", ErrRequiresCoreNotSatisfied, sp.Package.Name, sp.Package.RequiresCore, kernelVersion)
	}
	p, err := marketplace.DecodePreset(sp.Package)
	if err != nil {
		return nil, fmt.Errorf("install preset package %q: %w", sp.Package.Name, err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}

	return s.insert(ctx, &p)
}
