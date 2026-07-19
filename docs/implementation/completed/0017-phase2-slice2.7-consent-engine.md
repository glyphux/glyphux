# Implementation: Phase 2 slice 2.7 — install-time consent engine

## Goal

PRD §10.2 ("Three Points of Consent and Enforcement") names install-time
consent as mechanism #2 — the trust surface WordPress never had: an admin
sees a structured "commerce requests: content[read,write],
payments[charge,refund], admin UI, network→api.stripe.com — Allow?" screen
and decides. This slice builds that mechanism as a real, standalone,
testable kernel component: given a plugin's `sdk.Manifest`, produce the
structured data a consent screen would render, record an admin's decision
against it, and answer — at a future plugin-load time — whether a given
manifest shape currently has a live consent on file. It sits between slice
2.1's `pkg/sdk.Manifest` (mechanism #3's input) and the boundary enforcement
slices 2.4 (WASM) and 2.5 (RPC) are building in parallel; this slice does
not wire into either of them.

## Owning Contexts

- (no CONTEXT.md exists yet — see docs/agents/domain.md; this is core-kernel
  work in a new `internal/consent` package, per the PRD's own repo-tree
  sketch listing `consent/` as a kernel subdirectory, line ~383)

## Status

Complete. Merged into `dev`; independently re-verified via full repo
build/vet/test -race, and via a hand-driven program proving a real
`consent.Engine`-approved manifest can actually be loaded by the slice 2.4
WASM runtime end to end.

## Current Decisions

- **New package `internal/consent`, not `pkg/sdk/consent` or a `pkg/consent`
  public surface.** Per the task brief and the PRD's own repo tree, consent
  is a kernel-owned, audited concern a plugin never calls itself — only the
  kernel's own future admin-ui/API layer (producing a `ConsentRequest` for a
  human) and the future WASM/RPC load path (calling `IsConsented`) do. This
  mirrors `internal/permission`, `internal/identity`: kernel machinery
  plugins never import, as opposed to `pkg/sdk`'s plugin-facing contract.

- **Real SQLite-backed persistence behind `internal/db`, migration version
  14.** Grepped `Version:` across every `internal/*/*.go` file before
  writing any migration (this project has hit migration-version collisions
  twice already — slice 0009/0010, and *again* inside 0009 itself, per
  `internal/identity/mfa.go`'s own comment about colliding with
  `internal/media/store.go`'s independently-numbered 8). Current highest at
  the time of writing: `internal/identity/mfa.go`'s "mfa challenges"
  migration at **13** (identity also holds 2, 4, 10, 11, 12; content holds
  3, 5; media holds 6, 8; composition holds 7). **14 is therefore the next
  free integer**, used for the single new `consent_decisions` table
  (`internal/consent/store.go`). Re-verified with a fresh grep immediately
  before finalizing this doc — no other in-flight slice claimed 14.

- **The `Decision` type carries both the request and the grant, not just the
  end state.** `RequestedAPI`/`RequestedPermissions` alongside
  `GrantedAPI`/`GrantedPermissions` means a partial grant's shortfall is
  itself part of the persisted audit record (PRD §10.5: "every sensitive
  grant... is recorded"), not reconstructible only by diffing against
  whatever the manifest happens to look like later.

- **"Who decided" reuses `internal/identity`'s existing `int64` user-ID
  convention (`DecidedBy int64`), not `internal/permission.Principal`, and
  does not invent a new identity concept.** `permission.Principal` is
  deliberately narrow — just a `Role` string (see its own doc comment:
  "domain packages never need to import internal/identity... just the
  role") — it carries no identifier, so it cannot answer "which admin." No
  `internal/audit` package exists yet (PRD §10.5 names an audit subsystem,
  but it isn't built — confirmed by `find internal -iname '*audit*'`
  returning nothing). Given the choice between `permission.Principal` (no
  ID) and `identity.User.ID` / `identity.Sessions.UserID` (both already
  `int64`), this slice adopts the latter's type directly rather than
  inventing a `consent.Actor` or similar. Enforcing that only an
  admin-role principal may call `Decide`/`Approve`/`Deny` is explicitly
  **not** done here — see Open Questions.

- **Partial consent is supported — an admin can approve a strict subset of
  a manifest's declared API scopes and permissions, not just an
  all-or-nothing yes/no.** The PRD's own consent-screen example (§10.2)
  reads as one combined "Allow?", which is the more literal reading and
  would have been simpler to build. This slice instead chose partial
  consent, for three reasons:
  1. The task brief's own framing of the future boundary caller's need —
     "get a clear yes/no plus enough detail to enforce a subset if the
     answer is partially" — directly names subset-granting as the shape the
     *design* should support, not just the UI interaction.
  2. §10.1's adversarial-by-default framing and §10.4's "raw resources are
     near-forbidden" both push toward *more* granular admin control, not
     less — an admin who trusts `commerce`'s `content[read,write]` but
     balks at `payments[charge,refund]` today has no way to say so under a
     strict all-or-nothing model except rejecting the whole plugin, which
     is a worse outcome for both the admin and the ecosystem than a
     reduced but honest grant.
  3. `Decision.GrantedAPI`/`GrantedPermissions` already has to exist as a
     field for the "yes" case (a future boundary needs the granted set to
     enforce, not just a boolean) — supporting a strict-subset value in
     that same field costs nothing structurally over a boolean-only design.

  `Decide` rejects any grant that is **not** a subset of the request
  (`ErrGrantExceedsRequest`, checked per-capability, per-scope, per-permission,
  per-arg) — an admin can grant less than asked, never more.
  `Status` is computed as `StatusApproved` (grant == full request),
  `StatusDenied` (grant is empty), or `StatusPartial` (anything in
  between); `Approve`/`Deny` are thin convenience wrappers over `Decide`.

- **Re-consent-on-manifest-change is detected via a `Fingerprint` — a
  SHA-256 hash of the canonicalized (sorted) `API`+`Permissions` axes —
  used as part of the persisted-decision lookup key alongside
  `PluginName`+`PluginVersion`.** `Runtime` and `Requires` are deliberately
  excluded from the fingerprint: they don't change what an admin is being
  asked to trust (proved by
  `TestRequest_ReflectsManifestAxesAndFingerprint`, which asserts the same
  fingerprint across a `Requires.Core` bump). `IsConsented` recomputes the
  fingerprint from the manifest handed to it and looks up
  `(name, version, fingerprint)` — if a plugin re-declares a different
  shape under the **same version string**, the fingerprint changes, the old
  decision doesn't match, and `IsConsented` reports `false` until the admin
  re-`Decide`s the new shape. Proved end-to-end by
  `TestReConsent_RequiredWhenManifestGainsNewCapabilityAtSameVersion`:
  approve a manifest with only `content[read]`; it gains `payments[charge]`
  at the same version; `IsConsented` on the changed shape reports `false`;
  after a fresh `Approve` on the new shape, both the new fingerprint *and*
  the original fingerprint's own still-valid decision independently report
  `true` (fingerprints don't invalidate each other — each shape's consent
  history is its own row set).

- **`IsConsented(ctx, manifest sdk.Manifest) (Decision, bool, error)`, not
  `IsConsented(pluginName, pluginVersion string) (Decision, bool)` as the
  task brief's own suggested signature sketched.** Deliberate deviation,
  documented here per the brief's own "however you name it" allowance: the
  brief itself defines the check as "has an admin consented to exactly
  *this manifest's declared scopes*" — that question is unanswerable from
  name+version alone once re-consent-on-shape-change is in scope (the whole
  point of the fingerprint mechanism above is that name+version is *not*
  sufficient to identify a consent). A future WASM/RPC load-path caller
  already has the freshly-loaded manifest in hand at exactly the point it
  needs to ask this question, so taking the manifest directly is both more
  correct and no more expensive for that caller than taking two strings
  would have been.

- **`Request(m sdk.Manifest) (ConsentRequest, error)` is a pure function —
  it does not touch the database.** Only `Decide`/`Approve`/`Deny` persist
  anything. This keeps "what would we show an admin" (safe to call
  repeatedly, e.g. to re-render a consent screen) cleanly separate from
  "the admin actually decided" (the one write path), and matches
  `sdk.Manifest.Validate()`'s own existing pattern of pure shape-checking
  functions elsewhere in this codebase.

- **No capability-dependency-graph expansion on the consent screen.**
  `pkg/sdk/capability.go`'s `Graph`/`Resolve` (slice 2.3) operates on a
  different, composition/deployment-level vocabulary (19 nodes: content,
  identity, permissions, storage, media, forms, seo, mailer, events,
  notifications, membership, payments, commerce, packaging, signing,
  marketplace, tenancy, ai, stock-media) that is explicitly decoupled from
  `Manifest.API`'s four-capability vocabulary (content, users, media,
  events) per slice 2.3's own tracking doc (0013) — "a future slice can add
  a thin translation... this package does not assume or perform it." No
  such translation exists yet anywhere in the codebase, so this slice has
  nothing to resolve a manifest's declared capabilities *through* even if
  it wanted to show "commerce pulls in payments transitively" on a consent
  screen. **Explicitly deferred** to whichever future slice builds that
  translation; `ConsentRequest` presents exactly and only what
  `Manifest.API`/`Manifest.Permissions` declare, with no expansion.

## Open Questions — resolved

- **Should `Decide`/`Approve`/`Deny` themselves enforce that only an
  admin-role principal may call them?** Resolved: no, not in this slice.
  The task brief's required surface is `Request`/`Decide`-or-`Approve`/
  `Deny`/`IsConsented`; it does not ask for authorization-gating on top of
  that, and the existing precedent (`internal/content`'s domain APIs) gates
  via `permission.Principal` + `permission.AllowsPrincipal`, which has no
  identifier field to double as `DecidedBy` (see Current Decisions above on
  why `DecidedBy` uses `identity`'s `int64` convention instead). Wiring an
  actual authorization check in front of `Decide` is a caller-side (future
  admin-ui/API-transport) concern, matching how `internal/api` — not
  `internal/content` alone — is what actually resolves a principal from a
  request today. Left as an explicit follow-up rather than guessed at.

- **Does a `Decision` need a stable identity beyond
  `(plugin_name, plugin_version, fingerprint)`?** Resolved: yes — an
  auto-increment `ID`, because decisions are stored as an **append-only
  log**, not upserted in place. Re-approving the same exact shape (e.g. an
  admin revokes then re-grants) inserts a new row rather than overwriting
  the old one; `IsConsented`/`latest` reads the most recent row
  (`ORDER BY id DESC LIMIT 1`) for a given key. This preserves a full audit
  trail per PRD §10.5 rather than only ever exposing the current state.

- **Should `fingerprintOf` include `Requires`?** Resolved: no — see Current
  Decisions' fingerprint rationale above; proved by
  `TestRequest_ReflectsManifestAxesAndFingerprint`.

## Files/Modules Changed

- `internal/consent/consent.go` (new) — `Status` (`StatusApproved`/
  `StatusPartial`/`StatusDenied`), `Fingerprint` + `fingerprintOf` (SHA-256
  over canonicalized, sorted API+Permissions), `ConsentRequest`, `Decision`,
  `ErrInvalidManifest`, `ErrGrantExceedsRequest`, `Engine` with
  `NewEngine(*db.DB) *Engine`, `Request(sdk.Manifest) (ConsentRequest,
  error)`, `Approve`/`Deny`/`Decide(ctx, ConsentRequest, grantedAPI,
  grantedPermissions, decidedBy int64) (Decision, error)`,
  `IsConsented(ctx, sdk.Manifest) (Decision, bool, error)`, plus the private
  `statusFor`/`apiScopeCount`/`validateAPISubset`/`validatePermissionSubset`
  helpers enforcing "grant ⊆ request."
- `internal/consent/store.go` (new) — `Migrations` (version 14,
  `consent_decisions` table + lookup index, with a `PostgresSQL` dialect
  override for the autoincrement PK following `internal/identity/
  identity.go`'s exact existing pattern), `ErrNotFound`, and the private
  `insert`/`latest` persistence functions (JSON-encoded API/Permission
  slices in TEXT columns, matching `internal/media/store.go`'s existing
  convention for small, non-independently-queried array-shaped columns).
- `internal/consent/consent_test.go` (new) — 9 tests, all against a real
  `db.OpenSQLite` + `d.Migrate(ctx, consent.Migrations)` (no mocks),
  matching `internal/identity`/`internal/media`'s existing test-setup
  convention exactly: `Request` reflects manifest axes and produces a
  stable fingerprint independent of `Requires`; `Request` rejects an
  invalid manifest; `Approve` → `IsConsented` reports a full grant;
  `Deny` → `IsConsented` reports not-consented; an unknown plugin reports
  not-consented; a partial `Decide` records the granted subset and
  `StatusPartial`, and `IsConsented` still reports consented with the
  reduced grant; `Decide` rejects a grant exceeding the request (both an
  extra scope on a requested capability, and a whole un-requested
  capability); the core re-consent scenario (manifest gains a new
  capability at the same version → stale consent does not apply → fresh
  approval on the new shape is honored → the original shape's own consent
  independently remains valid); and a decision persists across two
  separate `Engine` instances opened against the same on-disk SQLite file
  (proving real persistence, not an in-memory fake).

## Acceptance Criteria

- [x] A `Decision` type records: which plugin (name+version), which API-axis
      capabilities+scopes and which permissions were consented (as the
      granted subset, distinct from what was requested), when, and by whom
      (`DecidedBy int64`, reusing `identity`'s existing user-ID type) —
      `internal/consent/consent.go`.
- [x] Real persistence behind `internal/db`, with a migration at the next
      free global version number (14), documented with rationale —
      `internal/consent/store.go`.
- [x] `ConsentEngine` (`Engine`) with `Request`, `Decide`/`Approve`/`Deny`,
      and `IsConsented` — `internal/consent/consent.go`.
- [x] Re-consent on manifest change: a stale consent does not silently apply
      to a manifest that gained a new capability since the last consented
      version, proven by a real test —
      `TestReConsent_RequiredWhenManifestGainsNewCapabilityAtSameVersion`.
- [x] Partial-vs-all-or-nothing decision made and documented (partial
      chosen), with tests proving the chosen behavior —
      `TestDecide_PartialGrant_RecordsSubsetAndPartialStatus`,
      `TestDecide_RejectsGrantExceedingRequest`.
- [x] No UI rendering, no wiring into `pkg/runtime/wasm` or
      `pkg/runtime/rpc` (neither package exists yet in this worktree — both
      are parallel, not-yet-landed slices 2.4/2.5), no marketplace review,
      no runtime boundary enforcement, no changes to `pkg/sdk` — confirmed
      by `git diff` touching only `internal/consent/*` and this doc.
- [x] `go build ./...`, `go vet ./...`, and `go test -race ./...` all green
      across the whole repo (see Risks for one caveat on how this worktree
      was brought current before work began).

## Risks

- **This worktree started far behind `dev`** — its initial checkout
  predated slices 0006 through 2.6 entirely (no `pkg/sdk`, no
  `docs/implementation` beyond 0005). It was fast-forwarded onto the local
  `dev` branch (`f1bef62`, which includes slices 2.1/2.2/2.3/2.6 — note:
  *local* `dev`, not `origin/dev`, which independently lags behind local
  `dev` by the same slices) before any consent code was written. The
  fast-forward was clean (this worktree's branch had zero unique commits
  ahead of the old base), but the parent session should double-check this
  branch's base still matches `dev` at merge time, and separately confirm
  whether `origin/dev` needs to catch up too.
- **No authorization gate on `Decide`/`Approve`/`Deny` itself** (see Open
  Questions) — any caller with a `*consent.Engine` can currently record a
  decision under any `DecidedBy` value. This mirrors `internal/content`'s
  own layering (the domain API checks capabilities; nothing stops a
  misbehaving *internal* caller from fabricating a principal), but is worth
  flagging explicitly since consent decisions are a higher-stakes audit
  surface than ordinary content writes. A future admin-ui/API-transport
  slice wiring this engine in for real should resolve the acting admin's
  identity itself and pass its real ID, the same way `internal/api`
  resolves `*permission.Principal` today.
- **Append-only log with no compaction/expiry.** `consent_decisions` never
  deletes old rows, by design (audit trail), but nothing in this slice
  addresses eventual table growth for a plugin that is repeatedly
  installed/upgraded/re-consented over a long-lived deployment. Not a
  concern at this phase's scale; flagged for whoever eventually designs
  audit-log retention (PRD §10.5 names an audit subsystem that doesn't
  exist yet).
- **`IsConsented`'s "most recent row wins" semantics are untested against a
  revoke-then-re-approve-then-revoke-again sequence** beyond what the
  re-consent test exercises (which only ever moves forward: approve → gains
  scope → re-approve). The `ORDER BY id DESC LIMIT 1` lookup should handle
  this correctly by construction, but there is no explicit test titled for
  a manual revoke scenario (there is no `Revoke` method distinct from
  `Deny` — an admin "revoking" a prior approval is just calling `Deny` again
  with the same `ConsentRequest`, which files a new denied row on top).
