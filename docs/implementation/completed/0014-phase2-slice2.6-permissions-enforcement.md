# Implementation: Phase 2 slice 2.6 — manifest + two-axis permissions enforcement

## Goal

PRD §7.3 ("Two-Axis Manifest") and §10 ("Permissions and Trust Model") require
that a plugin's declared `requires.core`/`requires.contract` constraints are
actually checked against the real running kernel, and that at least one
Permission-axis grant (`network`) means something more than "declared in a
struct." Slice 2.1 built `pkg/sdk`'s `Manifest.Validate()` and `NewHostAPI`,
but explicitly deferred both: `Validate()` only checks that `requires.core`
is a well-formed constraint STRING and that `requires.contract` is
non-empty, and nothing enforced the `admin_ui`/`scheduled_jobs`/`network`
permissions beyond presence-in-a-map. This slice closes that gap.

## Owning Contexts

- (no `CONTEXT.md` exists yet in this repo — see `docs/agents/domain.md`;
  this is core-kernel work extending `pkg/sdk`, plus one new tiny package,
  `pkg/kernel`)

## Status

Complete. Reconciled by hand against 2.2's overlapping changes to
`pkg/sdk/host.go`/`manifest.go` (both sides' additions coexist; only a
test-file conflict, resolved by keeping both new test blocks) and merged
into `dev`. Independently re-verified: full repo build/vet/test -race, plus
a hand-driven program confirming requires.core rejection and exact-match
network-allowlist denial actually behave as claimed.

## Current Decisions

- **Introduced a new package, `pkg/kernel`, holding a single exported
  constant: `kernel.Version = "0.2.0"`.** No kernel-version source of truth
  existed anywhere in the repo before this slice (grepped for
  `KernelVersion`, `VERSION` files, git tags — none exist). The one existing
  candidate, `cmd/glyphux/main.go`'s unexported `version = "0.0.1-dev"` var,
  was rejected as the source of truth for two reasons: (1) it is the
  developer CLI's own display string, and that CLI is explicitly documented
  in its own package comment as "a secondary surface... never the install
  path" — not the daemon whose actual running version a plugin's
  `requires.core` should be checked against; (2) `pkg/sdk` cannot import
  `cmd/glyphux` at all (a Go command may depend on library packages, never
  the reverse — `cmd/glyphux` already imports `pkg/contract`, not vice
  versa), so reusing it was not even mechanically possible without
  restructuring the CLI, which was out of scope. `pkg/kernel` is a new
  top-level `pkg/` package (sibling to `pkg/sdk` and `pkg/contract`)
  specifically so the constant is not scattered and is trivially greppable;
  it imports nothing, so it cannot create an import cycle with anything.
  **Value chosen: `"0.2.0"`**, reflecting the kernel's actual current
  development stage (mid Phase 2 of the PRD's multi-phase build) rather
  than the PRD document's own header ("Version: 1.0.0") — the PRD describes
  a target, not what `glyphuxd` actually is today. This is a judgment call;
  bumping it is a one-line, one-file change going forward. `pkg/kernel`'s
  own doc comment explains this reasoning in place so a future reader
  doesn't have to find this doc to understand why the constant lives there.
- **`requires.core` enforcement happens inside `NewHostAPI`, not as a
  separate exported step.** `NewHostAPI` already was the single
  construction point every plugin passes through (slice 2.1); adding the
  check there (right after `manifest.Validate()`, before any HostAPI value
  is built) means a plugin whose constraint the kernel doesn't satisfy
  fails to load with no separate call a caller could forget to make. New
  sentinel: `ErrUnsupportedCoreVersion`.
- **Constraint-satisfaction logic (`coreSatisfied`) lives in `manifest.go`**,
  next to the existing `validCoreConstraint`/`coreConstraintOperators` it
  reuses — same five-operator grammar (`>=`, `<=`, `==`, `>`, `<`, or an
  unprefixed exact match), using `golang.org/x/mod/semver.Compare` (no new
  dependency, per the brief). `validCoreConstraint` checks the constraint's
  own shape (unchanged, still `Validate()`'s job); `coreSatisfied` is the
  new runtime check, called only from `NewHostAPI`.
- **`requires.contract` enforcement is a map lookup against
  `knownContractVersions`**, defined in `host.go` (not `manifest.go`,
  and not in `pkg/contract` itself) as
  `map[string]bool{string(contract.ContentCompositionV0): true}`. Placement
  reasoning: `pkg/contract` defines what a contract version IS (the
  `Version` type, `ContentCompositionV0`); whether a given PLUGIN's declared
  `requires.contract` is one this KERNEL currently supports is a
  HostAPI-construction concern, so it lives with `NewHostAPI` in `host.go`,
  which already imports `pkg/contract`. Today this set has exactly one
  member because exactly one contract version exists in the whole repo
  (confirmed by grep) — this is a single map literal specifically so adding
  a second contract version later is a one-line change here, not a
  scattered one. New sentinel: `ErrUnsupportedContract`.
- **`validManifest()`'s shared test fixture (`pkg/sdk/manifest_test.go`)
  changed its `Requires.Core` from `">=1.0.0"` (copied verbatim from the
  PRD's own §7.3 example) to `">=0.1.0"`.** This was necessary: once
  `NewHostAPI` actually checks `requires.core` against `kernel.Version`
  (`"0.2.0"`), the PRD's own example constraint is UNSATISFIED by today's
  real pre-1.0 kernel, and every one of slice 2.1's 26 `host_test.go` tests
  that call `sdk.NewHostAPI(validManifest(), ...)` would have started
  failing at construction. `">=0.1.0"` is satisfied by `"0.2.0"` and keeps
  every existing test's actual intent (gating on API/permission scopes,
  not on core-version enforcement) unchanged. `manifest_test.go`'s own
  shape-only tests (`Validate()`) are unaffected either way, since
  `Validate()` never calls `coreSatisfied`.
- **The network-permission decision primitive is
  `Manifest.AllowsNetworkHost(host string) bool`, on `Manifest` itself, not
  a free function or a `HostAPI`-only method.** Reasoning: the network
  allowlist is data owned by the manifest's `Permissions` axis, so the
  decision function is a natural method on the type that owns that data —
  testable with a bare `Manifest` literal, no `KernelDeps`/`HostAPI`
  construction needed. `HostAPI` additionally exposes
  `AllowsNetworkHost(host string) bool` as a one-line delegation to
  `h.manifest.AllowsNetworkHost(host)`, purely for the convenience of a
  future caller that already holds a `HostAPI` value (the WASM host or RPC
  broker built in slices 2.4/2.5) rather than a bare `Manifest`.
- **Network matching semantics: exact hostname match only, case-insensitive,
  no wildcard/subdomain matching, deny-by-default.** The PRD's own example
  (`network: [api.stripe.com]`) does not specify subdomain behavior. Given
  §10.1's "adversarial by default," the safer default is to require a
  plugin author to declare exactly the hosts they need rather than
  inventing an implicit `*.stripe.com`-style bypass nobody asked for —
  a plugin needing `checkout.stripe.com` and `api.stripe.com` must declare
  both. `evil.api.stripe.com` is explicitly tested as DENIED against an
  allowlisted `api.stripe.com` to make this semantic unambiguous and
  regression-proof. Empty/blank host is always denied regardless of
  allowlist contents. A manifest with no `network` permission at all denies
  every host (there is nothing to iterate).
- **This is a decision primitive, not an enforcement point — explicitly.**
  Nothing in this slice intercepts an actual outbound network call, because
  the interception points the PRD names (§10.3: "the WASM host / RPC
  broker") do not exist yet — those are slices 2.4/2.5's job. What this
  slice ships is the pure, unit-testable yes/no function
  (`AllowsNetworkHost`) those future host/broker implementations are
  expected to call before making any outbound request on a plugin's behalf.
  Until then, nothing actually stops any Tier-A in-process code (which is
  first-party/fully-trusted per §8.1 anyway, and doesn't go through a
  network-mediating boundary at all today) from making arbitrary network
  calls — that is unchanged by this slice and is not a regression this
  slice introduces, since no such interception existed before either.
- **`admin_ui`/`scheduled_jobs`: no additional enforcement added beyond
  slice 2.1's existing presence check, as a deliberate, explicit
  conclusion, not an oversight.** Both permissions carry no `Args` at all
  (`Validate` rejects a non-network permission carrying args) — there is no
  finer-grained data on either grant for a deeper check to inspect (unlike
  `network`, whose `Args` is exactly the allowlist a decision function has
  something to compare against). §7.3's "scrutinized, risk-graded" language
  describes the MARKETPLACE REVIEW and INSTALL-TIME CONSENT points (§10.2's
  points 1 and 2 — a human reviewer grading risk, an admin's consent
  screen), not a THIRD runtime-enforcement axis beyond "is this permission
  present." Runtime enforcement (§10.2 point 3, "the boundary... exposes
  ONLY declared-and-consented capabilities") for these two is exactly what
  `RegisterAdminPage`/`RegisterJob` already do: a call absent the
  permission is not merely denied, the registration methods themselves
  refuse to record anything without it. There is no deeper artifact behind
  either grant today (no admin-page-scoping vocabulary, no
  job-frequency/resource-limit vocabulary exists in the PRD or this
  codebase) for a richer check to enforce against. Revisit if a later slice
  introduces such a vocabulary (e.g. per-page route scoping, job-frequency
  caps).

## Open Questions — resolved

- **Should `coreSatisfied` live in `manifest.go` or `host.go`?** Resolved:
  `manifest.go`, next to the constraint-shape validation it's a runtime
  counterpart to, and because it only needs `Requires.Core` and a version
  string — no `KernelDeps`/`HostAPI` machinery. `host.go`'s `NewHostAPI` is
  the only caller.
- **Should `knownContractVersions` live in `pkg/contract` instead, so the
  contract package is self-describing about what versions exist?**
  Resolved: no — `pkg/contract` already has exactly one contract-version
  constant total (`ContentCompositionV0`); a "known versions" set there
  today would be a redundant single-element mirror of that one constant.
  The genuinely new information this slice adds is "the KERNEL currently
  accepts these," which is a HostAPI/plugin-loading concern, not a
  contract-shape concern — keeping it in `host.go` also means slice 2.3
  (capability registry) or a future contract-versioning slice can extend
  this map without touching `pkg/contract` at all.
- **Does `AllowsNetworkHost` belong on the `HostAPI` interface at all, given
  slice 2.2/2.3 are independently extending the same interface in parallel
  branches?** Resolved: yes, added as one new method — it is purely
  additive (no existing method signature changed), scoped entirely to this
  slice's own permission concern, and unrelated to `On`/`Emit` (2.2) or any
  capability-graph/resolver surface (2.3), so it should not conflict when
  the parent session merges all three branches. If it does textually
  conflict in the interface's method list, resolution is trivial (it's an
  additive one-line entry, not a body edit).

## Files/Modules Changed

- `pkg/kernel/kernel.go` (new) — `Version` constant, with a doc comment
  explaining why this package exists and why `cmd/glyphux`'s CLI version
  var was not reused.
- `pkg/kernel/kernel_test.go` (new) — one test guarding that `Version`
  stays a valid semantic version as it's hand-bumped over time.
- `pkg/sdk/manifest.go` — added `Manifest.AllowsNetworkHost(host string)
  bool` (exact-match, case-insensitive, deny-by-default network-allowlist
  decision primitive) and the unexported `coreSatisfied(constraint,
  kernelVersion string) bool` helper.
- `pkg/sdk/manifest_test.go` — added 6 tests for `AllowsNetworkHost` (deny
  without the permission, allow on exact match, deny on unlisted host, deny
  on subdomain of an allowlisted host, case-insensitive match, deny empty
  host); changed the shared `validManifest()` fixture's `Requires.Core`
  from `">=1.0.0"` to `">=0.1.0"` (see Current Decisions) with a doc
  comment explaining why.
- `pkg/sdk/host.go` — added `ErrUnsupportedCoreVersion`,
  `ErrUnsupportedContract`, `knownContractVersions`; `NewHostAPI` now checks
  both after `manifest.Validate()` and before constructing the `hostAPI`
  value; added `AllowsNetworkHost(host string) bool` to the `HostAPI`
  interface and its `hostAPI` implementation (one-line delegation to
  `Manifest.AllowsNetworkHost`).
- `pkg/sdk/host_test.go` — added 7 tests: core-constraint satisfied/
  unsatisfied (both `>=`-prefixed and exact-match forms), known/unknown
  `requires.contract`, and `HostAPI.AllowsNetworkHost`'s delegation.

## Acceptance Criteria

- [x] A plugin whose `requires.core` constraint the running
      `kernel.Version` does not satisfy fails `NewHostAPI` with
      `ErrUnsupportedCoreVersion` — `TestNewHostAPIRejectsCoreConstraintKernelDoesNotSatisfy`,
      `TestNewHostAPIRejectsExactCoreConstraintNotMatchingKernelVersion`.
- [x] A plugin whose `requires.core` constraint the kernel DOES satisfy
      (both a `>=`-relative constraint and an exact-match constraint)
      succeeds — `TestNewHostAPIAcceptsCoreConstraintKernelSatisfies`,
      `TestNewHostAPIAcceptsExactCoreConstraintMatchingKernelVersion`.
- [x] A plugin declaring an unknown/unsupported `requires.contract` fails
      `NewHostAPI` with `ErrUnsupportedContract` —
      `TestNewHostAPIRejectsUnknownContractVersion`; a known one
      (`contract.ContentCompositionV0`) succeeds —
      `TestNewHostAPIAcceptsKnownContractVersion`.
- [x] `Manifest.AllowsNetworkHost` is a real, unit-testable, deny-by-default
      decision function: denies with no `network` permission at all, allows
      an exact allowlisted host, denies an unlisted host, denies a
      subdomain of an allowlisted host (proving no implicit wildcarding),
      matches case-insensitively, and denies an empty host — 6 tests in
      `manifest_test.go`, plus `TestHostAPIAllowsNetworkHostDelegatesToManifest`
      proving `HostAPI.AllowsNetworkHost` really delegates rather than
      reimplementing/diverging.
- [x] Every slice-2.1 test that already existed still passes unmodified in
      behavior (only the shared `validManifest()` fixture's `Requires.Core`
      string changed, to stay satisfiable by the new enforcement — no
      test's actual assertion changed).
- [x] `go build ./...`, `go vet ./...`, and `go test -race ./...` all green
      across the whole repo (20 testable packages, all `ok`, including the
      new `pkg/kernel`).

## Risks

- **`kernel.Version = "0.2.0"` is a manually-chosen, manually-maintained
  constant with no automated derivation** (no git-tag-based versioning, no
  build-time injection). It will silently go stale if a future slice adds
  meaningful kernel capability without bumping it, and nothing currently
  enforces that the bump happens. This mirrors `cmd/glyphux`'s own
  `version` var's existing informal-maintenance status; unifying the two
  (e.g. having the CLI report `kernel.Version` too, or introducing real
  build-time version injection) was judged out of scope for this slice and
  is a reasonable follow-up.
- **Network enforcement is a decision primitive only — this is a real,
  explicitly-flagged gap, not an oversight.** Until slices 2.4/2.5 build
  the WASM host and RPC broker and wire `AllowsNetworkHost` into whatever
  makes actual outbound HTTP calls on a plugin's behalf, a plugin's
  declared `network` allowlist has zero actual effect on any real network
  traffic. Anyone reading only the manifest/HostAPI layer could mistakenly
  believe network permissions are already enforced; this doc and the
  function's own doc comment both say otherwise.
- **Exact-hostname-only matching is a judgment call that could prove too
  strict in practice** — a plugin integrating with a provider that uses
  many rotating/regional subdomains would need to enumerate all of them.
  If slice 2.4/2.5 or real marketplace usage surfaces this as a real
  friction point, revisiting to support an explicit (not implicit)
  wildcard syntax (e.g. requiring authors to write `*.stripe.com`
  literally, still deny-by-default for anything not matching) is a
  contained, single-function change — it does not touch `Validate()`'s
  existing "network permission requires at least one arg" shape check.
