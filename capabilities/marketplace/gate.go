package marketplace

import (
	"crypto/ed25519"
	"errors"
	"time"
)

// This file implements PRD §12.5 mechanism 3 (update-gating) and the
// "graceful expiry — never a kill-switch" rule that follows it:
//
//	"licensing gates updates and support, never execution of
//	already-installed code."
//
// That rule is enforced here by keeping it as TWO functions that never call
// each other and share no gating logic: CanExecute (always allows) and
// CanFetchUpdate (entitlement-gated). This is a deliberate API-shape
// decision, not an incidental one — see gate_test.go's
// TestCanExecuteNeverConflatesWithUpdateGate, which is written so that it
// would fail if a future edit ever wired CanExecute to consult
// VerifyEntitlement.

// CanExecute reports whether an already-installed package's compiled/loaded
// code may continue to run. Per PRD §12.5's firm rule, this is
// UNCONDITIONAL: it takes no entitlement token, checks no signature, checks
// no expiry, and always returns true. It exists only so a call site has an
// explicit, named, greppable "yes, run it" decision to make — distinct
// from, and never merged into, CanFetchUpdate's entitlement-gated decision.
//
// A host calls this once per load, purely to have a single documented place
// that says "execution is never blocked by licensing" — not because the
// answer could ever meaningfully be false. No future change should give
// this function a token/clock parameter and start branching on it; that
// would reintroduce exactly the kill-switch PRD §12.5 rejects.
func CanExecute() bool {
	return true
}

// ErrUpdateVersionNotEntitled is returned by CanFetchUpdate when the
// entitlement token verifies and is currently valid, but its version range
// does not cover targetVersion (e.g. a license bought against 1.x being
// used to fetch a 2.0.0 update).
var ErrUpdateVersionNotEntitled = errors.New("marketplace: entitlement does not cover this update's version")

// CanFetchUpdate decides whether an update check/download may proceed for
// a package currently entitled by tok, wanting to fetch targetVersion, at
// time now. This is PRD §12.5 mechanism 3's actual gate: "the entitlement
// token's validity window is checked at download time." An expired,
// not-yet-valid, tampered, or wrongly-signed token — or a valid token whose
// version range doesn't cover targetVersion — all return false with the
// specific reason as the error; none of them affect CanExecute's answer for
// code already installed.
func CanFetchUpdate(pub ed25519.PublicKey, tok EntitlementToken, targetVersion string, now time.Time) (bool, error) {
	ent, err := VerifyEntitlement(pub, tok, now)
	if err != nil {
		return false, err
	}
	if !versionInRange(targetVersion, ent.MinVersion, ent.MaxVersion) {
		return false, ErrUpdateVersionNotEntitled
	}
	return true, nil
}

// ExpiryStatus is a human/admin-page-facing classification of an
// entitlement token's current state, for a host's own display purposes
// (e.g. an "installed packages" admin view showing entitlement health) —
// it carries no gating authority of its own; CanFetchUpdate's error is the
// authoritative reason a fetch was refused.
type ExpiryStatus string

const (
	ExpiryActive      ExpiryStatus = "active"
	ExpiryExpired     ExpiryStatus = "expired"
	ExpiryNotYetValid ExpiryStatus = "not_yet_valid"
	ExpiryInvalid     ExpiryStatus = "invalid"
)

// DescribeExpiry classifies tok's current state at time now for display,
// using the same VerifyEntitlement logic CanFetchUpdate relies on so the
// two can never disagree about what "expired" means.
func DescribeExpiry(pub ed25519.PublicKey, tok EntitlementToken, now time.Time) ExpiryStatus {
	_, err := VerifyEntitlement(pub, tok, now)
	switch {
	case err == nil:
		return ExpiryActive
	case errors.Is(err, ErrEntitlementExpired):
		return ExpiryExpired
	case errors.Is(err, ErrEntitlementNotYetValid):
		return ExpiryNotYetValid
	default:
		return ExpiryInvalid
	}
}
