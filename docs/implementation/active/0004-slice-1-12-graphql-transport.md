# Implementation: Slice 1.12 — GraphQL transport

## Goal

Add a GraphQL surface over the same domain APIs the REST transport
(`internal/api`) already serves — an optional-within-phase transport, not a
replacement. Per PRD §16 Phase 1: "GraphQL surface over the same domain
APIs."

## Owning Contexts

- `docs/glyphux-prd.md` §16 Phase 1, slice 1.12

## Status

In-flight (dispatched to a background agent in an isolated git worktree;
see the agent's branch for the actual diff before merging to `dev`)

## Current Decisions

- **Library: gqlgen** (schema-first + codegen), confirmed with user — matches
  the typed/codegen ethos already used for the 1.11 SDKs.
- **Scope**: mirror the full REST domain surface — content CRUD +
  drafts/publish/versions/rollback, content-type management (slice added
  outside the original PRD list, see `0003-content-type-management-api.md`),
  media (queries + upload as a mutation), users. Same capability gates as
  REST (`internal/permission`), enforced in resolvers exactly as
  `requireCapability` does for REST — GraphQL is another client of the same
  domain APIs (`internal/content`, `internal/media`, `internal/identity`,
  `internal/composition`), never a new privileged path.
- **Auth**: no separate GraphQL login mutation. Callers authenticate via the
  existing REST `/api/v0/auth/login` once, then send the bearer token as
  `Authorization: Bearer <token>` on GraphQL requests too — the resolver
  context extracts the principal the same way `currentUser`/`requireUser` do
  in `internal/api/auth.go`. Keeps auth as one code path, not two.
- **Mount point**: `POST /graphql` (+ GraphiQL/playground UI at `GET
  /graphql` in non-production... actually keep this simple: just mount the
  handler, defer a playground UI decision to the implementing agent if
  gqlgen's default handler bundles one trivially).
- **Seam (TDD)**: HTTP-level tests hitting the real GraphQL endpoint on a
  real `api`-equivalent server (same pattern as `internal/api`'s existing
  `httptest`-based tests) — queries/mutations exercised over real HTTP, real
  sqlite-backed domain stores, no mocked resolvers.

## Open Questions

- None blocking; playground/introspection-in-production is a judgment call
  left to the implementing pass (default to enabled — a self-hosted daemon
  behind admin's own auth is a different risk profile than a public SaaS
  API; introspection just describes the schema, does not bypass capability
  checks on resolvers).

## Files/Modules Expected

- `internal/graphql/` (new): schema (`.graphql` files), generated resolvers
  (gqlgen codegen), resolver implementations wired to the existing domain
  APIs.
- `internal/api/api.go` or `internal/server/server.go`: mount `/graphql`.
- `go.mod`: gqlgen + its runtime dependency.

## Acceptance Criteria

- [ ] Content CRUD + publish/unpublish/versions/rollback reachable via GraphQL queries/mutations, same validation/error behavior as REST (typed GraphQL errors, not swallowed).
- [ ] Content-type management (list/define/delete) reachable via GraphQL.
- [ ] Media (list/get/upload/delete) reachable via GraphQL.
- [ ] Users (create/list) reachable via GraphQL, admin-only.
- [ ] Every mutation enforces the same capability as its REST counterpart — an editor cannot publish, a viewer cannot write, etc. — proven by a test per capability boundary, not just the happy path.
- [ ] `go build/vet/test` green; verified against the real compiled binary end-to-end (a real GraphQL query/mutation round-trip over real HTTP).

## Risks

- gqlgen codegen adds a build step (`go generate` or a `gqlgen.yml` +
  `go run github.com/99designs/gqlgen generate`) — must be documented so
  `go build ./...` alone doesn't silently serve a stale schema. The
  implementing agent should make sure generated code is checked in (not
  gitignored) so the repo builds without requiring codegen to run first,
  consistent with how this repo has no other codegen step today.
