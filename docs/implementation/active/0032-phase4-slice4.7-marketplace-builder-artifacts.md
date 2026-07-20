# Implementation: Phase 4 slice 4.7 — marketplace distribution of builder artifacts

## Goal

PRD §13.4 ("Marketplace Distribution and Monetization"): "all four
distributable artifacts flow through the one signed-package marketplace...
free and paid block plugins, composition presets, composition bundles, and
themes. Each carries a manifest with `license`, `requires.core`,
`requires.contract`, declared block dependencies, and (for presets/bundles)
theme compatibility." `docs/specs/phase3-4-spec.md`'s Ticket P4.7 one-liner:
"signed packages for blocks/presets/bundles/themes, building on P3.5's
signing infrastructure." This ticket extends the existing
`capabilities/marketplace` package (Phase 3 slice 3.5 — Ed25519 package
signing/verification, offline-verifiable entitlement tokens, update-gating,
graceful expiry) to actually package and import two of the four artifact
kinds, and documents why the other two stop at "structurally supported" for
this slice.

## Scope decision — presets/bundles implemented, blocks/themes documented as an extension point

**Implemented for real, exercised end-to-end by tests:** composition
presets and composition bundles (`pkg/contract.CompositionPreset`/
`CompositionBundle`, slice 4.6). Both are pure JSON documents with no
compiled code inside them, so "package and sign a preset" is exactly
"marshal it, hash it, sign the hash" — the existing `capabilities/
marketplace.Sign`/`Verify` machinery applies with no structural change.

**Deliberately NOT implemented, documented as a structurally-supported
extension point:** blocks and themes as *installable marketplace packages*.
Investigated both:

- A `pkg/blocks.Definition` is itself pure data (name, prop schema, slot
  names — no code), so packaging a *definition* would be as
  straightforward as a preset. But a block's actual runtime behavior isn't
  expressed by `Definition` at all in this codebase — first-party blocks
  (`blocks/firstparty`) are compiled-in Go, and `pkg/sdk.HostAPI.
  RegisterBlock` is only ever called by already-loaded Tier-A (in-process)
  or already-loaded Tier-B/C (WASM/RPC) plugin code. There is no existing
  "fetch a signed package, load its code, call RegisterBlock from inside
  it" runtime path.
- A `pkg/theme.Theme` is likewise pure Go code implementing an interface
  (`themes/starter`, `themes/headless`) with no serialized "theme document"
  analogous to a preset — there's nothing for a theme package's artifact
  bytes to *be* other than "a compiled plugin module."
- Checked `pkg/runtime/wasm` and `pkg/runtime/rpc` directly: both host and
  execute an **already-loaded** plugin instance (Wazero module / RPC
  broker). Neither resolves "signed package bytes sitting on disk" into "a
  loaded, running plugin instance" — that resolution step (fetch → verify →
  load into a `*wasm.Runtime` or spawn an RPC process → call
  `RegisterBlock`/register the theme) doesn't exist yet for **any** kind,
  including the plugin/theme kind the original P3.5 `Package` type already
  nominally covers. Building it is a plugin-installation-lifecycle project
  of its own, not an additive extension of this ticket's packaging/
  compatibility scope.

Given that, the concrete implementation is scoped to presets and bundles —
the two kinds that are genuinely just data — and blocks/themes are recorded
as `ArtifactKind` enum values (`KindBlock`, `KindTheme`) with no packaging/
decoding helper, exactly the "structurally-supported-but-not-yet-exercised
extension point" framing the ticket asked for if the runtime-loading gap
turned out to be real. It is real: confirmed by reading `pkg/runtime/wasm/
runtime.go` and `pkg/runtime/rpc/broker.go` and `hostserver.go` in full —
both assume a caller already has a loaded module/process in hand; neither
owns "resolve a package into a loaded module."

## What was built

**`capabilities/marketplace`** (extended, not forked):

- `artifact_kind.go` (new) — `ArtifactKind` (`KindPlugin`, `KindTheme`,
  `KindBlock`, `KindPreset`, `KindBundle`), with the scope-boundary
  reasoning above recorded directly in its doc comment.
- `package.go` (edited) — `Package` gains `Kind ArtifactKind`, `License
  string`, `RequiresCore string`. `signingPayload`'s envelope now includes
  all three, so a tampered `License` (e.g. quietly relabeling a paid
  preset "free") or a loosened `RequiresCore` fails `Verify` exactly like a
  tampered artifact byte or a tampered `sdk.Manifest.Permission` already
  did. `Manifest sdk.Manifest` is unchanged and stays meaningful only for
  `KindPlugin`/legacy packages — presets/bundles carry their own
  compatibility manifest *inside* the artifact (see below), not through
  this field.
- `composition.go` (new) — `PackagePreset`/`DecodePreset` and
  `PackageBundle`/`DecodeBundle`, plus `CoreConstraintSatisfied` (a small,
  local re-implementation of `pkg/sdk`'s operator-prefixed semver-
  constraint format — `>=`, `<=`, `==`, `>`, `<`, or exact match with no
  prefix — since `pkg/sdk`'s `coreSatisfied`/`validCoreConstraint` are
  unexported; mirrors this package's own P3.5 precedent of small local
  semver helpers over reaching into `pkg/sdk` internals).
- `composition_test.go` (new) — round-trip packaging/decoding for both
  kinds; publish-time self-consistency rejection (a preset/bundle whose
  manifest under-declares what its composition tree actually uses is
  refused a `Package` at all, never gets signed); kind-mismatch decode
  rejection; tamper detection specifically for the two new fields
  (`License`, `RequiresCore`); `CoreConstraintSatisfied` table test.

**`pkg/contract.Manifest` reused as-is, not reinvented.** PRD §13.4's
manifest requirements map directly onto fields that already exist:
`requires.contract` → `contract.Manifest.RequiresContract` (already on
every `CompositionPreset`/`CompositionBundle` since slice 4.6); "declared
block dependencies" → `contract.Manifest.Blocks`; "theme compatibility" →
`contract.Manifest.Slots`/`Themes`. The only two PRD-required fields that
had no existing home anywhere — `license` and `requires.core` — are the
only two new fields added, at the `marketplace.Package` level (not
`contract.Manifest`), because they're marketplace/kernel-distribution
concerns, not composition-authoring concerns, and because a preset/bundle
has no `sdk.Manifest` to carry `requires.core` the way a plugin package
does. This is the "small, additive extension" the ticket asked for, not a
parallel manifest shape.

**Publish-time compatibility check** (PRD §13.4/§13.3: "checked at publish
and import"): `PackagePreset`/`PackageBundle` call `preset.Validate()`/
`bundle.Validate()` (structural) and then `pkg/compat.CheckPreset`/
`CheckBundle` — reusing slice 4.6's existing compatibility-contract engine
verbatim — against a **synthetic** `*blocks.Registry` built purely from the
artifact's own declared `Manifest.Blocks` (never a live/real registry; see
`composition.go`'s `syntheticRegistry`/`mergeRegistry` helpers). This
catches "the manifest under-declares what the composition tree actually
uses" before the artifact is ever signed — `PackagePreset`/`PackageBundle`
return the artifact's own `compat.Result` alongside a non-nil error and
build no `Package` at all when it's not self-consistent.

**Import-time compatibility check reuses slice 4.6's `Import` UNCHANGED —
the ticket's explicit instruction, followed literally:**

- `internal/preset/marketplace.go` (new) — `Store.InstallFromPackage`:
  verifies `sp`'s signature under the marketplace's public key
  (`marketplace.Verify`), checks `sp.Package.RequiresCore` against the
  running kernel version (`marketplace.CoreConstraintSatisfied`, compared
  against `pkg/kernel.Version` at the call site), decodes the artifact
  (`marketplace.DecodePreset`), structurally validates it, and persists it
  under a fresh ID via a new shared `Store.insert` helper — **without**
  running `Save`'s own `compat.CheckPreset` gate against the live registry.
  This is deliberate: `Save`'s own doc comment already explains it requires
  full live-registry compatibility because a preset saved through it is
  "authored locally, from blocks the author actually has" — a marketplace
  package is exactly the *other* case that same doc comment flags
  (`Import`'s case): "built somewhere else... must tolerate (and explain,
  not silently drop) a preset built against a block this host lacks."
  `InstallFromPackage` persists unconditionally (once signature/
  requires.core clear) and leaves live-registry/theme-region compatibility
  entirely to the caller's own subsequent call to the pre-existing,
  **byte-for-byte untouched** `Store.Import(id, targetRoute)`, which
  already returns a structured `compat.Result` instead of hard-erroring.
- `internal/bundle/marketplace.go` (new) — `Store.InstallFromPackage`,
  the exact bundle-shaped mirror, same reasoning, feeding into the
  pre-existing, untouched `Store.Import`.
- `Store.Save` in both packages was refactored (not behaviorally changed)
  to extract its ID-generation/marshal/INSERT tail into a private `insert`
  helper, shared by `Save` (after `Save`'s own compat-gated validation) and
  `InstallFromPackage` (after its own, deliberately different, validation)
  — avoiding a second copy of the INSERT SQL rather than accepting
  duplication.
- `internal/preset/marketplace_test.go`, `internal/bundle/marketplace_test.go`
  (new) — real end-to-end tests with real Ed25519 keys, a real signed
  package, a real SQLite-backed store: happy path (host has the declared
  block → install, then `Import` merges); graceful-degradation path (host
  lacks the declared block → install still succeeds, `Import` declines with
  a structured `compat.Result`, nothing merges — proving `InstallFromPackage`
  never short-circuits the "explainable, not silently rejected" behavior
  `Import` already guarantees); tampered-artifact rejection; wrong-signing-
  key rejection; unsatisfied-`requires.core` rejection; capability-gate
  rejection (non-admin, anonymous).

## Current decisions

- **No entitlement-token check in `InstallFromPackage`.** Per PRD §12.5's
  firm rule ("licensing gates updates and support, never execution of
  already-installed code") and this repo's own `capabilities/marketplace.
  CanExecute` precedent (unconditional — no token parameter at all,
  specifically so no future caller could wire one in), installing an
  already-signed, already-obtained package is treated as the same category
  of act as running already-installed code, not as a fresh purchase check.
  The entitlement/`CanFetchUpdate` machinery exists to gate whether a
  future *update* may be fetched — it says nothing about whether bytes
  already in hand may be installed and run. A future "installed packages"
  admin view (flagged as unbuilt in P3.5's own tracking doc) is the natural
  place to surface entitlement/expiry status for *display*
  (`marketplace.DescribeExpiry`) — this ticket does not add that view; it
  is out of scope here as it was in P3.5.
- **`RequiresCore` lives on `Package`, not inside `contract.Manifest`.**
  `contract.Manifest.RequiresContract` already exists and is reused as-is
  for the Layer-2 layout-contract-version half of PRD §13.4's requirement.
  `requires.core` (kernel-version compatibility) has no equivalent home in
  `contract.Manifest` and adding one there would conflate a
  composition-authoring concern with a marketplace-distribution concern —
  a locally-authored preset saved via `Store.Save` has no notion of "which
  kernel version" at all today, and this ticket didn't want to invent one
  just to reuse a field name. Keeping it on `marketplace.Package` also
  gives every kind (including the not-yet-implemented plugin/theme kinds) one
  uniform place to declare it, rather than `sdk.Manifest.Requires.Core` for
  one kind and something else for another.
- **Publish-time self-check uses a *synthetic* registry, never a live
  one.** `PackagePreset`/`PackageBundle` deliberately do not accept a
  `*blocks.Registry` parameter at all — the publish-time question is "is
  this artifact internally consistent with what it itself declares," not
  "does some particular host have these blocks." Passing a real registry
  in would blur that line and let a publish-time check accidentally start
  depending on which host happened to run it.
- **`InstallFromPackage` hard-rejects `requires.core` before decoding
  proceeds to a compatibility check at all** (`ErrRequiresCoreNotSatisfied`,
  declared separately but identically in both `internal/preset` and
  `internal/bundle`, mirroring how `ErrNotFound` is already independently
  declared in both rather than shared). Reasoning: an artifact declaring it
  needs a newer kernel than this host runs cannot be meaningfully evaluated
  for block/slot compatibility at all, since the compatibility-contract
  types/semantics it was authored against may themselves have changed by
  that kernel version. This is a hard gate, unlike the block/slot
  mismatch, which `InstallFromPackage` tolerates by design (see above).
- **No new DB migration.** Both `internal/preset` and `internal/bundle`
  already have their own tables (migrations 17 and 18, the two most recent
  before this ticket); `InstallFromPackage` writes through the exact same
  `presets`/`bundles` tables `Save` already uses — a marketplace-sourced
  preset/bundle is stored identically to a locally-authored one once
  persisted, indistinguishable by any `Get`/`List`/`Import` caller. There is
  no "installed marketplace packages" registry/ledger table in this ticket
  — that would be a real "installed packages admin view" feature, flagged
  as future work by P3.5's own tracking doc and not required by this
  ticket's scope. Re-verified the next-free migration slot live by
  grepping `Version:` across `internal/*/*.go` before writing any code:
  18 (`internal/bundle`) remains the highest; nothing here adds 19.

## Files/modules changed

- `capabilities/marketplace/artifact_kind.go` (new)
- `capabilities/marketplace/composition.go` (new)
- `capabilities/marketplace/composition_test.go` (new)
- `capabilities/marketplace/package.go` (edited — `Kind`/`License`/
  `RequiresCore` fields, `signingPayload` envelope)
- `internal/preset/marketplace.go` (new)
- `internal/preset/marketplace_test.go` (new)
- `internal/preset/store.go` (edited — `Save`'s INSERT tail extracted into
  a shared private `insert` helper; no behavior change to `Save` itself)
- `internal/bundle/marketplace.go` (new)
- `internal/bundle/marketplace_test.go` (new)
- `internal/bundle/store.go` (edited — same `insert`-extraction refactor)
- No changes to `pkg/contract`, `pkg/compat`, `pkg/blocks`, `pkg/theme`,
  `pkg/runtime/wasm`, `pkg/runtime/rpc`, or any admin-ui/sdk-js code (this
  is a backend-only ticket — no builder UI surface for "install a
  marketplace package" exists yet; that would be a future slice consuming
  `InstallFromPackage` from an HTTP handler).

## Acceptance criteria

- [x] `capabilities/marketplace`'s existing signed-package format extended
      to cover preset and bundle artifacts, reusing `Sign`/`Verify`/
      `SignedPackage` unchanged.
- [x] Package manifest carries `license`, `requires.core` (new,
      `Package`-level), `requires.contract`, declared block dependencies,
      and theme compatibility (all four reused from `pkg/contract.Manifest`
      via the artifact itself).
- [x] Compatibility checked at publish time (`PackagePreset`/
      `PackageBundle` reject a self-inconsistent artifact, never producing
      a `Package` for it to be signed).
- [x] Compatibility checked at import time via slice 4.6's existing
      `Import` methods — untouched, not reimplemented.
- [x] P3.5's signing/entitlement/update-gating/expiry code reused, not
      forked; the only additive change is `Kind`/`License`/`RequiresCore`
      on `Package` plus the new `composition.go` helpers.
- [x] Blocks/themes scope boundary investigated (`pkg/runtime/wasm`,
      `pkg/runtime/rpc` read in full) and documented as a
      structurally-supported-but-not-yet-exercised extension point
      (`ArtifactKind`'s `KindBlock`/`KindTheme`), not silently dropped.
- [x] TDD: real Ed25519 signing, real SQLite-backed stores, real
      `pkg/compat` checks throughout — no mocks.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide (verified before opening the PR).
- [x] No `admin-ui`/`sdk-js` changes — backend-only ticket, confirmed by
      the Files/modules list above.
- [x] Migration version re-verified live (18 remains the highest; no new
      migration added by this ticket).

## Risks / follow-ups

- **No real marketplace registry/delivery service** to publish through or
  install from — same limitation P3.5 already carries forward; this ticket
  adds packaging/import primitives a future registry-integration slice
  would call, not a hosted service.
- **No "installed packages" admin view** surfacing entitlement/expiry
  status for installed presets/bundles — flagged as future work by both
  P3.5's tracking doc and this one; `marketplace.DescribeExpiry` already
  exists and is ready for such a view to call.
- **Blocks/themes remain undeliverable as marketplace packages** until a
  future slice builds the plugin-installation-lifecycle step (resolve
  signed bytes → loaded WASM module or spawned RPC process →
  `RegisterBlock`/theme registration). `ArtifactKind`'s `KindBlock`/
  `KindTheme` are ready for that slice to fill in without another `Kind`-
  shaped migration.
- **No HTTP/admin-UI surface calls `InstallFromPackage` yet** — this
  ticket is the backend primitive; wiring a real "install this marketplace
  package" flow into `internal/api` and the builder UI is future work, not
  required by this ticket's own acceptance criteria (which is scoped to
  packaging + compatibility-gated import, per the ticket's own framing of
  that as "probably the highest-value behavior to test").
