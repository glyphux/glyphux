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

In-flight

## Current Decisions

- Domain APIs must accept a caller identity/role (or a pre-resolved
  `permission.Capability` set) and check it internally before performing any
  read/write/delete/publish operation — not trust the caller to have
  already checked.
- The natural shape: each domain API method takes an explicit
  `*identity.User` (or a minimal `Principal` interface exposing `Role`) as
  its first argument after context, and calls `permission.Allows(user.Role,
  capability)` itself, returning a typed permission-denied error
  (`permission.ErrDenied` or similar) that transports map to 403/FORBIDDEN.
  This mirrors how `content.API` already takes `ctx` first.
- `internal/api` and `internal/graphql` keep their existing
  `requireCapability`/`canReadDrafts` helpers for the fast-fail 401/403 at
  the transport edge (good UX: reject before even calling the domain layer)
  — but the domain layer must ALSO check, defense-in-depth, per §10.2
  "Three Points of Consent and Enforcement". Removing the transport-level
  checks entirely is explicitly NOT the fix; both layers check.
- Anonymous/public reads (content:read for published items, content-types
  list, media file serving) still need a "no principal" path through the
  domain API — model this as `nil` principal or a documented
  zero-value/anonymous role that `permission.Allows` already handles
  correctly for the public capability set. Check `internal/permission`'s
  existing role table before inventing new roles.
- This changes exported method signatures on `content.API`, `media.API`,
  and `composition.Store` (at least `DefineContentType`/`RemoveContentType`,
  arguably every mutating method). Every call site in `internal/api`,
  `internal/graphql`, `pkg/client`'s test server, `sdk-js`'s test harness's
  Go fixture, and any existing `_test.go` files must be updated in the same
  change — this is the reason this slice must land before the
  media/identity/user-management slices that also touch these files.
- `internal/permission`'s `Allows(role, capability)` currently ignores the
  tenant dimension the PRD's §11.5 no-op-tenant-seam calls for structurally
  (the audit flagged this too). If cheap to add without redesigning
  anything (e.g. `Allows(role Role, capability Capability, tenant TenantID)`
  where `TenantID` is presently always the zero value), do it here since
  it's the same "capability check signature" surface being touched anyway;
  if it turns out non-trivial, split it into its own follow-up note instead
  of blocking this slice.

## Open Questions

- Exact shape of the "principal" type passed into domain APIs — reuse
  `*identity.User` directly (simplest, but couples domain APIs to the
  identity package) vs. a narrower `permission.Principal{Role
  permission.Role}` interface (cleaner boundary, more plumbing). Prefer
  whichever keeps `internal/content` from importing `internal/identity`
  (layering: content shouldn't need to know identity's full shape) —
  investigate and record the decision here once made.
- Whether draft-visibility (`ContentReadDrafts`) filtering (currently done
  in the transport layer, filtering published-only vs all statuses) moves
  into the domain API's read methods too, or stays a transport-side
  post-filter. The PRD's boundary language suggests it should move, for
  consistency with everything else in this slice.

## Files/Modules Expected

- `internal/permission/permission.go` (+ test) — principal/capability shape,
  possibly tenant param
- `internal/content/content.go`, `content_test.go` — capability checks in
  Create/Update/Delete/Publish/Unpublish/Rollback/list-drafts-vs-published
- `internal/composition/store.go`, `store_test.go` — capability checks in
  DefineContentType/RemoveContentType (content-types:manage)
- `internal/media/media.go` (+ test) — capability checks in
  Upload/Delete (media:write)
- `internal/api/*.go` — update call sites to pass the resolved principal
  through; keep the existing transport-level `requireCapability` fast path
- `internal/graphql/*.go` — same
- `pkg/client`, `sdk-js` test harnesses — update any direct domain-API
  construction in test servers if signatures change
- New/updated tracking doc moved to `docs/implementation/completed/` when
  done

## Acceptance Criteria

- [ ] Every mutating (and draft-reading) domain-API method rejects an
      under-privileged or anonymous caller itself, independent of whether
      the transport layer already checked — provable by a test that calls
      the domain API directly (bypassing `internal/api`/`internal/graphql`
      entirely) with an under-privileged principal and asserts a permission
      error.
- [ ] REST and GraphQL transports still behave identically to before this
      change from an external HTTP-client's perspective (same status codes,
      same error shapes) — full existing test suites pass unmodified in
      behavior (signatures may change, external behavior must not).
- [ ] `go build/vet/test ./...` (+ `-race` on touched packages) green.
- [ ] Real end-to-end verification against the compiled `glyphuxd` binary:
      confirm a few representative endpoints still enforce correctly
      post-setup (401 anonymous, 403 under-privileged, 200 admin) for at
      least content write, content-type manage, and media write.
- [ ] Tracking doc moved to `docs/implementation/completed/` with this
      checklist filled in truthfully — do not mark items done that weren't
      independently verified.

## Risks

- Highest-conflict-risk slice in this batch: touches nearly every domain
  package's public signatures. Must land and merge to `dev` before slices
  0007 (media), 0008 (identity MFA/OAuth), 0009 (users management) start,
  or those will hit constant merge conflicts on the same files.
- Risk of over-engineering a tenant dimension that isn't needed yet (§18.3:
  "share math, not behavior, and only after duplication proves it") — keep
  the tenant param, if added, a true no-op zero-value with no behavior
  change, not a speculative multi-tenancy feature.
