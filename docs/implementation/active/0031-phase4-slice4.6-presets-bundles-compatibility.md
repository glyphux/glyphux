# Implementation: Phase 4 slice 4.6 — composition presets & bundles + compatibility contract

## Goal

PRD §13 in full (§13.1–§13.4), per `docs/specs/phase3-4-spec.md`'s Ticket
P4.6: two new marketplace-distributable artifacts — **Composition Preset**
("a serialized fragment of Layer-2 composition": a saved arrangement of
blocks, at any scale from a section to a page) and **Composition Bundle**
(pages + presets + sample content + a theme reference — a starter site) —
built directly from `pkg/contract.Layout`/`Region`/`Block`, not a parallel
format. The hard part per §13.3: **the compatibility contract** — a preset's
manifest declares which blocks/slots it assumes; a theme declares which
regions it exposes; import-time validation must detect a referenced block
the current setup lacks and surface an explainable "this preset needs the
`pricing-table` block — install it?" condition instead of silently
rendering garbage.

## Owning Contexts

No `CONTEXT.md` exists yet (see `docs/agents/domain.md`). New: `pkg/compat`
(the compatibility contract engine), `internal/preset` (Composition Preset
store), `internal/bundle` (Composition Bundle store), `internal/api/
presets.go`, `internal/api/bundles.go`. Modified: `pkg/contract` (new
`CompositionPreset`/`CompositionBundle`/`Manifest`/`SampleContentItem`
types), `pkg/theme` (`Theme` interface gained `Regions() []string`),
`themes/headless`, `themes/starter` (implement `Regions()`),
`internal/permission` (new `PresetsManage` capability), `internal/api/api.go`
(`Server.presets`/`.bundles` fields, `WithPresets` option, ten new routes),
`cmd/glyphuxd/main.go` (preset/bundle stores wired into the running daemon),
`sdk-js/src/{presets,bundles,types,client,index}.ts`,
`admin-ui/src/pages/builder/BuilderPage.tsx` ("Save as preset").

## Status

Complete. `go build ./... && go vet ./... && go test -race ./...` green
repo-wide (every package, including the four new ones).
`cd admin-ui && npm run build && npm test` green (97 tests, +2 new).
`cd sdk-js && npm test` green (61 tests, +12 new: 8 presets, 4 bundles).
Smoke-tested against the real compiled `glyphuxd` binary end-to-end: setup
wizard, login, save a preset, `GET .../check` with both a satisfied and an
unsatisfied theme-region declaration, import a preset into a route and
confirm the merged Layout, save+import a bundle (page merge + sample content
item creation via a real `content-types` PUT and `content` list), not just
`go test`/`npm test`.

## Current Decisions

- **A Composition Preset is built directly from `pkg/contract.Layout`/
  `Region`/`Block`, wrapped with a `Manifest`** (`pkg/contract/preset.go`):
  `CompositionPreset{ContractVersion, Name, Description, Layout, Manifest}`.
  PRD §13.2 is explicit that a preset is "a serialized fragment of Layer-2
  composition," so reusing the exact Layer-2 types (rather than inventing a
  parallel "template" schema) is the whole point — "import a template" is
  literally "merge this `Layout` fragment into a target `Layout`."
- **`Manifest` (shared by both Preset and Bundle) declares
  `requires_contract` (the Layer-2 contract version composed against),
  `blocks` (declared block-type dependencies), `slots` (declared theme
  *region* dependencies — explicitly NOT a `Block`'s own nested `Slots`,
  which are block-defined, not theme-defined; the doc comment calls this out
  since the two concepts share the word "slot"), and `themes` (optional
  named-theme restriction).** One shared type rather than two near-identical
  ones because Bundle's own compatibility requirements are structurally
  identical to a Preset's, just aggregated across more pages.
- **`Theme` gained a new `Regions() []string` method — confirmed by reading
  the code, not assumed, that no such declaration existed anywhere before
  this slice.** Both `themes/headless` and `themes/starter` render whatever
  region names a `Layout` happens to contain (neither had a fixed set in
  code); this slice makes the *declaration* real without changing rendering
  behavior: `headless.Regions()` returns `nil` (true to its "pure JSON
  passthrough, no opinion about regions" design — `pkg/compat` treats `nil`
  as "no declared restriction," not "zero regions allowed"); `starter.
  Regions()` returns its four example region names (`header`, `main`,
  `sidebar`, `footer`, matching its own pre-existing doc comment) as a real,
  checkable declaration, while `Render` itself stays generically permissive
  for forward-compatibility. This is a real interface-breaking change (every
  `theme.Theme` implementer must add the method); the only two
  implementations in-tree were updated, and no other implementer exists
  (grepped for `theme.Theme` conformers before concluding this).
- **`pkg/compat` (new package) implements the compatibility contract as
  `CheckPreset(preset, registry, themeRegions) Result` /
  `CheckBundle(bundle, registry, themeRegions) Result` — deliberately
  decoupled from `pkg/theme`.** `pkg/compat` takes a destination theme's
  already-called `Regions()` as a plain `[]string`, not a `theme.Theme`,
  because `pkg/theme` imports `internal/content` (a kernel-internal
  package); depending on it from `pkg/compat` would make this otherwise-
  public, plugin-importable package (like `pkg/blocks`/`pkg/contract`)
  transitively reach into kernel internals — exactly the boundary PRD §17.2
  prohibits for public contract packages. Callers (here, `internal/preset`/
  `internal/bundle` via `internal/api`) call a theme's `Regions()` and pass
  the result in.
- **`Result` is a struct (`Compatible bool`, `MissingBlocks []string`,
  `MissingSlots []string`, `UnsupportedContract string`), never a bare
  boolean** — the PRD's explicit ask: "this preset needs the `pricing-table`
  block — install it?" requires knowing exactly what's missing, not just
  pass/fail.
- **Both the manifest's *declared* dependencies and the composition tree's
  *actual* referenced block types/regions are checked, unioned together.**
  Catches two independent failure modes: an author's manifest that
  under-declares (forgot to list a block the tree actually uses — walked via
  `contract.WalkBlocks`, the one shared tree-walk `pkg/contract` already
  exports) and a manifest that's simply wrong. `TestCheckPresetMissingBlockTypeReferencedButNotDeclared`
  in `pkg/compat/compat_test.go` covers this directly: an empty
  `Manifest.Blocks` still surfaces a tree-referenced missing block.
- **`Store.Save` (both `internal/preset` and `internal/bundle`) requires
  full block compatibility (`compat.Check*` against the live registry, no
  theme-region gate) before persisting; `Store.Import` tolerates
  incompatibility and reports it instead of erroring.** A preset/bundle
  saved through this host's own "save my current composition" flow is
  authored locally, from blocks the author actually has — there's no
  legitimate reason for it to reference something unregistered, so Save
  rejects it (`contract.ValidationErrors`, mirroring `internal/layout.Store.
  Save`'s identical "the store never holds a contract-invalid document"
  invariant). Import is the opposite case by design: a preset/bundle
  authored *elsewhere* (marketplace-distributed, per §13.4, dispatched next
  as P4.7) may well reference a block this host doesn't have — Import must
  not throw, must not partially merge, and must return the full `Result` so
  a caller can render the "install it?" prompt. Verified with
  `TestImportDeclinesToMergeWhenIncompatibleButReportsWhy`/
  `TestImportDeclinesToMergeOrCreateWhenThemeLacksDeclaredSlot`-shaped tests
  in both `internal/preset/store_test.go` and `internal/bundle/store_test.go`:
  an incompatible Import leaves the destination Layout completely untouched
  (`layout.ErrNotFound` still returned for a route that was never merged
  into).
- **`internal/preset.Record`/`internal/bundle.Record` wrap the portable
  `contract.CompositionPreset`/`CompositionBundle` with a store-assigned
  `ID`/`CreatedAt`/`UpdatedAt`, mirroring the split `internal/content.Item`
  already draws between a content item's own `Data` and its storage
  identity.** `contract.CompositionPreset`/`CompositionBundle` themselves
  carry no ID field — they are the exact document a marketplace package (or
  a hand-authored file) distributes, with no notion of "which row in which
  host's DB." `Record` is what *this host's* store hands back once one has
  been saved here; deliberately kept out of `pkg/contract` for the same
  reason `internal/content.Item` isn't itself `pkg/contract.ContentType`.
- **`internal/preset.Store.Import` merges a preset's `Layout.Regions` into a
  target route's `Layout` region-by-region (same-named region replaced
  wholesale, other regions untouched)** — verified directly with
  `TestImportMergesIntoExistingLayoutWithoutClobberingOtherRegions`. No
  finer-grained (block-level, "insert at position N") merge is attempted:
  `pkg/contract.Layout` has no ordering/anchor concept to merge against, and
  the PRD doesn't ask for one; a region-level replace is the coarsest
  reasonable unit and mirrors how re-`PUT`ting a whole `Layout` already
  behaves for any other region conflict.
- **`internal/bundle.Store.Import`'s sample-content creation is deliberately
  best-effort, not atomic with page-Layout merging** — see
  `ImportResult.ContentErrors`'s doc comment. By the time sample content
  creation runs, every page's `Layout` has already been merged and is
  already real, valid structure; aborting the whole import over one bad
  sample item (e.g. an unregistered content type — a content-model concern,
  outside `pkg/compat`'s block/slot vocabulary entirely) would throw away
  that already-good work for an unrelated problem. Covered by
  `TestImportReportsSampleContentErrorButStillMergesPages`.
- **New capability `permission.PresetsManage`, shared by both Presets and
  Bundles (not split into two capabilities), admin-only** — mirrors
  `permission.LayoutsManage`'s exact "structural, site-wide change" reasoning
  (importing rewrites an arbitrary route's `Layout`); one capability, not two,
  because in this v1 role matrix they always have the identical holder and
  no call site checks one without the other — splitting would add a second
  capability name with zero behavioral difference.
- **No live "selected theme" concept exists anywhere in the running daemon
  today** (grepped for every `pkg/theme` import outside the theme packages
  themselves and their own tests — none exists; themes are compiled-in Go
  packages, never wired into `cmd/glyphuxd`'s actual serving path yet). This
  is a real, pre-existing gap this ticket does not attempt to close (that's
  a rendering-pipeline concern for a future slice, likely alongside P4.5's
  live preview). Given that gap, `theme_regions` is an explicit request-level
  parameter — a comma-separated `?theme_regions=` query param for the
  read-only `GET .../check` endpoints, a `theme_regions` JSON body field for
  `POST .../import` — supplied by the caller (today, an admin-ui client
  that already knows which theme it's targeting), rather than the server
  resolving it from state that doesn't exist yet. `themeRegionsParam` in
  `internal/api/presets.go` is the one place this parsing lives. Flagged as
  a Risk below, not silently worked around.
- **Ten new routes**, all 404ing unless `api.WithPresets(presetStore,
  bundleStore)` is configured (mirrors `WithLayouts`'s identical "opt-in,
  404 unless configured" precedent; `cmd/glyphuxd/main.go` is the one real
  call site):
  - `GET /api/v0/presets`, `GET /api/v0/presets/{id}` — public reads.
  - `POST /api/v0/presets` — validate + persist; `presets:manage` + CSRF.
  - `GET /api/v0/presets/{id}/check?theme_regions=a,b` — public,
    non-mutating compatibility check.
  - `POST /api/v0/presets/{id}/import` (body `{route, theme_regions}`) —
    `presets:manage` + CSRF; always 200 with the `compat.Result` (even when
    incompatible — a caller distinguishes "declined" from "server error" by
    reading `.compatible`, not the HTTP status, per §13.3's explainability
    requirement).
  - The equivalent five `/api/v0/bundles...` routes, with `POST .../import`
    taking no `route` (a bundle declares its own page routes) and returning
    `internal/bundle.ImportResult` (the `compat.Result` plus which pages
    merged and which sample content was created).
- **Admin-ui scope boundary, decided explicitly (per this ticket's own
  instruction to make and document this call):** a minimal but real
  integration — "Save as preset" wired into the existing `BuilderPage`
  (`SavePresetBar`: a name field + button, builds a `CompositionPreset` from
  the current draft via a new `manifestFor` helper that walks the draft's
  own tree for `blocks`/`slots`, then calls `client.presets.save`) — is
  **in scope**; a full presets/bundles *management* UI (browse saved
  presets, import one into an arbitrary route from a list, browse/import
  bundles, a bundle-import theme-region picker) is **out of scope, deferred
  to a follow-up**. Reasoning: this ticket is already four new Go packages
  (`pkg/compat`, `internal/preset`, `internal/bundle`, plus contract/theme/
  permission/api changes) plus a full `sdk-js` surface; a whole new
  admin-ui page (list + import-picker + bundle browser) is a distinct,
  sizeable UI slice of its own, while leaving `sdk-js` consumers with
  *zero* UI entry point felt worse than a minimal, real one. "Save as
  preset" was chosen over "import a preset" as the one integration point
  because it fits naturally into the builder's existing single-route
  editing flow with no new page/route needed (import targets an arbitrary
  route and a theme-region choice — a picker UI, not a one-button
  add-on) — the natural next admin-ui slice, alongside a dedicated
  bundle-import flow, is a good candidate for whatever ticket does the
  admin-ui half of P4.7 (marketplace distribution) or a dedicated follow-up.

## Migration Version

`preset.Migrations` = 17, `bundle.Migrations` = 18 — re-verified live by
grepping `Version:` across `internal/*/*.go` immediately before writing each
migration (16 was the highest existing, from `internal/layout`, slice 4.4a);
no other Phase-4 ticket's migration had landed in `dev` as of this writing.

## Open Questions — resolved

- **Should `Save` (author-time) also gate on theme-region compatibility, not
  just block-registry compatibility?** Resolved as no. A preset saved
  through this host's own builder is, by construction, using whatever
  regions the builder's current draft targets — there is no "destination
  theme" distinct from "the one rendering this host" at save time the way
  there is at import time (a preset from *elsewhere*, being imported into
  *this* host, possibly a different theme). Gating Save on theme regions
  would need this ticket to also decide "which theme is this host's active
  one," which is exactly the not-yet-existing "selected theme" concept
  flagged above — out of scope here.
- **Should `CompositionBundle.Presets` (bundled standalone presets) be
  imported into the preset library as their own saved `Record`s during
  `bundle.Store.Import`, or left inert?** Resolved as left inert for this
  slice — `CheckBundle` does fold their block/slot requirements into the
  aggregate compatibility `Result` (so an included preset's missing
  dependency still blocks/reports correctly), but `Import` does not call
  `preset.Store.Save` for each one. Reasoning: doing so would require
  `bundle.Store` to hold a `*preset.Store` dependency (a new cross-package
  wiring this ticket didn't otherwise need) for a feature ("also save these
  as standalone reusable presets") the PRD's own bundle description doesn't
  explicitly require ("a preset collection at site scale," not "and also
  register each one individually"). Flagged as a Risk below, a reasonable
  follow-up if real usage wants it.

## Files/Modules Changed

- `pkg/contract/preset.go` (new) — `CompositionPresetV1`, `CompositionPreset`,
  `Manifest`, `Validate()`.
- `pkg/contract/bundle.go` (new) — `CompositionBundleV1`, `CompositionBundle`,
  `SampleContentItem`, `Validate()`, `asValidationErrors` helper.
- `pkg/compat/compat.go` (new) — `Result`, `CheckPreset`, `CheckBundle`.
- `pkg/compat/compat_test.go` (new) — 9 tests covering all-present, missing
  block (declared and tree-only), missing slot, no-restriction theme,
  unsupported contract version, and bundle-level aggregation.
- `pkg/theme/theme.go` — `Theme` interface gained `Regions() []string`.
- `themes/headless/headless.go` (+ test) — `Regions()` returns `nil`.
- `themes/starter/starter.go` (+ test) — `Regions()` returns
  `["header","main","sidebar","footer"]`.
- `internal/permission/permission.go` (+ test) — new `PresetsManage`
  capability, admin-only.
- `internal/preset/store.go` (new) — `ErrNotFound`, `Migrations` (17),
  `Record`, `Store`, `NewStore`, `Get`, `List`, `Save`, `Check`, `Import`.
- `internal/preset/store_test.go` (new) — 15 tests.
- `internal/bundle/store.go` (new) — `ErrNotFound`, `Migrations` (18),
  `Record`, `ContentRef`, `ImportResult`, `Store`, `NewStore`, `Get`, `List`,
  `Save`, `Check`, `Import`.
- `internal/bundle/store_test.go` (new) — 10 tests.
- `internal/api/api.go` — `Server.presets`/`.bundles` fields, `WithPresets`
  option, ten new routes.
- `internal/api/presets.go` (new) — `themeRegionsParam`,
  `handlePresetsList/Get/Create/Check/Import`, `writePresetError`.
- `internal/api/presets_test.go` (new) — 9 tests (`testServerWithPresets`
  helper).
- `internal/api/bundles.go` (new) — the equivalent five bundle handlers.
- `internal/api/bundles_test.go` (new) — 4 tests.
- `cmd/glyphuxd/main.go` — `preset.Migrations`/`bundle.Migrations` added to
  the daemon's migration set; `preset.NewStore`/`bundle.NewStore` wired in
  via `api.WithPresets`.
- `sdk-js/src/types.ts` — `Manifest`, `CompositionPreset`, `PresetRecord`,
  `SampleContentItem`, `CompositionBundle`, `BundleRecord`, `CompatResult`,
  `ContentRef`, `BundleImportResult`.
- `sdk-js/src/presets.ts` (new) — `PresetsResource` (`list`/`get`/`save`/
  `check`/`import`).
- `sdk-js/src/bundles.ts` (new) — `BundlesResource` (same shape).
- `sdk-js/src/client.ts` — `GlyphuxClient.presets`/`.bundles` wired in.
- `sdk-js/src/index.ts` — new types exported.
- `sdk-js/test/presets.test.ts` (new) — 8 tests.
- `sdk-js/test/bundles.test.ts` (new) — 4 tests.
- `admin-ui/src/pages/builder/BuilderPage.tsx` — `manifestFor`,
  `SavePresetBar`, wired in alongside the existing `SaveBar`.
- `admin-ui/src/pages/builder/BuilderPage.test.tsx` — 2 new tests, mock
  extended with `presets.save`.
- `internal/adminui/dist/*` — rebuilt embedded admin-ui assets.

## Acceptance Criteria

- [x] New contract types for Composition Preset and Composition Bundle,
      built from `Layout`/`Region`/`Block`, each carrying a `Manifest`
      (contract version, required blocks, theme-compatibility hints).
- [x] The compatibility contract itself: a validation function producing a
      structured, explainable `Result` (missing block types, missing slots,
      unsupported contract version) against the live `blocks.Registry` and a
      theme's declared regions — not a boolean.
- [x] DB-backed stores + `internal/api` HTTP handlers to save/list/import
      presets and bundles, validate-then-persist, capability-gated writes,
      correctly-numbered migrations (17, 18, re-verified live).
- [x] New `sdk-js` client methods mirroring `layouts.ts`'s conventions.
- [x] Admin-ui scope decision made and documented: minimal real integration
      ("Save as preset" in the builder) shipped; full management UI
      deferred.
- [x] TDD followed: `pkg/compat`'s compatibility-checking function (the
      highest-value seam) covered first and most thoroughly — all-present,
      missing block (both declared and tree-only), missing slot, no-
      restriction theme, unsupported/newer contract version, bundle-level
      aggregation — with the store/API layers' tests built on top of it,
      using real SQLite/registry/theme dependencies throughout, no mocks.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] `cd admin-ui && npm run build && npm test` green (97 tests).
- [x] `cd sdk-js && npm test` green (61 tests).
- [x] Smoke-tested against the real compiled `glyphuxd` binary: setup,
      login, preset save/check/import (both satisfied and unsatisfied slot
      cases), bundle save/import (page merge + real sample-content-item
      creation against a real content type).

## Risks

- **No live "selected theme" wiring anywhere in the daemon** — `Theme.
  Regions()` is real and implemented, but nothing server-side currently
  knows "which theme is this deployment's active one" to source
  `theme_regions` from automatically; every check/import call must be told
  explicitly. A future slice that wires an actual active-theme selection
  into `cmd/glyphuxd` (likely alongside P4.5 live preview or a themes-
  management feature) should replace `themeRegionsParam`'s "caller supplies
  it" convention with a server-resolved value, at which point the explicit
  parameter can become an override rather than the only source.
- **`CompositionBundle.Presets` are compatibility-checked but not
  separately persisted as reusable `preset.Record`s on Import** — see Open
  Questions. A user who imports a bundle cannot yet browse/reuse one of its
  bundled presets independently afterward; only the bundle's own pages
  benefit from the merge.
- **No admin-ui management UI for presets/bundles** (list saved presets,
  import one into an arbitrary route, browse/import bundles) — an explicit,
  documented scope decision (see Current Decisions), not an oversight. The
  `sdk-js` surface (`client.presets`/`client.bundles`, full `list`/`get`/
  `save`/`check`/`import`) is complete and ready for that UI whenever it's
  built.
- **No optimistic-concurrency/CAS on preset/bundle writes or on the Layout
  merges Import performs** — inherited directly from `internal/layout.
  Store`'s own identical, previously-flagged risk (slice 4.4a's tracking
  doc). Two admins racing an import into the same route will silently
  last-write-wins.
- **Region-level (not block-level) merge granularity on Import** — see
  Current Decisions. A preset importing into a route whose Layout already
  uses the same region name for something else replaces that region
  wholesale rather than attempting to interleave/append blocks within it.
  Documented as a deliberate scope decision, not silently accepted breakage:
  a caller who wants additive merging within a shared region must currently
  choose non-conflicting region names.
- **Ticket P4.7 (marketplace distribution) will build directly on
  `pkg/contract.CompositionPreset`/`CompositionBundle`/`Manifest` and
  `pkg/compat.Result`** — these types were kept deliberately clean (no
  storage-assigned IDs, no admin-ui-specific fields) for exactly that reason,
  but P4.7 is the first real external consumer to prove it; any shape gap
  found there is expected, not evidence this slice got the contract wrong.
