# Implementation: Slice 1.12 — GraphQL transport

## Goal

Add a GraphQL surface over the same domain APIs the REST transport
(`internal/api`) already serves — an optional-within-phase transport, not a
replacement. Per PRD §16 Phase 1: "GraphQL surface over the same domain
APIs."

## Owning Contexts

- `docs/glyphux-prd.md` §16 Phase 1, slice 1.12

## Status

Completed

## Current Decisions

- **Library: gqlgen v0.17.94** (schema-first + codegen), confirmed with
  user — matches the typed/codegen ethos already used for the 1.11 SDKs.
  Generated code (`internal/graphql/generated/`) is checked into git, not
  gitignored, so `go build ./...` works with no codegen step required
  first. Regenerate with `cd internal/graphql && go run
  github.com/99designs/gqlgen generate` after editing
  `schema/schema.graphql`.
- **Scope**: mirrors the full REST domain surface — content CRUD +
  drafts/publish/versions/rollback, content-type management, media
  (queries + delete; **upload intentionally excluded**, see Risks/Gaps),
  users (list + create). Same capability gates as REST
  (`internal/permission`), enforced per-resolver via `requireCapability` —
  GraphQL is another client of the same domain APIs (`internal/content`,
  `internal/media`, `internal/identity`, `internal/composition`), never a
  new privileged path.
- **Auth**: no separate GraphQL login mutation. Callers authenticate via
  the existing REST `/api/v0/auth/login` once, then send the bearer token
  as `Authorization: Bearer <token>` on GraphQL requests too.
  `internal/graphql/auth.go` resolves the token to a principal in HTTP
  middleware (`Resolver.withPrincipal`, wrapping gqlgen's handler) and
  stashes it in the request context; each resolver calls
  `requireCapability(ctx, capability)` to enforce the gate — an independent
  reimplementation of `currentUser`/`requireCapability`
  (`internal/api/auth.go`), since those are unexported to package `api` and
  `internal/api` must not be modified. Cookie-based auth (the browser path)
  is deliberately not supported over GraphQL — only bearer tokens, per the
  brief.
- **Error shape**: GraphQL responses are always HTTP 200, so REST's
  404/422/409/401/403 distinctions are carried as a `code` extension on
  each GraphQL error instead (`NOT_FOUND`, `VALIDATION` — with `issues`
  when applicable, `CONFLICT`, `UNAUTHENTICATED`, `FORBIDDEN`,
  `SETUP_REQUIRED`, `INTERNAL`). See `internal/graphql/errors.go`, which
  mirrors `writeContentError`/`writeMediaError`/`writeContentTypeError` in
  `internal/api/api.go` exactly, one condition at a time.
- **Mount point**: `POST /graphql` only (no playground/GraphiQL UI —
  out of scope per the brief). `internal/server/server.go`'s `Handler`
  gained an **optional variadic** `...http.Handler` parameter for the
  GraphQL handler so every existing caller (including
  `internal/bootstrap`'s tests, which this task must not modify) keeps
  compiling with zero changes. `cmd/glyphuxd/main.go`'s `buildFullHandler`
  builds `graphql.NewResolver` from the same domain API instances already
  wired for the REST `apiServer` and passes `graphql.NewHandler(resolver)`
  through. Introspection is left enabled (gqlgen's default) — a
  self-hosted daemon behind its own auth is a different risk profile than
  a public SaaS API, and introspection describes the schema without
  bypassing any resolver's capability check.
- **Seam (TDD)**: HTTP-level tests hitting the real GraphQL endpoint on a
  real sqlite-backed test server (`internal/graphql/graphql_test.go`,
  mirroring `testServerWithAuth` in `internal/api/auth_test.go`) — no
  mocked resolvers, no mocked domain layer. 16 tests, one per
  query/mutation plus dedicated capability-boundary tests (editor
  publish/unpublish rejected, viewer write rejected, non-admin content-type
  and user management rejected, anonymous rejected).

## Open Questions

None blocking.

## Files/Modules Expected

- `internal/graphql/schema/schema.graphql`: the schema, grown incrementally
  across TDD cycles (queries first, then mutations, in the order:
  contentTypes → contentItem → contentItems → contentVersions →
  createContentItem → update/delete/publish/unpublish/rollback →
  defineContentType/removeContentType → media queries/delete →
  users/createUser).
- `internal/graphql/gqlgen.yml`: codegen config (exec + models under
  `generated/`, hand-written resolvers via `follow-schema` layout).
- `internal/graphql/generated/`: gqlgen-generated exec schema + models
  (checked in).
- `internal/graphql/resolver.go`: `Resolver` struct + `NewResolver`,
  wiring the same domain API instances REST uses.
- `internal/graphql/schema.resolvers.go`: resolver bodies (gqlgen
  preserves hand-written implementations across regeneration).
- `internal/graphql/auth.go`: bearer-token principal resolution +
  `requireCapability`/`canReadDrafts` helpers.
- `internal/graphql/errors.go`: domain-error → GraphQL-error mapping with
  `code` extensions.
- `internal/graphql/content.go`: shared draft/locale dispatch helpers
  (`getContentItem`/`listContentItems`), mirroring
  `handleContentGet`/`handleContentList`'s branching in `internal/api/api.go`.
- `internal/graphql/models.go`: domain type → generated GraphQL model
  conversions.
- `internal/graphql/handler.go`: `NewHandler`, wiring gqlgen's
  `handler.NewDefaultServer` with the auth middleware.
- `internal/graphql/graphql_test.go`: the full test suite (16 tests).
- `internal/server/server.go`: `Handler` gains the optional variadic
  GraphQL handler parameter.
- `cmd/glyphuxd/main.go`: `buildFullHandler` builds and mounts the
  GraphQL handler.
- `go.mod`/`go.sum`: `github.com/99designs/gqlgen` + its runtime deps.

## Acceptance Criteria

- [x] Content CRUD + publish/unpublish/versions/rollback reachable via GraphQL queries/mutations, same validation/error behavior as REST (typed GraphQL errors via `code` extensions, not swallowed).
- [x] Content-type management (list/define/delete) reachable via GraphQL, including the 409-equivalent (`CONFLICT`) delete guard when items of that type still exist.
- [x] Media (list/get/delete) reachable via GraphQL. Upload is **not** — see Risks/Gaps below; this is an intentional, brief-approved deviation from the original planning note's "media (queries + upload as a mutation)" line.
- [x] Users (create/list) reachable via GraphQL, admin-only.
- [x] Every mutation enforces the same capability as its REST counterpart — proven by dedicated tests: `TestCreateContentItemMutationRequiresContentWrite` (viewer rejected), `TestPublishContentItemMutationRequiresContentPublish` (editor rejected — editor holds `content:write` but not `content:publish`), `TestUpdateContentItemMutation` (viewer rejected), `TestDefineContentTypeMutationRequiresAdmin` / `TestRemoveContentTypeMutationBlockedWhenItemsExist` (editor rejected, admin-only), `TestMediaQueriesArePublicAndDeleteRequiresMediaWrite` (viewer rejected), `TestUsersQueryAndCreateUserMutationAreAdminOnly` (editor and anonymous both rejected).
- [x] `go build/vet/test` green for the whole repo (including `-race` on `internal/graphql`, `internal/server`, `cmd/glyphuxd`); verified against the real compiled binary end-to-end: booted `glyphuxd` as a subprocess, completed `POST /setup` over real HTTP, then drove real `defineContentType`, `createContentItem`, `publishContentItem`, `createUser` mutations and `contentTypes`/`contentItem`/`users` queries via `curl` against the running daemon, including a draft-visibility check (anonymous 404-equivalent before publish, visible after) and a capability-denial check (viewer rejected with `FORBIDDEN` on both a content mutation and the admin-only `users` query). Confirmed REST (`GET /api/v0/content-types`, `GET /api/v0/content/post`) reads back the same data GraphQL wrote, proving both are transports over one domain layer.

## Risks / Gaps

- **Media upload is not exposed over GraphQL** (deviation from the original
  planning note, explicitly approved in this task's brief). Upload is
  inherently multipart/binary; GraphQL mutations don't handle file uploads
  cleanly without extra tooling (e.g. the graphql-multipart-request-spec,
  which gqlgen supports but which pulls in additional client-side
  complexity for no clear win here). REST's `POST /api/v0/media` already
  covers upload and remains the sanctioned path for it; JS/Go clients can
  mix REST (upload) and GraphQL (everything else) freely since both sit
  over the same domain APIs and the same bearer token works on both.
- gqlgen's `go run github.com/99designs/gqlgen generate` needs
  `github.com/urfave/cli/v3` and `github.com/goccy/go-yaml` resolvable via
  `go get` before each regeneration in a from-scratch checkout — they are
  gqlgen's own tool dependencies, not used by any package in this module,
  so `go build`/`go test` don't need them and they don't stick in `go.mod`
  as persistent requires. This is a minor codegen-workflow wrinkle, not a
  build-time risk (the checked-in `generated/` package needs no such
  dependency to compile).
- The delete-guard read-then-write in `removeContentType`
  (`CountItems` then `RemoveContentType`) has the same non-transactional
  race already accepted for the REST equivalent (see
  `0003-content-type-management-api.md`'s Risks) — not a new risk
  introduced by this slice.
