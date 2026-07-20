# Implementation: Phase 3 slice 3.5 — `marketplace` capability

## Goal

PRD §14 slice 3.5: "packaging, signing, versioning, multi-vendor...
includes the paid-extension protection model (§12.5): signing,
offline-verifiable entitlement tokens, update-gating, and graceful
expiry — no source DRM, no phone-home." `docs/specs/phase3-4-spec.md`'s
Ticket P3.5 is the authoritative scope: package signing/verification,
entitlement token issuance/verification (offline-verifiable — no
phone-home to a licensing server), update-gating by entitlement status,
graceful expiry handling. PRD §12.1–§12.6 (read in full, not reproduced
here) is the authoritative source for what these four mechanisms mean and
why source-encryption DRM is explicitly rejected.

This ticket is deliberately more abstract/primitive-focused than the other
Phase 3 capabilities: there is no real marketplace registry service, no
publish-time review pipeline, and no update-fetch service to integrate
against yet (§12.6, multi-vendor marketplace, is explicit future
growth-path, not V1). What this slice builds is the cryptographic and
decision **primitives** the PRD describes — proven by real tests — not a
hosted service.

## Owning Contexts

- No `CONTEXT.md` exists yet (see `docs/agents/domain.md`); this adds one
  new package, `capabilities/marketplace`, under the existing
  `capabilities/` directory. Does not touch `capabilities/membership`,
  `capabilities/commerce`, `capabilities/notifications`,
  `capabilities/seo`, or `capabilities/forms` (membership, slice 3.4, was
  built concurrently by a separate agent in its own worktree).
- No changes to `pkg/sdk`, `pkg/kernel`, or `pkg/contract`. This package
  imports `pkg/sdk` (for `sdk.Manifest`, embedded in `Package`) but adds
  nothing to it.
- No new external dependency. Uses Go stdlib `crypto/ed25519`,
  `crypto/sha256`, `encoding/json`, and the already-present
  `golang.org/x/mod/semver` (already a direct dependency, used by
  `pkg/sdk/manifest.go`) for version-range comparison.

## Status

Complete. Built via TDD. `go build ./... && go vet ./... && go test -race
./...` green across the whole repo.

## Current Decisions

- **Not an `sdk.Plugin`.** This was the ticket's own flagged judgment call.
  Every other first-party capability in this tree
  (`capabilities/forms`/`notifications`/`seo`/`commerce`) implements
  `sdk.Plugin` because each registers a content type, admin page, or event
  subscription a host needs to grant through `HostAPI`. This capability has
  no such surface: package signing/verification and entitlement-token
  issuance/verification are pure functions over Ed25519 keys and byte
  payloads; the run-vs-update-gate decision is pure logic over a token and
  a clock. None of it needs a `HostAPI` call. Wrapping it in a
  `Plugin`/`Manifest`/`Register` ceremony would add plumbing with nothing
  real for it to gate. See `capabilities/marketplace/marketplace.go`'s
  package doc for the fuller version of this reasoning, including a note
  that a future slice adding a real "installed packages" admin view or
  wiring update-fetching into the host's capability registry is the
  natural place to grow an `sdk.Plugin` on top of these primitives.
- **`Package` embeds `sdk.Manifest` directly**, rather than reinventing a
  parallel manifest shape, per the ticket's instruction to build on the
  existing type. `Package.Name`/`Version` are kept distinct from
  `Manifest.Name`/`Manifest.Version` (registry listing identity vs.
  extension-contract identity) — documented in `package.go`'s doc comments
  as expected to match in the common case but not structurally forced to.
- **Signing payload commits to the artifact's SHA-256 hash, not the raw
  artifact bytes.** `signingPayload` JSON-encodes
  `{name, version, manifest, artifact_hash}`. This keeps sign/verify cost
  proportional to a fixed-size digest regardless of how large a real WASM
  module or RPC binary payload is, while still making the artifact
  tamper-evident (any single changed byte changes its SHA-256 hash, changes
  the signed payload, and fails `Verify`).
- **One error, `ErrInvalidSignature`, covers both "tampered" and "wrong
  key" for package verification** — documented in `package.go` as a
  deliberate consequence of Ed25519 verification's own semantics, not a
  missed distinction: a signature computed under key A over payload P and
  one computed under key B over a tampered P' are both simply "does not
  verify under this public key," and Ed25519 gives no way to tell those
  apart. This is unlike the entitlement token below, where "expired" is
  distinguishable from "invalid signature" because expiry is a check made
  independently *after* signature verification succeeds, not a property
  Ed25519 itself reports.
- **Entitlement verification order: signature first, then validity
  window.** `VerifyEntitlement` checks `ed25519.Verify` before consulting
  `NotBefore`/`NotAfter`. This is not arbitrary — the window fields
  themselves are part of the signed payload, so an unverified token's
  claimed expiry cannot be trusted enough to even look at. This ordering is
  also exactly what makes `ErrEntitlementInvalidSignature` and
  `ErrEntitlementExpired` distinguishable and mutually exclusive: a
  genuinely-issued-but-expired token passes the signature check (its bytes
  are exactly as the marketplace signed them) and only then fails the time
  check; a tampered-in-any-field token (including a forged `NotAfter`
  extension — the attack this ordering specifically forecloses) fails at
  the signature step before the time check ever runs. Proven by
  `entitlement_test.go`'s
  `TestVerifyEntitlementRejectsExpiredTokenWithSpecificReason` and
  `TestVerifyEntitlementRejectsTamperedTokenWithDistinctReason`, which each
  also assert the OTHER sentinel error is NOT satisfied — a test that would
  fail if the two ever collapsed into one generic failure.
- **The "no phone-home" property is proven two independent ways**, per the
  ticket's framing of it as the single most important property to get
  right: (1) `entitlement_test.go`'s
  `TestVerifyEntitlementDoesNotRequireNetworkAccess` globally replaces
  `http.DefaultTransport`, `http.DefaultClient`'s transport, and
  `net.DefaultResolver` with ones that fail every request/lookup
  immediately, then calls `VerifyEntitlement` and asserts it still
  succeeds — if verification made any network call at all, even one whose
  result would normally be ignored, it would fail through this broken
  transport/resolver. (2) `noimports_test.go`'s
  `TestNonTestSourceImportsNoNetworkingPackage` parses every non-test `.go`
  file in the package with `go/parser` and asserts none of them import
  `net`, `net/http`, `net/rpc`, or any other networking package at all —
  proving the capability to phone home isn't merely unused at runtime but
  structurally absent from the package's import graph. Both are kept
  intentionally redundant: the behavioral test could in principle miss a
  network path this specific test doesn't intercept; the structural test
  closes that gap by checking imports directly.
- **`CanExecute` takes zero parameters and always returns `true`** — the
  strongest form of PRD §12.5's "no license check ever disables a running
  production site" rule available: rather than accepting a token and
  ignoring it, it accepts nothing, so there is no entitlement-shaped value
  a future caller or maintainer could even attempt to wire into its
  decision. `CanFetchUpdate` is the only function in this package that
  calls `VerifyEntitlement`; `CanExecute` and `CanFetchUpdate` share no
  code. `gate_test.go`'s `TestCanExecuteNeverConflatesWithUpdateGate`
  constructs a token that is definitively, provably not entitled to update
  (via a `CanFetchUpdate` assertion first, as a sanity check the premise is
  real) and then asserts `CanExecute()` is unaffected — this is the test
  that would need to change, and would then fail, if a future edit ever
  gave `CanExecute` a token parameter and an early-return on verification
  failure.
- **`versionInRange`/`CanFetchUpdate`'s version-range check reuses
  `golang.org/x/mod/semver.Compare`**, not a hand-rolled comparison, per
  the ticket's explicit instruction. `MinVersion`/`MaxVersion` are simple
  inclusive bounds (empty = unbounded on that side) rather than
  `pkg/sdk`'s own operator-prefixed constraint-string format
  (`coreConstraintOperators`/`coreSatisfied` in `pkg/sdk/manifest.go`),
  because those helpers are unexported and specific to a single
  Requires.Core constraint; a two-field inclusive range is sufficient for
  an entitlement's "buy against 1.x" shape and simpler to reason about for
  this slice's scope. Revisit if a future need arises for entitlement
  version ranges with excluded bounds or multiple disjoint ranges.
- **`ExpiryStatus`/`DescribeExpiry` (`gate.go`) is an added, judgment-call
  extra** beyond the ticket's four numbered items — a small
  admin-page-facing classification (`active`/`expired`/`not_yet_valid`/
  `invalid`) for a future "installed packages" admin view to display,
  reusing `VerifyEntitlement`'s own logic so the two can never disagree
  about what "expired" means. It carries no gating authority of its own —
  `CanFetchUpdate`'s returned error remains the authoritative reason a
  fetch was refused. This is the "admin page for viewing installed
  packages' entitlement status" possibility the ticket explicitly flagged
  as a legitimate optional addition; implemented here only as the
  data/decision primitive (a pure function), with no actual
  `RegisterAdminPage` call or React page, consistent with this slice
  shipping no `sdk.Plugin` at all.

## Open Questions — resolved

- **Should this be an `sdk.Plugin`?** No — see Current Decisions above.
  Documented explicitly per the ticket's instruction to make and record
  this call.
- **How should "tampered" vs. "wrong key" be distinguished for package
  signatures?** They aren't — `ErrInvalidSignature` covers both, and this
  is correct given Ed25519's own semantics (see Current Decisions). The
  ticket's tamper/wrong-key test requirements are both satisfied
  (`TestVerifyRejectsTamperedArtifact`, `TestVerifyRejectsTamperedManifest`,
  `TestVerifyRejectsWrongKey` all pass and all correctly return
  `ErrInvalidSignature`); only the entitlement token (a separate type with
  a separate, later-in-time expiry check) has genuinely distinguishable
  failure reasons, per the ticket's explicit ask for tokens specifically.
- **What does "prove the no-phone-home property with a test" mean
  concretely, given this package makes no network calls at all in normal
  operation — there's nothing to *not* call?** Resolved by writing two
  complementary tests rather than one: a behavioral one that makes network
  I/O actively hostile/impossible during verification and shows
  verification still succeeds, and a structural one that proves the import
  graph has no networking package present at all. See Current Decisions
  above for the full reasoning.
- **Should update-gating and graceful-expiry be one function or two?**
  Two, non-negotiably, per the ticket's own instruction — `CanExecute` and
  `CanFetchUpdate` share no logic and no call relationship in either
  direction.

## Files/Modules Changed

- `capabilities/marketplace/marketplace.go` (new) — package doc (including
  the "why not an `sdk.Plugin`" reasoning and the mapping from this
  package's pieces to PRD §12.5's four numbered mechanisms),
  `versionInRange` and `canonicalSemver` helpers built on
  `golang.org/x/mod/semver`.
- `capabilities/marketplace/package.go` (new) — `Package` (embeds
  `sdk.Manifest`), `SignedPackage`, `Sign`, `Verify`, `ErrInvalidSignature`,
  `signingPayload` (name/version/manifest/artifact-hash JSON envelope).
- `capabilities/marketplace/entitlement.go` (new) — `Entitlement`
  (license/extension/version-range/validity-window/scope),
  `EntitlementToken`, `IssueEntitlement`, `VerifyEntitlement` (offline,
  clock-injected), `ErrEntitlementInvalidSignature`,
  `ErrEntitlementExpired`, `ErrEntitlementNotYetValid`.
- `capabilities/marketplace/gate.go` (new) — `CanExecute` (unconditional),
  `CanFetchUpdate` (entitlement- and version-range-gated),
  `ErrUpdateVersionNotEntitled`, `ExpiryStatus`/`DescribeExpiry`
  (admin-display classification).
- `capabilities/marketplace/package_test.go` (new) — sign-then-verify round
  trip; tampered-artifact rejection; tampered-manifest rejection;
  wrong-key rejection.
- `capabilities/marketplace/entitlement_test.go` (new) — valid-token
  acceptance; expired-token rejection with specific reason (and
  cross-check that it's NOT also an invalid-signature error); not-yet-valid
  rejection; tampered-token rejection with specific reason (and cross-check
  it's NOT also an expired error); wrong-key rejection;
  `TestVerifyEntitlementDoesNotRequireNetworkAccess` (the behavioral
  no-phone-home proof, breaking `http.DefaultTransport`/
  `http.DefaultClient`/`net.DefaultResolver` globally with `t.Cleanup`
  restoration).
- `capabilities/marketplace/gate_test.go` (new) — `CanExecute`
  unconditional-true test; `TestCanExecuteNeverConflatesWithUpdateGate`
  (the explicit run-vs-update-gate regression test); `CanFetchUpdate`
  in-range/out-of-range/expired cases; `DescribeExpiry` classification
  across all four states.
- `capabilities/marketplace/noimports_test.go` (new) — the structural
  no-phone-home proof: parses every non-test source file's imports via
  `go/parser` and fails if any forbidden networking package appears.
- No changes to `pkg/sdk`, `pkg/kernel`, `pkg/contract`, or any other
  `capabilities/*` package.

## Acceptance Criteria

- [x] New package `capabilities/marketplace`, not a running marketplace
      registry service.
- [x] `Package` type (name, version, manifest, artifact bytes/hash) built
      on `pkg/sdk.Manifest`.
- [x] Real Ed25519 sign/verify functions (Go stdlib `crypto/ed25519`, no
      new dependency).
- [x] Test: a genuinely signed package verifies successfully
      (`TestSignThenVerifySucceeds`).
- [x] Test: a tampered package (one byte changed after signing) fails
      verification (`TestVerifyRejectsTamperedArtifact`,
      `TestVerifyRejectsTamperedManifest`).
- [x] Test: a package signed with the wrong key fails verification
      (`TestVerifyRejectsWrongKey`).
- [x] Entitlement token type: buyer/license ID, entitled extension
      name+version range, validity window/expiry, allowed scope
      (`Entitlement`).
- [x] Signed with the marketplace's private key (Ed25519), verified using
      only the marketplace's public key baked into the verifying code, no
      network call in the verification path.
- [x] Test proving verification is genuinely offline with no network
      access available at all
      (`TestVerifyEntitlementDoesNotRequireNetworkAccess`, plus the
      structural `TestNonTestSourceImportsNoNetworkingPackage`).
- [x] Test: a valid token within its validity window verifies
      (`TestVerifyEntitlementAcceptsValidToken`).
- [x] Test: an expired token fails with a specific "expired" reason
      distinguishable from "invalid signature"
      (`TestVerifyEntitlementRejectsExpiredTokenWithSpecificReason`).
- [x] Test: a tampered token fails
      (`TestVerifyEntitlementRejectsTamperedTokenWithDistinctReason`,
      `TestVerifyEntitlementRejectsWrongKey`).
- [x] Update-gating decision function given a package's current
      entitlement token and current time (`CanFetchUpdate`).
- [x] Distinct "can this already-installed package still run" function
      that always returns true/allow regardless of entitlement status
      (`CanExecute`).
- [x] Test proving these are two different functions, never conflated —
      would fail if someone accidentally wired the run gate to check
      entitlement (`TestCanExecuteNeverConflatesWithUpdateGate`).
- [x] Graceful expiry semantics documented (package doc, `gate.go` doc
      comments) and tested where there's real logic to test
      (`CanFetchUpdate`'s expired-token case, `DescribeExpiry`'s
      classification) — no real update-fetch service exists to
      integration-test this against end-to-end, documented as deferred.
- [x] Versioning/compatibility reuses `golang.org/x/mod/semver`, not
      reinvented.
- [x] Injectable clock (`now time.Time` parameter throughout), no
      hardcoded `time.Now()` in the package's logic — expiry tests are
      deterministic without sleeping.
- [x] Documented which pieces are "real, tested, production-usable as-is"
      vs. "the shape a future registry-integration slice would call" (see
      `marketplace.go`'s package doc: signing, entitlement verification,
      and the run/update-gate distinction are all production-usable as-is;
      there is no real registry, publish-time review pipeline, or
      update-fetch service to integrate against — that remains future
      work, consistent with §12.6 being an explicit growth-path, not V1).
- [x] Does not touch `capabilities/membership`, `capabilities/commerce`,
      `capabilities/notifications`, `capabilities/seo`, or
      `capabilities/forms`.
- [x] `go build ./... && go vet ./... && go test -race ./...` green across
      the whole repo.

## Risks

- **No real registry/publish-time-review integration exists.** PRD §12.4's
  "publish-time review... static analysis of declared vs. used
  capabilities, flagging of raw-resource requests" is not implemented
  here — there is no registry service for it to run against. This
  package's `Sign`/`Verify` are the primitive a future review pipeline
  would call at the end of its own review step, not the review step
  itself.
- **No real update-fetch service exists.** `CanFetchUpdate` is a decision
  function; nothing in this repo yet calls it from an actual "check for
  updates" code path, because no such path exists yet. Its correctness is
  proven at the unit level (given a token and a clock, does it decide
  correctly), not integration-tested against a real fetch flow.
- **Revocation is out of scope**, matching PRD §12.5's own honest
  statement of the trade-off: an offline-verified token cannot learn of a
  chargeback/fraud revocation before its own `NotAfter`. This package
  implements no revocation-list mechanism; the PRD's own mitigation (short
  token windows plus online-renewal revocation-list distribution) is a
  future registry-side concern, not a primitive this slice's scope covers.
- **`ExpiryStatus`/`DescribeExpiry` has no consumer yet** (no admin page
  calls it) — shipped as a documented, tested, optional primitive per the
  ticket's own "that's a legitimate addition, your call" framing, not
  wired into any `RegisterAdminPage` call since this slice ships no
  `sdk.Plugin`.
- **`MinVersion`/`MaxVersion` inclusive-range shape may not match a future
  registry's real licensing-tier vocabulary** (e.g. "all future 1.x
  releases" vs. "exactly 1.2.3" vs. "1.2.3 and any patch release") —
  documented in Current Decisions as a deliberate scope-limited choice,
  revisit when a real registry integration defines its actual tiers.
