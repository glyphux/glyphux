# Implementation: Phase 4 slice 4.3 — theme rendering contract + Layer-2 server-side rendering

## Goal

PRD §9 (§9.1 "themes never mutate composition"; §9.2 theme capabilities;
§9.3 rendering contract, "fully usable headlessly," V1 target server-side
Go templating + static assets) + PRD §14 slice 4.3: "themes render Layer-2
composition." Per `docs/specs/phase3-4-spec.md`'s corrected Ticket P4.3:
**no theme system existed at all before this slice** (Phase 1 deliberately
shipped "no renderer, no builder"), so this is a from-scratch build, not an
extension of anything pre-existing. Builds: a new public `pkg/theme`
package (the `Theme` interface + read-only `CompositionView`); `themes/
headless` (the built-in default JSON theme, proving "fully usable
headlessly" for real); `themes/starter` (a server-side Go-template HTML
reference theme, rendering all four first-party blocks from `blocks/
firstparty`, including the recursive container/slot case) — both themes
confirmed in scope by the user, not headless-only.

## Owning Contexts

No `CONTEXT.md` exists yet (see `docs/agents/domain.md`). New: `pkg/theme`
(new public package, sibling to `pkg/contract`/`pkg/blocks`/`pkg/sdk`);
`themes/headless` and `themes/starter` (new top-level packages, siblings to
`capabilities/` and `blocks/firstparty`, per the PRD's own repo-tree
sketch). Modified: `internal/boundary` (two pre-existing invariant checkers
had to be re-evaluated now that `themes/` is real — see below; this was not
in the ticket's explicit exclusion list, and getting the whole repo's tests
green required it). No other existing package modified — `capabilities/*`,
`pkg/sdk`, `pkg/blocks`, and `pkg/contract/layout.go` are untouched, per
this ticket's explicit scope boundary.

## Status

Complete. Built via TDD, one behavior at a time, against real dependencies
throughout: a real SQLite-backed `content.API` (same `db.OpenSQLite` +
migration pattern `capabilities/forms`'s tests use), a real `pkg/blocks.
Registry` with real first-party blocks registered via `blocks/firstparty.
RegisterAll`, and real `contract.Layout` documents validated with both
`Layout.Validate()` and `blocks.ValidateLayout()` before being rendered —
no mocks anywhere in this slice's tests. `go build ./... && go vet ./...
&& go test -race ./...` green across the whole repo.

## Current Decisions

- **`Theme` is a two-method interface: `Name() string` and `Render(ctx
  context.Context, view CompositionView) (output []byte, contentType
  string, err error)`.** `contentType` travels alongside the bytes rather
  than being assumed by the caller, because a theme decides its own output
  shape (JSON for headless, HTML for starter, anything else a future theme
  might add) — a caller serving HTTP needs the MIME type to set the
  response header correctly, and inferring it from `output`'s bytes would
  be fragile and redundant when the theme already knows it. `ctx` is taken
  for the same reason every other domain-API method in this codebase takes
  one (cancellation/timeouts/tracing — see `internal/content.API`).
- **`CompositionView`'s fields are unexported, with only `Items()`/
  `Item()`/`Layout()` getters and no setter anywhere** — this is the literal
  enforcement of PRD §9.1's hard law ("themes never mutate composition,"
  "no mutation methods anywhere on it"). Rather than relying on convention
  (exported fields a theme *could* technically write to, just "shouldn't"),
  the type structurally has no exported mutation surface: the only way to
  build one is `NewCompositionView`, the only way to read one is the three
  getters. This makes "a theme added a mutation method" a change to
  `pkg/theme/theme.go` itself, not a quiet addition somewhere else.
- **`CompositionView` wraps `[]*content.Item` (plural), not a single
  `*content.Item`**, with `Item()` as a convenience accessor for the
  common single-item-route case. The ticket's own scope text says "content
  item(s)" — ambiguous between one and many — and a list route (e.g. a
  blog index rendering many posts in one pass) is a real case a rendering
  contract needs to support without a second, parallel "multi-item view"
  type. A single-item route is simply a one-element slice; no information
  is lost, and no theme is forced to handle two different view shapes.
- **`Layout` is `*contract.Layout`, nilable** — Layer 2 is additive to
  Layer 1 per PRD §14, never mandatory, so a route with no Layer-2 layout
  at all (pure Layer-1 content) must still produce a valid
  `CompositionView`. `themes/headless` handles a nil Layout by simply
  omitting it from its JSON (`omitempty`); `themes/starter` treats a nil
  Layout as a genuine error, since an HTML theme with nothing to walk has
  nothing meaningful to render (see below).
- **`themes/headless` does not implement any bespoke tree-walk of
  `contract.Layout`'s regions/blocks/slots — it hands a thin JSON-tagged
  projection straight to `encoding/json.Marshal`.** `Layout`, `Region`, and
  `Block` (including `Block.Slots`' nested `[]Block`) already carry their
  own `json:"..."` tags from slice 4.1; `encoding/json`'s ordinary recursive
  marshaling already walks that tree correctly. Writing a second, parallel
  walk in `pkg/theme` or `themes/headless` to do what `encoding/json`
  already does for free would be pure duplication with no behavioral
  benefit — the ticket asks for a function that "actually walks... and
  produces theme-specific output," and for JSON output, the honest answer
  is that `encoding/json`'s own walk already produces exactly that.
  `document.Items` is explicitly normalized so headless clients always see
  a JSON array for a field documented as a list, never `null`, even when
  the view has zero items.
- **`themes/starter`'s recursive block-to-HTML walk (`renderBlock`) is
  theme-specific, hand-written in `themes/starter`, and deliberately does
  NOT reuse `pkg/contract.WalkBlocks`.** `WalkBlocks`'s `visit` signature
  returns a flat `contract.ValidationErrors` accumulator — built for
  Layout.Validate/blocks.ValidateLayout's "collect every violation
  anywhere in the tree" shape, where visit order doesn't matter and no
  caller needs a per-node return value. Starter's HTML rendering is the
  opposite shape: a container's own `<div>` must literally *contain* its
  children's already-rendered HTML strings, in slot order, which needs a
  walk that returns a value up the call stack for the parent to embed, not
  one that accumulates errors into a side list. Bending `WalkBlocks` to
  return per-node output would require closures over a mutable
  path-keyed output map — more machinery, and no more correct, than the
  plain recursive `renderBlock(b contract.Block) (template.HTML, error)`
  actually shipped. This is this slice's explicit "shared vs.
  theme-specific" architectural call the ticket asked to be made and
  documented: shared JSON-tree-walk logic already exists for free via
  `encoding/json`; shared HTML-tree-walk logic does not exist anywhere yet
  and doesn't fit `WalkBlocks`'s shape, so it lives in `themes/starter`
  only, not in `pkg/theme`.
- **`themes/starter` uses `html/template`, not `text/template`** — a
  deliberate, documented call (in `themes/starter/starter.go`'s package
  doc comment), not a default reached for without thought. A `Block`'s
  `Props` come from a Layer-2 `contract.Layout` document (hand-authored
  JSON, a future builder UI, or a plugin), and there is currently NO
  sanitization pipeline anywhere for Layer-2 block props —
  `pkg/contract.Layout.Validate` is purely structural (non-empty Type,
  valid slot names), and `internal/content`'s `sanitizeRichText` runs only
  on Layer-1 content items, inside `content.API.Create/Update/Rollback`;
  it never touches a `Layout` document. Every prop on every block type is
  therefore untrusted. `html/template` auto-escapes every interpolated
  value contextually (HTML body, attribute, URL position); `text/template`
  would emit a prop's raw bytes verbatim, including `<script>...
  </script>`, directly into the page. `TestRenderAutoEscapesUntrustedProps
  OnEveryBlockType` proves this for both `heading`'s and `paragraph`'s
  `text` props, with no exception for either.
  - **Post-review fix (independent `/code-review` audit against this
    slice's own PR #9):** an earlier draft of this file had `renderBlock`'s
    `"paragraph"` case cast its `text` prop to `template.HTML(s)`,
    rendering it unescaped, on the claim that rich text is "sanitized
    server-side on every write... already sanitized at write time." That
    claim was false for the data path this code actually renders: the
    `text` in `b.Props` here is a Layer-2 `contract.Block`'s prop, not a
    Layer-1 content item's field — `sanitizeRichText` never runs on it.
    The cast was a real stored-XSS hole (any `<script>` placed in a
    paragraph block's `text` prop — hand-authored layout JSON, a future
    builder UI, or a plugin-registered layout — would have executed in a
    visitor's browser), and the test that existed at the time
    (`TestRenderAutoEscapesUntrustedHeadingTextButNotRichTextParagraph`)
    asserted the unescaped output as correct, certifying the vulnerability
    instead of catching it. Fixed by removing the cast entirely — every
    prop on every block type, with no exceptions, now renders through
    html/template's ordinary auto-escaping. Rendering a prop as trusted
    HTML (e.g. treating rich text as pre-sanitized markup) is explicitly
    deferred until a real sanitization pipeline exists for Layer-2 block
    props — a natural future ticket, likely alongside whatever
    validates/sanitizes builder-authored layouts in Ticket P4.4 or P4.6.
    The test was renamed to `TestRenderAutoEscapesUntrustedPropsOnEvery
    BlockType` and now asserts both `heading` and `paragraph` text are
    escaped identically.
- **`themes/starter.Theme` takes a `*blocks.Registry` at construction
  (`New(registry *blocks.Registry) *Theme`)**, read-only (only `Get` is
  called, never `Register`) — a theme needs to know which block types it's
  being asked to render and reject an unregistered one with a real error
  (`TestRenderErrorsOnUnknownBlockType`) rather than silently rendering
  nothing or panicking on a missing template lookup.
- **`themes/starter` renders regions in sorted-name order.**
  `contract.Layout.Regions` is a Go map, which has no iteration order of
  its own; rendered HTML output must be deterministic run-to-run (a static
  site generator / any downstream diffing or caching depends on this).
  Proven by `TestRenderIsDeterministicAcrossRegionsRegardlessOfMapOrder`.
- **`themes/starter.Render` errors on a nil `Layout`** (unlike
  `themes/headless`, which happily emits an items-only document). An HTML
  theme with no Layer-2 layout to walk has nothing meaningful to produce —
  there is no first-party convention yet for "render Layer-1 content with
  no layout as HTML" (that would need a default page template keyed off
  content type, out of this ticket's scope), so starter reports the gap
  explicitly rather than emitting an empty or misleading page.
- **Neither `themes/headless` nor `themes/starter`'s production code
  imports `internal/content` (or any other kernel-internal package)** —
  discovered as a real conflict partway through this slice, not designed in
  from the start (see the boundary-invariant fix below), and reworked once
  found. `themes/headless`'s wire-format `document.Items` field is typed
  `any` (holding whatever `[]*content.Item` `CompositionView.Items()`
  returns) rather than naming `internal/content.Item` directly in source —
  Go's import requirement is about referencing an identifier by name, not
  about what concrete type flows through a value at runtime, so this is a
  real, not cosmetic, way to avoid the import while still emitting
  identical JSON. The end result is stronger than originally planned: both
  built-in themes are built using only the same public surface
  (`pkg/theme`, `pkg/contract`, and — for starter — `pkg/blocks`) a
  third-party theme author would be restricted to, proving even the
  built-in default theme needs no special kernel access to do its job
  (mirroring `capabilities/forms`'s own "built entirely on the public
  extension API" framing for capabilities).
- **`internal/boundary`'s invariant 1 (`plugin_imports.go`,
  `CheckPluginKernelImports`) and invariant 4 (`manifest_capabilities.go`,
  `CheckCapabilityDeclarations`) both had explicit tripwire tests
  (`..._VacuousOnRealRepo`) written back in Phase 2, asserting that no
  `themes/` (or `plugins/`) directory existed yet, with an explicit comment
  telling whoever made it real to re-evaluate the invariant, not just
  delete the test. This slice is that re-evaluation:
  - `sanctionedPluginImportPrefixes` (invariant 1) gained `pkg/blocks` and
    `pkg/theme` alongside the existing `pkg/sdk`/`pkg/contract`. Both are
    public top-level `pkg/` packages explicitly designed for plugin/theme
    authors to import (`pkg/blocks.Registry`'s own doc comment says so for
    `pkg/blocks`; `pkg/theme` IS the rendering contract this invariant
    exists to keep themes within), and neither exposes kernel internals
    (`pkg/blocks` is a registry of block *definitions*; `pkg/theme.
    CompositionView` has no exported field or mutation method). Admitting
    them doesn't weaken what the invariant protects against — a
    plugin/theme reaching into `internal/*` state directly — it just
    recognizes that Phase 4 added two more sanctioned public surfaces
    alongside the original two.
  - `CheckPluginKernelImports` now skips `_test.go` files. Test code never
    ships into whatever loads plugin/theme code at runtime, and this
    project's own TDD convention requires first-party theme tests to use
    real kernel-backed dependencies (a real SQLite `content.API`, a real
    `blocks.Registry`) exactly like `capabilities/forms`'s tests already
    do — without this exclusion, that convention and this invariant would
    directly conflict for every first-party theme's test suite forever.
  - Both `..._VacuousOnRealRepo` tests were replaced with real assertions
    against the real `themes/` tree (`TestPluginKernelImportsCheck_
    EnforcedOnRealThemesTree`, `TestCapabilityDeclarationCheck_
    StillVacuousOnRealThemesTree`) rather than simply deleted — invariant 1
    is now genuinely enforced (proven against real production code, which
    could easily have failed it, and did in an earlier draft of this slice
    before the `internal/content` import was removed); invariant 4 remains
    dormant for themes specifically, but now because no `RequireCapability`
    -shaped call exists anywhere in real theme code (themes have no
    capability-request mechanism at all, per PRD §9.1), not because the
    directory itself doesn't exist.

## Open Questions — resolved

- **Should `pkg/theme` itself own a shared "walk a Layout and produce
  output" helper both themes call into?** No — see the "shared vs.
  theme-specific" decision above. The only genuinely shared piece across
  both themes turned out to be the *contract* (`Theme`, `CompositionView`),
  not the walking logic itself; headless's walk is free via
  `encoding/json`, starter's walk has a fundamentally different shape
  (value-returning recursion vs. error-accumulating traversal) that
  doesn't fit `WalkBlocks`.
- **Should `CompositionView` expose a way to look up a specific content
  item by ID/type from `Items()`, beyond the raw slice?** Deferred — not
  asked for by this ticket's scope, and no theme built here needed it
  (`Item()`'s "first item" convenience covers the single-item-route case
  that's actually exercised). A future multi-item-route theme (e.g. a
  paginated blog index) is the natural place to add this if it's ever
  needed.
- **Should `themes/starter` validate a `Block`'s `Props` against its
  registered `Definition.Props` schema before rendering (type-checking
  prop values, not just block-type existence)?** Out of scope — this is
  the identical open question slice 4.1/4.2's tracking doc already flagged
  as a Risk (`blocks.ValidateLayout` only checks existence, not prop-value
  types) and deferred there; this ticket doesn't reopen or extend that
  scope. `themes/starter`'s templates degrade gracefully today (a missing
  prop renders as an empty string/zero value via Go's template engine,
  rather than panicking), which is acceptable for a reference theme.
- **Should first-party built-in themes be exempt from
  `internal/boundary`'s plugin-kernel-import invariant, the way
  `blocks/firstparty` and `capabilities/*` are (by living outside the
  `plugins`/`themes` tree roots the checker walks)?** No — the ticket's own
  scope text explicitly places built-in themes at `themes/headless` and
  `themes/starter`, inside the enforced tree, not beside it. Rather than
  special-casing an exemption, both themes were reworked to need no
  kernel-internal import at all (see above), which is a strictly better
  outcome: it's now proven, not just assumed, that a first-party theme
  needs nothing a third-party theme couldn't also use.

## Files/Modules Changed

- `pkg/theme/theme.go` (new) — `Theme` interface, `CompositionView` (with
  unexported fields), `NewCompositionView`, `Items()`, `Item()`,
  `Layout()`.
- `pkg/theme/theme_test.go` (new) — 3 tests covering `Item()`'s
  first-item/empty-items behavior and `Layout()` passthrough.
- `themes/headless/headless.go` (new) — `Theme`, `New`, `Name`, `Render`,
  `document` (the JSON wire shape, `Items` typed `any` to avoid importing
  `internal/content`).
- `themes/headless/headless_test.go` (new) — 2 tests: a real content item
  + real Layer-2 layout (heading + container/paragraph/image, using real
  `blocks/firstparty` definitions and a real `blocks.Registry`) rendered to
  JSON and decoded back, asserting item and nested-slot structure
  round-trip correctly; and a nil-view case proving `items` always encodes
  as `[]`, never `null`.
- `themes/starter/starter.go` (new) — `Theme`, `New`, `Name`, `Render`,
  `renderBlock` (recursive), `pageTemplate`, `blockTemplates` (heading,
  paragraph, image, container).
- `themes/starter/starter_test.go` (new) — 6 tests: theme identity;
  no-layout error; all four first-party blocks rendered to HTML including
  the nested container/slot case (asserting children appear nested inside
  the container's own `<div>`, in slot order); html/template auto-escaping
  proof (`heading` and `paragraph` text both escaped, no exceptions —
  post-review-fixed, see Current Decisions); unknown-block-type error;
  deterministic sorted-region-order output across repeated runs and
  reordered map construction.
- `internal/boundary/plugin_imports.go` — `sanctionedPluginImportPrefixes`
  gained `pkg/blocks` and `pkg/theme`; `CheckPluginKernelImports` now
  skips `_test.go` files.
- `internal/boundary/plugin_imports_test.go` —
  `TestPluginKernelImportsCheck_VacuousOnRealRepo` replaced with
  `TestPluginKernelImportsCheck_EnforcedOnRealThemesTree` (asserts zero
  violations against the real `themes/` tree instead of asserting the
  directory doesn't exist).
- `internal/boundary/manifest_capabilities_test.go` —
  `TestCapabilityDeclarationCheck_VacuousOnRealRepo` replaced with
  `TestCapabilityDeclarationCheck_StillVacuousOnRealThemesTree` (asserts
  zero `RequireCapability`-shaped calls in the real `themes/` tree instead
  of asserting the directory doesn't exist).

## Acceptance Criteria

- [x] New public `pkg/theme` package: `Theme` interface (`Name`, `Render`)
      and read-only `CompositionView` (Layer-1 items + optional Layer-2
      Layout), with no mutation method anywhere on `CompositionView`.
- [x] `themes/headless`: built-in default theme emitting `CompositionView`
      as JSON, proven with a real test rendering a real content item + a
      real Layer-2 layout built from real `blocks/firstparty` blocks,
      round-tripped back through `json.Unmarshal` and structurally
      checked.
- [x] `themes/starter`: server-side Go-template HTML theme rendering all
      four first-party blocks (`heading`, `paragraph`, `image`,
      `container`) into real HTML via `html/template`, with the
      `html/template`-vs-`text/template` call made and documented
      explicitly (auto-escaping every prop on every block type, with no
      exceptions — an earlier draft's "rich text is pre-sanitized"
      exception was a real stored-XSS hole, fixed post-review; see Current
      Decisions).
- [x] Recursive slot rendering proven: a `container` block's nested
      `content`-slot children render inside its own HTML element, in
      order — `TestRenderProducesHTMLForAllFourFirstPartyBlocksIncludingNestedSlot`.
- [x] Server-side Layer-2 rendering logic identified and its
      shared-vs-theme-specific placement made and documented explicitly
      (headless: free via `encoding/json`; starter: theme-specific
      `renderBlock`, not `contract.WalkBlocks`, with the reasoning why
      recorded above).
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide, including two pre-existing `internal/boundary` invariant
      checkers whose own tripwire tests required re-evaluating the checkers
      themselves now that `themes/` is real (see Current Decisions).
- [x] TDD discipline followed: seam decisions (interface shape,
      `CompositionView`'s read-only design, shared-vs-theme-specific
      rendering-logic placement, `html/template` vs `text/template`) were
      thought through and documented before any test was written; then one
      behavior at a time — `pkg/theme` first (no dependents yet), then
      `themes/headless` (simplest theme, proves the contract is usable at
      all), then `themes/starter` (the actual block-to-HTML rendering
      logic this ticket is named for) — each with real dependencies (real
      SQLite-backed `content.API`, real `blocks.Registry`, real
      `blocks/firstparty` definitions), no mocks.
- [x] `capabilities/*`, `pkg/sdk`, `pkg/blocks`, `pkg/contract/layout.go`
      untouched — this ticket only adds new packages that consume them,
      plus the `internal/boundary` fix described above (not in the
      exclusion list, and required for a repo-wide green test run).

## Risks

- **No daemon/HTTP wiring.** Neither theme is wired into `cmd/glyphuxd`'s
  actual daemon startup or exposed over an HTTP route yet — this mirrors
  every prior capability slice's own documented gap (see slice 4.1/4.2's
  tracking doc: "no existing plugin-loading sequence in the running daemon
  yet at all, for any capability"). `Theme.Render` is real, tested, and
  ready for whatever future slice adds route resolution + theme selection
  + HTTP serving.
- **`themes/starter` has no static asset delivery** (PRD §9.2: "may ship
  assets — CSS, JS, fonts, images"). This ticket's scope was server-side
  block-to-HTML rendering specifically; asset pipeline/delivery is a
  distinct concern (arguably reusing Phase 1's existing media pipeline,
  `internal/media`) not asked for here, flagged rather than silently
  skipped.
- **There is no sanitization pipeline for Layer-2 block props at all.**
  `pkg/contract.Layout.Validate` and `pkg/blocks.ValidateLayout` check
  structure and block-type existence only; nothing sanitizes or restricts
  what a `Block.Props` value contains. `themes/starter` closes the
  immediate stored-XSS risk by auto-escaping every prop unconditionally
  (see Current Decisions' post-review fix), which is correct and
  sufficient for HTML *rendering* safety, but does not add any validation
  at the point a `Layout` is authored/accepted. A future ticket — likely
  alongside whatever validates/sanitizes builder-authored layouts (Ticket
  P4.4/P4.6) — is the natural place to decide whether any Layer-2 prop
  should ever be allowed to carry trusted markup, and if so, how it would
  be sanitized before being marked trusted.
- **No prop-value type-checking before rendering** — carried over
  unchanged from slice 4.1/4.2's own flagged Risk; `themes/starter`'s
  templates degrade gracefully (missing/wrong-shaped props render as
  empty/zero values) rather than rejecting a `Layout` with malformed prop
  values at render time.
- **`themes/starter`'s page template is intentionally minimal** (a bare
  `<!DOCTYPE html>` shell with one `<section>` per region) — there is no
  first-party CSS, no per-content-type page-template selection, no
  head/meta beyond `<title>`. It exists to prove real block-to-HTML
  rendering end to end, not to be a production-ready starter site; a
  future slice building an actual "starter" site product would extend this
  considerably.
- **`internal/boundary`'s invariant 1 now only has real production code to
  check under `themes/`, not `plugins/`** — `plugins/` still doesn't exist
  (no plugin-loading system runs in-tree plugin code yet, same gap every
  prior slice has flagged), so invariant 1's real-repo enforcement is
  one-tree-root-real, one-tree-root-still-hypothetical. Not a gap this
  slice introduced or could close — flagged for whichever future slice
  finally adds real in-tree `plugins/` code.
