# Implementation: Phase 4 slice 4.4a — layout/block HTTP transport

## Goal

PRD §14 slice 4.4a, per `docs/specs/phase3-4-spec.md`'s Ticket P4.4a: the
HTTP transport for the Layer-2 block registry and Layout documents the
visual builder (slice 4.4b, dispatched after this merges) will consume via
the JS SDK. Mirrors the existing content/media transport pattern: `GET
/api/v0/blocks` (list registered block definitions), `GET`/`PUT
/api/v0/layouts/{route}` (load/save a Layout document for a route,
validated both structurally and against the real registry before
persisting), a real DB-backed store, and typed `sdk-js` client methods for
all three endpoints.

## Owning Contexts

No `CONTEXT.md` exists yet (see `docs/agents/domain.md`). New:
`internal/layout` (DB-backed Layout store), `internal/api/layouts.go`
(HTTP handlers). Modified: `internal/permission` (new `LayoutsManage`
capability), `internal/api/api.go` (`Server.layouts`/`Server.blocks` fields,
`WithLayouts` option, three new routes), `pkg/blocks/registry.go`
(`Definition` gained JSON tags — the first time it's serialized over HTTP),
`cmd/glyphuxd/main.go` (first-party blocks now actually wired into the
running daemon), `sdk-js/src/{blocks,layouts,types,client,index}.ts`.

## Status

Complete. `go build ./... && go vet ./... && go test -race ./...` green
across the whole repo (36 packages). `cd sdk-js && npm test` green (7 test
files, 49 tests, including 8 new: 2 for blocks, 6 for layouts). Smoke-tested
against the real compiled `glyphuxd` binary end-to-end (setup wizard, login,
`GET /api/v0/blocks`, `PUT`/`GET /api/v0/layouts/home`) per this project's
"always smoke-test the real binary" convention, not just `go test`.

## Current Decisions

- **New capability `permission.LayoutsManage`, not a reuse of
  `ContentTypesManage` or `ContentWrite`.** A Layout is neither Layer-1
  schema (content types) nor a Layer-1 content item — it's Layer 2's own
  structural document ("additive to Layer 1, never polluting it", PRD §14).
  Reusing `ContentTypesManage` would conflate two independently-versioned
  contract layers' write permissions into one capability name whose
  existing call sites (content-type schema edits) mean something
  structurally different. Granted to `RoleAdmin` only, for the same
  "structural, site-wide change" reasoning `ContentTypesManage` is
  admin-only; editors get no layout-write capability in this slice (they
  can write/publish content, not rearrange templates).
- **`internal/layout.Store.Save` takes `*blocks.Registry` as an explicit
  parameter, not a field captured at `NewStore` time.** The registry is the
  shared, running daemon's live, mutable state — the exact same
  `*blocks.Registry` `internal/api.Server` holds and
  `blocks/firstparty.RegisterAll` populates at daemon construction — so
  passing it per-call keeps the store from owning or caching a copy of
  runtime state it doesn't actually own, mirroring how the store already
  takes `principal` per-call rather than binding one at construction.
- **Both structural (`contract.Layout.Validate()`) and registry-existence
  (`blocks.ValidateLayout()`) validation live inside `Store.Save`, not just
  in the API handler.** Matches `internal/composition.Store.SaveWith`'s own
  "the store never holds a contract-invalid document" invariant — a future
  caller of `Store.Save` that isn't the API handler (a CLI import tool, a
  future GraphQL mutation) gets the same guarantee for free instead of
  needing to remember to re-validate.
- **Route format: one or more non-empty, lowercase, `/`-delimited segments
  matching `[a-z0-9_-]+`** (`"home"`, `"blog/index"` valid; `""`,
  `"/home"`, `"home/"`, `"blog//index"`, `"Has-Upper"` rejected as
  `contract.ValidationErrors`). This is deliberately more permissive than
  `pkg/contract`'s existing region/slot-name `validIdent` (which forbids
  hyphens) since a route is a URL path segment, where hyphens are
  idiomatic, not a Go-identifier-shaped document key.
- **`GET /api/v0/layouts/{route...}` uses Go 1.22+'s trailing wildcard
  path pattern** (not `{route}`, which only matches one segment) so a
  multi-segment route like `"blog/index"` round-trips through the URL
  path itself, matching the spec's own example route names. The `sdk-js`
  client mirrors this by percent-encoding each `/`-delimited segment of a
  route independently (`encodePathSegments` in `sdk-js/src/layouts.ts`)
  rather than encoding the whole route (which would turn the slash into
  `%2F` and make Go's route matcher see one opaque segment instead of two).
- **`GET /api/v0/blocks` and `GET /api/v0/layouts/{route}` are public
  reads, no capability gate** — mirrors `GET /api/v0/content-types`'
  existing precedent (schema-shaped reads have no explicit capability check
  in this codebase; only writes are gated). `PUT /api/v0/layouts/{route}`
  requires `layouts:manage` and CSRF protection, matching every other
  mutating route's convention.
- **Block/layout support is wired into `internal/api.Server` via a new
  `WithLayouts(store, registry)` Option, not new required parameters on
  `api.New`.** Eleven existing call sites across `internal/api`'s own test
  suite, `pkg/client`, `internal/server`, `internal/bootstrap`, and
  `cmd/glyphuxd` construct an `api.Server` today; adding required
  parameters would force every one of them (mostly testing unrelated
  routes) to learn about this slice's stores just to keep compiling.
  Mirrors `WithOAuth`'s exact precedent: omitting the option leaves the
  three new routes 404ing rather than panicking or serving broken behavor.
  `cmd/glyphuxd/main.go` is the one call site that actually configures it
  for the real running daemon.
- **`blocks/firstparty.RegisterAll` is now actually wired into
  `cmd/glyphuxd`'s daemon startup** — this is the first slice to do so.
  Slices 4.1/4.2's tracking doc (0026) explicitly flagged this as
  deliberately deferred ("No daemon bootstrap wiring... applies equally to
  every first-party capability, not a gap unique to this slice"); this
  ticket's own scope text ("from the shared, running daemon's
  `pkg/blocks.Registry`") requires it to actually be wired for
  `GET /api/v0/blocks` to return anything real, so it's done here, for
  blocks specifically — no other first-party capability's wiring gap is
  addressed by this change.
- **`pkg/blocks.Definition` gained JSON tags** (`name`, `display_name`,
  `props`, `slots`, snake_case matching every other wire-facing
  `pkg/contract` type) since this slice is the first time it's ever
  serialized over HTTP; the previous tag-less struct relied on Go's default
  field-name-as-key encoding, which nothing before this slice depended on.

## Open Questions — resolved

- **Should layout writes support optimistic concurrency (CAS), like
  `internal/composition.Store` does for the single composition row?**
  Deferred. Each route is its own independent row (unlike composition's
  single shared row), so two editors racing on the *same* route is a much
  narrower window than composition's every-write-touches-one-row problem,
  and the spec didn't ask for it. A future slice can add a version column
  if concurrent layout editing turns out to need it — flagged as a Risk,
  not silently skipped.
- **Should `GET /api/v0/layouts/{route}` require any capability, given it's
  a "draft" layout document that might contain unpublished template
  changes?** Resolved as no capability gate, matching content-types list
  and content's own published-vs-draft precedent: this slice has no notion
  of a draft/published split for layouts at all (Layer-2 versioning was
  explicitly deferred by slice 4.1/4.2's tracking doc), so there's nothing
  yet to gate a "draft" view behind. If layout drafts/publishing are added
  in a future slice, this read gate should be revisited alongside it.

## Files/Modules Changed

- `internal/permission/permission.go` — new `LayoutsManage` capability,
  granted to `RoleAdmin` only.
- `internal/permission/permission_test.go` — `TestOnlyAdminHoldsLayoutsManage`,
  `LayoutsManage` added to `TestAdminHoldsEveryCapability`.
- `pkg/blocks/registry.go` — `Definition` gained JSON tags.
- `internal/layout/store.go` (new) — `ErrNotFound`, `Migrations` (version
  16), `Store`, `NewStore`, `Load`, `Save` (route-format + structural +
  registry validation, then persist).
- `internal/layout/store_test.go` (new) — 9 tests.
- `internal/api/api.go` — `Server.layouts`/`Server.blocks` fields,
  `WithLayouts` option, three new routes (`GET /api/v0/blocks`,
  `GET`/`PUT /api/v0/layouts/{route...}`).
- `internal/api/layouts.go` (new) — `handleBlocksList`, `handleLayoutGet`,
  `handleLayoutPut`, `writeLayoutError`.
- `internal/api/layouts_test.go` (new) — 11 tests (`testServerWithLayouts`
  helper + blocks-list and layout GET/PUT coverage, including 404-when-
  unconfigured, multi-segment routes, validation-failure mapping, and the
  admin-only/public-read split).
- `cmd/glyphuxd/main.go` — `layout.Migrations` added to the daemon's
  migration set; `blocks.New()` + `firstparty.RegisterAll` + `layout.NewStore`
  wired into `buildFullHandler` via `api.WithLayouts`.
- `sdk-js/src/types.ts` — `BlockDefinition`, `LayoutBlock`, `LayoutRegion`,
  `Layout`.
- `sdk-js/src/blocks.ts` (new) — `BlocksResource.list()`.
- `sdk-js/src/layouts.ts` (new) — `LayoutsResource.get()`/`.save()`,
  `encodePathSegments` helper for multi-segment routes.
- `sdk-js/src/client.ts` — `GlyphuxClient.blocks`/`.layouts` wired in.
- `sdk-js/src/index.ts` — new types exported.
- `sdk-js/test/blocks.test.ts` (new) — 2 tests.
- `sdk-js/test/layouts.test.ts` (new) — 6 tests.

## Acceptance Criteria

- [x] `GET /api/v0/blocks` lists every registered block `Definition` from
      the shared, running daemon's `pkg/blocks.Registry`.
- [x] `GET /api/v0/layouts/{route}` loads a persisted Layout, 404 if none
      exists yet.
- [x] `PUT /api/v0/layouts/{route}` validates (structural
      `Layout.Validate()` + registry `blocks.ValidateLayout()`) before
      persisting, 422 with issues on either failure, mirroring
      content-types PUT's pattern.
- [x] A real DB-backed layout store exists (`internal/layout`), following
      `internal/composition`'s Load/Save shape, migration version 16
      (re-verified as the next unused number after audit's 15
      immediately before writing it).
- [x] `sdk-js` exposes typed client methods for all three endpoints, wired
      into `GlyphuxClient`.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] `cd sdk-js && npm test` green.
- [x] Smoke-tested against the real compiled `glyphuxd` binary (not just
      `go test`/`npm test`): setup wizard, login, `GET /api/v0/blocks`,
      `PUT`/`GET /api/v0/layouts/home` all exercised over real HTTP against
      a real subprocess.
- [x] TDD discipline followed: `internal/layout/store_test.go` and
      `internal/api/layouts_test.go` written before their implementations;
      `sdk-js` tests written against the real running-daemon harness
      (`global-setup.ts`), no mocks.

## Risks

- **No optimistic-concurrency/CAS on layout writes** — see Open Questions.
  Two editors saving the same route concurrently will silently last-write-
  wins, unlike composition's CAS-retry-loop protection.
- **No layout draft/publish/versioning** — a `PUT` immediately overwrites
  whatever was previously live for that route, with no history and no way
  to preview before publishing. Out of scope per slice 4.1/4.2's own
  resolved Open Question ("Layer-1 Composition's own draft/version handling
  is a kernel concern; `pkg/contract.Layout` itself is just the document
  shape"); the visual builder (slice 4.4b) or a later slice is the natural
  place to add this if the PRD calls for it.
- **No prop-value type-checking** — inherited directly from
  `blocks.ValidateLayout` (slice 4.1/4.2's own flagged risk): a `PUT` with
  a `heading` block missing its required `text` prop, or an image block's
  `src` pointing at a non-existent media item, is accepted as long as the
  block *type* is registered. The visual builder's prop-editing UI is the
  natural place this gets addressed, per slice 4.1/4.2's own note.
- **Route format is a new, first-use-in-this-slice convention** (lowercase
  `/`-delimited `[a-z0-9_-]+` segments) with no prior art elsewhere in the
  contract layer to check consistency against; a future slice introducing
  route-like identifiers elsewhere (e.g. a routing/redirects feature)
  should reuse this exact validation rather than inventing a third format.
