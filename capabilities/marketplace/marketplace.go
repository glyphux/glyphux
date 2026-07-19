// Package marketplace is the PRD §12 marketplace/package-system capability
// (Phase 3 slice 3.5): the cryptographic and decision PRIMITIVES the PRD
// describes for packaging, signing, offline entitlement, and update-gating
// — not a running marketplace registry service. Standing up a hosted
// registry, a publish-time review pipeline (§12.4), or a real update-fetch
// service is real infrastructure outside a single vertical slice's scope;
// there is no such service to integrate against yet. What follows is the
// data model and logic a future registry-integration slice would call.
//
// This is deliberately NOT an sdk.Plugin. Every other first-party capability
// in this tree (capabilities/forms, capabilities/notifications,
// capabilities/seo, capabilities/commerce) registers content types, admin
// pages, or event subscriptions through a HostAPI, because each has some
// end-user-facing surface a host needs to grant. This capability has none
// of that: package signing/verification and entitlement-token
// issuance/verification are pure functions over Ed25519 keys and byte
// payloads, and the run-vs-update-gate decision is pure logic over a token
// and a clock. Wrapping that in a Plugin/Manifest/Register ceremony would
// add HostAPI plumbing with nothing for it to gate — no content type, no
// admin page, no event this slice needs to emit. (A future slice that adds
// an actual "installed packages" admin view, or wires update-fetching into
// the host's own capability registry, would be the natural place to grow a
// real sdk.Plugin on top of these primitives — this package is built so
// that slice can import it directly.)
//
// Four pieces, matching PRD §12.5's four-mechanism list (minus mechanism 4,
// encrypted-secrets-at-rest, which is explicitly a separate existing
// concern — §15.3 — not this ticket's job):
//
//  1. Package signing/verification (package.go) — mechanism 1, "signing
//     (integrity + authenticity)". Real, tested, production-usable as-is:
//     Ed25519 sign/verify over a package's name, version, manifest, and
//     artifact hash.
//  2. Offline-verifiable entitlement tokens (entitlement.go) — mechanism 2.
//     Real, tested, production-usable as-is: Ed25519 sign/verify with no
//     network call anywhere in the verification path (see
//     entitlement_test.go and noimports_test.go, which prove this
//     structurally as well as behaviorally).
//  3. Update-gating (gate.go) — mechanism 3. Real, tested,
//     production-usable as-is: CanExecute (always allows) is kept
//     deliberately distinct from CanFetchUpdate (entitlement-gated).
//  4. Graceful expiry semantics — not a separate function; it is the
//     documented, tested BEHAVIOR of CanExecute/CanFetchUpdate together
//     (see gate.go's doc comments and gate_test.go). There is no real
//     update-fetch service yet to integrate-test this against end-to-end;
//     what's tested here is the decision function a future update-fetch
//     call site would consult before proceeding.
package marketplace

import "golang.org/x/mod/semver"

// canonicalSemver adds the "v" prefix golang.org/x/mod/semver requires, so
// this package's own version/version-range fields can be written as plain
// "1.0.0" like pkg/sdk.Manifest's Version field and the PRD's own examples
// do. Mirrors pkg/sdk's unexported helper of the same name and purpose;
// duplicated rather than imported because pkg/sdk does not export it and
// this package should not reach into pkg/sdk's internals for a three-line
// string helper.
func canonicalSemver(v string) string {
	if v == "" || v[0] == 'v' {
		return v
	}
	return "v" + v
}

// versionInRange reports whether version falls within [min, max] inclusive,
// using golang.org/x/mod/semver for comparison rather than reinventing
// semver parsing. An empty min or max is treated as unbounded on that side.
// version, min, and max are all plain ("1.2.3") or "v"-prefixed semver
// strings; an invalid version never satisfies any range.
func versionInRange(version, min, max string) bool {
	v := canonicalSemver(version)
	if !semver.IsValid(v) {
		return false
	}
	if min != "" {
		if m := canonicalSemver(min); semver.IsValid(m) && semver.Compare(v, m) < 0 {
			return false
		}
	}
	if max != "" {
		if m := canonicalSemver(max); semver.IsValid(m) && semver.Compare(v, m) > 0 {
			return false
		}
	}
	return true
}
