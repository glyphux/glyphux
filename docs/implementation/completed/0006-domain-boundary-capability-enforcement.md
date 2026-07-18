# Implementation: Domain-API boundary capability enforcement

## Goal

PRD §1.8 ("Permission engine v1: capability-based permissions... enforced at
the domain-API boundary") and §10.5 ("The Boundary Is the Enforcement
Point") both require capability checks to live at the domain-API boundary —
`internal/content`, `internal/composition`, `internal/media` — not only in
the HTTP/GraphQL transport handlers that happen to call them today.

A full audit (2026-07-18, two-axis code-review of dev vs main) found this is
currently false: `internal/content.API` has zero capability checks and never
imports `internal/permission`. Enforcement exists only in
`internal/api/auth.go`'s `requireCapability` and `internal/graphql/auth.go`'s
identical reimplementation. Any code holding a `*content.API`,
`*composition.Store`, or `*media.API` reference — a future plugin host, a
background job, a test that forgets the transport layer — has unchecked
access. This is the single most architecturally significant finding from
the audit.

## Owning Contexts

- (no CONTEXT.md exists yet in this repo — see docs/agents/domain.md; this
  is core-kernel work: `internal/content`, `internal/composition`,
  `internal/media`, `internal/permission`)

## Status

Completed. Merged forward into `dev` and the graphql/pkg-client/sdk-js gap
this worktree's branch couldn't cover (see the caveat below) has been
closed — see "Merge-forward addendum" at the end of this doc.

Completed (with one important scope caveat below — read before merging).

**Scope caveat, important for the merge into `dev`:** the worktree this
slice was implemented in was branched from an ancestor commit of `dev` that
predates `internal/graphql`, `pkg/client`, and the `sdk-js` Go test fixture
(`sdk-js/testdata/seedcomposition/main.go`) — none of those exist in this
worktree's checkout, only `internal/api` (REST) does. Everything this doc
originally asked for in those packages **could not be done here because the
files don't exist in this branch's history**. The changes below cover
`internal/permission`, `internal/content`, `internal/composition`,
`internal/media`, and `internal/api` (+ `internal/setup`) completely and
verifiably. **Whoever merges this into current `dev` must also apply the
equivalent principal-plumbing to `internal/graphql`'s handlers and any
`pkg/client`/`sdk-js` test-server construction that constructs `content.API`/
`composition.Store`/`media.API` calls directly** — the signatures changed
(principal added as the argument right after `ctx`) exactly as this doc
predicted, so those call sites will fail to compile against the new
signatures and need the same one-line-per-call-site treatment applied to
`internal/api` in this slice (see the `internal/api` commit for the pattern:
`s.principal(r)` resolves the request's caller into a `*permission.Principal`
once, passed through to every domain-API call).

## Current Decisions

- Domain APIs accept a caller identity/role and check it internally before
  performing any read/write/delete/publish operation — not trust the caller
  to have already checked. Implemented as `*permission.Principal` (see Open
  Questions — decision made below), passed as the argument immediately after
  `ctx` on every mutating (and draft-exposing) method.
- Each domain API method calls `permission.AllowsPrincipal(principal,
  capability)` itself and returns `permission.ErrDenied` — a new sentinel in
  `internal/permission` — on denial. Transports map it to 403 (`internal/api`'s
  `writeContentError`/`writeMediaError` both gained a
  `errors.Is(err, permission.ErrDenied)` case). In practice this transport-side
  mapping is unreachable through the normal HTTP surface today, because
  `internal/api`'s existing `requireCapability`/`canReadDrafts` checks already
  gate every route the domain layer also gates — the domain-level check is
  true defense-in-depth, only reachable by a caller that holds a
  `*content.API`/`*composition.Store`/`*media.API` reference directly (a
  plugin host, a background job, a test) and skips the transport layer, which
  is exactly the gap the audit flagged.
- `internal/api` keeps its existing `requireCapability`/`canReadDrafts`
  helpers for the fast-fail 401/403 at the transport edge. A new
  `(*Server).principal(r)` helper in `internal/api/auth.go` resolves the
  request's authenticated user (if any) into a `*permission.Principal{Role}`
  once, and every content/media domain-API call site now passes it through.
  Both layers check; the transport-level checks were not removed.
- Anonymous/public reads stay a `nil`-principal, capability-free path,
  implemented as a set of methods that never took (and still don't take) a
  principal at all, rather than a "public role" — `GetPublished`,
  `ListPublished`, `GetLocalizedPublished`, `ListLocalizedPublished`,
  `GetPing` (content) and `Get`, `List`, `Open`, `Resize`, `StoragePath`
  (media, already public) remain principal-free and unchanged. This was
  chosen over inventing an anonymous role in the capability matrix because
  `permission.Allows("", capability)` already correctly returns false for
  every real capability — public reads are "no check happens here", not "the
  anonymous role happens to hold content:read".
  `internal/content.ListVersions` was deliberately **left principal-free
  too** even though it exposes historical (potentially-draft) version data —
  it was already reachable with zero auth before this change (verified via
  the pre-existing `TestContentPublishVersionsRollbackOverHTTP` test, which
  hits `GET .../versions` anonymously and asserts 200), and this slice's
  acceptance criteria require external HTTP behavior to stay byte-for-byte
  identical. Tightening that pre-existing gap is out of scope here and
  should be its own follow-up note if wanted.
- This changed exported method signatures on `content.API` (`Create`, `Get`,
  `List`, `GetLocalized`, `ListLocalized`, `Update`, `Delete`, `Publish`,
  `Unpublish`, `Rollback`), `media.API` (`Upload`, `Delete`), and
  `composition.Store` (`Save`). Every call site in `internal/api` and every
  `_test.go` in `internal/content`, `internal/media`, `internal/api`,
  `internal/setup` was updated in the same change. (`internal/graphql`,
  `pkg/client`, `sdk-js` do not exist in this worktree's branch — see the
  Status caveat above.)
- `composition.Store.Save` needed one special case beyond "always check
  `ContentTypesManage`": the very first `Save` (first-run setup, before any
  composition or admin account exists) cannot require a principal, because
  none can exist yet at that point in the flow. `Save` now checks
  `Exists(ctx)` first; if a composition already exists, the caller must hold
  `content-types:manage`; if none exists yet, the write is allowed
  unconditionally — that bootstrap write's real security boundary is
  `internal/setup`'s own one-time token + localhost check, which was already
  there and unchanged. `internal/setup/setup.go`'s call site now passes `nil`
  explicitly with a comment explaining why.
- Added `permission.ContentTypesManage` (`"content-types:manage"`) to the
  capability matrix, admin-only, since this worktree's `composition.Store`
  had no `DefineContentType`/`RemoveContentType` methods to attach it to (see
  Status caveat — those don't exist in this branch's history yet); it is
  attached to `Save`, the only mutating method that exists here. A future
  content-type-management endpoint/method built on top of `Save` (or
  replacing it with `DefineContentType`/`RemoveContentType`) automatically
  inherits this check.
- **Deferred, not done here:** the tenant-dimension no-op parameter on
  `permission.Allows`/`AllowsPrincipal` (§11.5's forward-compat seam). Per
  this doc's own stated escape hatch ("if it turns out non-trivial, split it
  into its own follow-up note instead of blocking this slice"), it was judged
  non-trivial enough to risk this slice's primary goal: it would touch every
  call site changed in this slice a second time (all of `internal/content`,
  `internal/media`, `internal/composition`, `internal/api`, plus the
  `internal/graphql`/`pkg/client`/`sdk-js` sites this slice couldn't reach at
  all) for a parameter that is a true no-op today. Left as an explicit
  follow-up rather than silently dropped.

## Open Questions — resolved

- **Principal shape: resolved as `permission.Principal{Role string}`,** a
  new exported type added to `internal/permission` (not `*identity.User`).
  `internal/content`, `internal/composition`, and `internal/media` import
  `internal/permission` (which they needed anyway to call
  `Allows`/`AllowsPrincipal`) and never import `internal/identity` — the
  layering goal this doc asked for. `internal/api/auth.go` gained a
  `(*Server).principal(r) *permission.Principal` helper that narrows the
  already-resolved `*identity.User` down to this shape at the transport
  boundary, so identity's full shape never crosses into the domain packages.
  `permission.RoleOf(p *Principal) string` and
  `permission.AllowsPrincipal(p *Principal, capability) bool` are the two
  nil-safe helpers domain code calls; `nil` uniformly means "anonymous."
- **Draft-visibility filtering: resolved as moved into the domain API**, per
  this doc's own steer ("the PRD's boundary language suggests it should
  move"). `content.Get`/`List`/`GetLocalized`/`ListLocalized` (the
  all-status, admin-facing reads) now require `content:read_drafts`
  themselves; `GetPublished`/`ListPublished`/`GetLocalizedPublished`/
  `ListLocalizedPublished` remain the separate, principal-free, published-only
  methods they already were (this predates the slice — slice 1.5 already
  split "admin view" vs "public view" into separate methods; this slice just
  added the capability gate to the admin-view half). `internal/api`'s
  `canReadDrafts` transport-level check is unchanged and still decides which
  pair of methods to call; it is now redundant with the domain check for
  every reachable HTTP request, which is exactly the intended
  defense-in-depth.

## Files/Modules Changed

- `internal/permission/permission.go` (+ `permission_test.go`) — added
  `Principal`, `RoleOf`, `AllowsPrincipal`, `ErrDenied`,
  `ContentTypesManage`. Tenant dimension **not** added (see Current
  Decisions — deferred).
- `internal/content/content.go` (+ `content_test.go`) — capability checks in
  `Create`/`Update`/`Delete`/`Rollback` (`content:write`),
  `Publish`/`Unpublish` (`content:publish`),
  `Get`/`List`/`GetLocalized`/`ListLocalized` (`content:read_drafts`).
  `GetPublished`/`ListPublished`/`GetLocalizedPublished`/
  `ListLocalizedPublished`/`GetPing`/`ListVersions` unchanged
  (principal-free, public). New unexported `get`/`list` helpers let the
  published-read methods reuse the storage-layer fetch without going through
  the now-capability-gated `Get`/`List`.
- `internal/composition/store.go` (+ new `store_test.go`) — capability
  check in `Save` (`content-types:manage`, except the first write).
- `internal/media/media.go` (+ `media_test.go`) — capability checks in
  `Upload`/`Delete` (`media:write`). Reads unchanged (public).
- `internal/api/auth.go` — new `(*Server).principal(r)` helper.
- `internal/api/api.go` — every content domain-API call site passes
  `s.principal(r)`; `writeContentError` maps `permission.ErrDenied` to 403.
- `internal/api/media.go` — `Upload`/`Delete` call sites pass
  `s.principal(r)`; `writeMediaError` maps `permission.ErrDenied` to 403.
- `internal/api/auth_test.go` — updated direct `comps.Save(...)` test-fixture
  calls to pass `nil` (bootstrap-style fixture writes).
- `internal/setup/setup.go` — `compositions.Save(ctx, nil, comp)` with an
  explanatory comment.
- **Not touched (does not exist in this worktree's branch):**
  `internal/graphql/*.go`, `pkg/client`, `sdk-js/testdata/seedcomposition/main.go`.
  See the Status caveat.

## Acceptance Criteria

- [x] Every mutating (and draft-reading) domain-API method rejects an
      under-privileged or anonymous caller itself, independent of whether
      the transport layer already checked — provable by a test that calls
      the domain API directly (bypassing `internal/api` entirely) with an
      under-privileged principal and asserts a permission error. Verified:
      `internal/content/content_test.go`'s
      `TestCreateRejectsUnderPrivilegedAndAnonymousPrincipal`,
      `TestUpdateRejectsUnderPrivilegedAndAnonymousPrincipal`,
      `TestDeleteRejectsUnderPrivilegedAndAnonymousPrincipal`,
      `TestPublishAndUnpublishRequireContentPublishNotJustContentWrite`,
      `TestRollbackRejectsUnderPrivilegedAndAnonymousPrincipal`,
      `TestGetAndListRejectAnonymousAndViewerLacksReadDrafts`;
      `internal/media/media_test.go`'s
      `TestUploadRejectsUnderPrivilegedAndAnonymousPrincipal`,
      `TestDeleteRejectsUnderPrivilegedAndAnonymousPrincipal`;
      `internal/composition/store_test.go`'s
      `TestSaveAllowsNilPrincipalOnFirstWriteOnly`,
      `TestSaveRejectsUnderPrivilegedPrincipalOnSubsequentWrites`. (Not
      re-provable for `internal/graphql` — doesn't exist in this branch.)
- [x] REST transport still behaves identically to before this change from an
      external HTTP-client's perspective (same status codes, same error
      shapes) — the full existing `internal/api` test suite
      (`auth_test.go`, `content_test.go`, `media_test.go`, `users_test.go`,
      `ratelimit_test.go`) passes unmodified in behavior; only the
      `comps.Save` fixture call sites needed a `nil` argument added, no
      assertions changed. (GraphQL not applicable — doesn't exist here.)
- [x] `go build ./...`, `go vet ./...`, and `go test -race ./...` all green
      across the whole repo as it exists in this worktree (`internal/api`,
      `internal/composition`, `internal/content`, `internal/db`,
      `internal/identity`, `internal/media`, `internal/permission`,
      `internal/server`, `pkg/contract` — 10 packages with tests, all pass;
      `internal/setup`, `internal/config`, `cmd/*` have no test files and
      build clean).
- [x] Real end-to-end verification against the compiled `glyphuxd` binary:
      built `./cmd/glyphuxd`, ran it against a fresh SQLite DB in a temp data
      dir, completed setup via a real POST to `/setup` (site_name,
      admin_email, admin_password, database=sqlite), then via `curl`:
      - Content write (`POST /api/v0/content/article`): 401 anonymous, 403
        viewer, 201 admin (created item returned).
      - Media write (`POST /api/v0/media`, multipart upload): 401 anonymous,
        403 viewer, 201 admin (created item returned).
      - Content-type management: no HTTP endpoint for this exists in this
        worktree's branch (composition mutation is `composition.Store.Save`,
        called only by the pre-auth setup wizard here), so this was verified
        by calling `composition.Store.Save` directly against the *running
        daemon's actual SQLite file* (same file, separate short-lived Go
        program, daemon left running throughout): an admin principal's Save
        succeeded (used to seed the `article` content type the content-write
        checks above needed), then a follow-up run with an editor principal
        and with a nil (anonymous) principal both got `permission.ErrDenied`
        — proving the boundary check holds against the real persisted state
        of the real running binary, not just an in-memory test fixture.
      - A viewer user was created via the real `POST /api/v0/users` admin
        endpoint and logged in via the real `POST /api/v0/auth/login` to
        obtain the session cookie used for the 403 checks above — the full
        chain (setup → user creation → login → capability-gated write) was
        exercised end-to-end, not stubbed.
- [x] Tracking doc moved to `docs/implementation/completed/` with this
      checklist filled in truthfully.

## Risks

- Highest-conflict-risk slice in this batch: touches nearly every domain
  package's public signatures. **Realized, not just anticipated:** this
  worktree's branch predates `internal/graphql`/`pkg/client`/`sdk-js`
  entirely, so those integrations still need the same treatment applied
  when this merges forward into current `dev` — see the Status caveat for
  exactly what pattern to replicate.
- Risk of over-engineering a tenant dimension that isn't needed yet — avoided
  by deferring it entirely rather than adding it partially; see Current
  Decisions.

## Merge-forward addendum (parent session, on `dev`)

This slice's worktree branch predated `internal/graphql`, `pkg/client`, and
`sdk-js` (2026-07-18) as well as several audit fixes already landed on
`dev` (composition's optimistic-concurrency CAS/version column,
`RemoveContentTypeGuarded`, and the GraphQL cookie-auth fix). Merging
required real conflict resolution, not just accepting one side:

- **`internal/permission`**: the merge worktree had independently invented
  `ContentTypesManage` with the string value `"content-types:manage"`
  (hyphen) — `dev` already had the same capability, same name, but value
  `"content_types:manage"` (underscore) from an earlier slice (0003) this
  worktree's branch predated. Kept `dev`'s value (already wired through
  REST/GraphQL/admin-UI/both SDKs); took the worktree's `Principal`,
  `RoleOf`, `AllowsPrincipal`, `ErrDenied` additions as-is. A duplicate
  `TestOnlyAdminHoldsContentTypesManage` test function (one from each side)
  was deduplicated.
- **`internal/composition/store.go`**: `dev`'s CAS/version-column fix and
  `RemoveContentTypeGuarded` didn't exist in the worktree; the worktree's
  capability check on `Save` didn't exist on `dev`. Merged both: `Save`
  keeps its exists-check + `AllowsPrincipal` gate (nil principal allowed
  only on the pre-existing first write) and still delegates to
  `SaveWith`/the CAS-based `DefineContentType`/`RemoveContentTypeGuarded`.
  **Additionally added principal checks to `DefineContentType` and
  `RemoveContentType`/`RemoveContentTypeGuarded` themselves** — the
  worktree's branch never knew these methods existed (they're the *actual*
  mutation path `internal/api/contenttypes.go`'s PUT/DELETE handlers and the
  GraphQL resolvers call; `Save` is only used by the pre-auth setup
  bootstrap), so without this addition the domain-boundary check this whole
  slice exists for would have been silently absent for content-type
  management specifically.
- **`internal/setup/setup.go`**: `dev` had refactored the setup-submission
  path into a `Committer`/`w.commit(ctx, in)` abstraction (atomic
  composition+admin-account write via `SaveWith` inside a transaction) that
  didn't exist in the worktree, which still inlined a direct
  `compositions.Save(ctx, nil, comp)` call. Kept `dev`'s `w.commit` call
  (it already uses the capability-check-free `SaveWith`, not `Save`, so no
  behavior change was needed there at all).
- **`internal/graphql`**: added `domainPrincipal(ctx) *permission.Principal`
  in `auth.go` (bridges the already-resolved `*identity.User` to the narrow
  `Principal` type, mirroring `internal/api/auth.go`'s `principal` method),
  and threaded it through every resolver call to `content`/`compositions`/
  `media` in `content.go` and `schema.resolvers.go`. Added
  `permission.ErrDenied` → `FORBIDDEN` mapping to `mapContentError`,
  `mapMediaError`, and `mapContentTypeError` in `errors.go` for
  defense-in-depth parity with REST.
- **`pkg/client`'s and `internal/graphql`'s test servers, `sdk-js`'s Go
  seed fixture**: updated every direct domain-API call (`comps.Save`,
  `content.Create/Publish/Update/Get`, `media.Upload`) to pass a principal
  (`nil` for bootstrap-style first-write fixtures, an admin
  `*permission.Principal` for calls needing an already-authenticated
  caller).
- **Found during merge, fixed**: `maxCASAttempts` (10) was too low under
  `-race` — `TestConcurrentDefineContentTypeDoesNotLoseUpdates`'s 20
  concurrent writers occasionally exhausted retries under `-race`'s slower
  scheduling. Raised to 50; re-ran the test 3x under `-race`, consistently
  green.

**Verification after merge-forward**: `go build ./...`, `go vet ./...`,
`go test -race ./...` all green across every package. `sdk-js`'s full
suite (30/30, real daemon subprocess, real seed fixture) and `admin-ui`'s
suite (12/12) + build (byte-identical `internal/adminui/dist` output) both
re-run and green. Hand-driven curl pass against the actually compiled
`glyphuxd` binary: full setup → admin login → content-type definition →
viewer creation → viewer login, then confirmed the capability matrix live
on both transports — REST `POST /api/v0/content/article` (401 anonymous /
403 viewer / 201 admin), REST `PUT /api/v0/content-types/{name}` (403
viewer), GraphQL `createContentItem` mutation (FORBIDDEN for viewer,
success for admin), and GraphQL `{ users { ... } }` authenticated via the
session cookie alone (no bearer header) still resolving correctly —
confirming the earlier cookie-auth fix survived the merge intact.
