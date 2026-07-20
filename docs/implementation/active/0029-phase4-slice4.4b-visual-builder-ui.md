# Implementation: Phase 4 slice 4.4b — visual builder UI

## Goal

PRD §14 slice 4.4, per `docs/specs/phase3-4-spec.md`'s Ticket P4.4b: the
visual page-builder UI in `admin-ui`, consuming Ticket DS's design system,
slice 4.4a's `sdk-js` `BlocksResource`/`LayoutsResource`, and rendering via
P4.3's `contract.Layout` shape — using only the public JS SDK, holding no
privileged access. Built on a page-builder framework (Craft.js, chosen over
Puck — see below) supplying the editor's node-tree state model and
serialization to/from `contract.Layout`. Live theme-accurate preview is
explicitly out of scope (P4.5); a plain structural tree view of the current
draft is what this ticket asks for instead.

## Owning Contexts

No `CONTEXT.md` exists yet (see `docs/agents/domain.md`). New:
`admin-ui/src/pages/builder/` (the entire builder page and its supporting
modules). Modified: `admin-ui/src/App.tsx` (two new routes),
`admin-ui/src/components/layout/AppShell.tsx` (a "Builder" sidebar link),
`admin-ui/src/lib/permissions.ts` (a `"layouts:manage"` capability, mirroring
`internal/permission.LayoutsManage`, admin-only), `admin-ui/package.json`
(`@craftjs/core` added as a dependency). Nothing outside `admin-ui` changed
— `sdk-js`'s `BlocksResource`/`LayoutsResource` and every Go package were
already complete from slice 4.4a/4.3 and needed no changes.

## Status

Complete. `go build ./... && go vet ./... && go test -race ./...` green
repo-wide (no Go files touched by this slice at all). `cd admin-ui && npm
run build` green (`tsc -b && vite build`, output committed to
`internal/adminui/dist`). `cd admin-ui && npm test` green — 95 tests across
21 files (9 new for `serialize.ts`'s round-trip logic, 3 for `Palette`, 4
for `TreeView`, 3 for `PropsEditor`, 5 for `BuilderPage`, plus the
pre-existing 76). `cd sdk-js && npm test` green — 49 tests, unaffected (this
slice made no `sdk-js` changes; confirmed rather than assumed, per the
ticket's own instruction to verify it explicitly).

## Current Decisions

- **Craft.js (`@craftjs/core@0.2.12`), not Puck, and not a from-scratch
  dnd-kit-only tree editor.** Both were evaluated against the actual shape
  of this ticket:
  - **Puck** (`@measured/puck`, still pre-1.0 canary releases only) ships a
    complete, opinionated page-builder *application* — its own toolbox/
    layer-panel/properties-panel chrome, and a rendering model built around
    actually mounting each registered component's real React output in the
    canvas (a genuine WYSIWYG live-preview engine). That's a mismatch on
    two counts: (1) this ticket's own design-system requirement is to build
    the builder's chrome *from `admin-ui`'s existing `components/ui`*, not
    adopt a second, independent UI shell that would fight the app's own
    layout/theming; (2) Puck's whole reason for being is accurate
    component-level rendering, which is explicitly the *next* ticket
    (P4.5) — adopting it now means either fighting its live-render model to
    keep preview out of scope, or building real "starter"-theme-accurate
    React renderers for every first-party block a slice early. Puck's
    dependency tree also pulls in a *second* dnd-kit package
    (`@dnd-kit/react`, `@dnd-kit/helpers` — distinct from the
    `@dnd-kit/core`/`@dnd-kit/sortable` already in `package.json`), which
    the ticket's own dependency list didn't anticipate.
  - **Craft.js** is the better fit precisely because it is *not* a
    complete page-builder product — it is a state-management/node-tree
    library ("supplying the editor state model, node tree... " is close to
    verbatim from Craft.js's own README) that leaves every pixel of UI
    chrome to the consumer. `useNode`/`useEditor` give connectors
    (`connect`, `drag`) and a serializable `SerializedNodes` node graph;
    what actually renders for a given node is 100% our own React
    components (`./nodes.tsx`'s `Page`/`RegionCanvas`/`BlockNode`/
    `SlotCanvas`), styled with the exact same `components/ui`/Tailwind
    tokens every other admin-ui page uses. This makes "a plain structural
    view, no theme-accurate preview" the *natural* output — there was
    nothing to suppress or work around, unlike Puck.
  - **A from-scratch dnd-kit-only tree editor was rejected** as needless
    reinvention: the ticket explicitly names Craft.js/Puck as supplying
    "the editor state model, node tree, and serialization" — a hand-rolled
    tree would have to reinvent exactly that (undo/redo-capable node
    graph, drag-drop-to-arbitrary-canvas semantics, linked-node/named-slot
    concept) that Craft.js already provides, tested, in ~30kB.
  - **dnd-kit itself turned out not to be needed at all for this slice.**
    Craft.js ships its own `connectors.create(el, template)` connector — a
    real, working HTML5-drag-and-drop mechanism for exactly "drag an
    element from an arbitrary DOM node (a palette button) onto any
    registered canvas" (see `Palette.tsx`'s `PaletteItem`). Layering
    dnd-kit's own sortable/DnD-context machinery on top would be a second,
    redundant drag-and-drop engine running side-by-side with Craft's own —
    genuinely worse, not more thorough. `@dnd-kit/core`/`@dnd-kit/sortable`
    remain installed (unused by this slice) exactly as they were before —
    removing them is out of this ticket's scope since nothing here added
    them and no other admin-ui page consumes them yet either.

- **Craft.js's "linked node" mechanism is the literal implementation of
  `contract.Block.Slots`' named-slot map.** A container-shaped block's
  render function (`BlockNode`'s `BlockSlots` helper) emits one
  `<Element id={slotName} canvas is={SlotCanvas} />` per slot the block's
  registered `Definition.Slots` declares; Craft.js stores each as a
  `linkedNodes[slotName] -> canvasNodeId` entry on the parent node — which
  is exactly `map[string][]Block`'s own shape, just addressed by node id
  instead of embedded inline. This is why `serialize.ts`'s
  `nodeTreeToLayout` needs no bespoke "is this a slot or a region" logic:
  it walks `node.linkedNodes` generically for every block, whether that
  block is `container` or any future container-shaped first-party/plugin
  block — nothing here is hardcoded to `container` specifically.

- **Node ids are derived purely from tree position (`region:main[0].slot:
  content[1]`-shaped strings), never random/uuid-generated.** Two
  consequences, both deliberate: (1) `layoutToNodeTree` is a pure,
  fully-deterministic function of its `Layout` input — the same layout
  always produces the same graph, which is what makes the round-trip tests
  in `serialize.test.ts` assert on deep equality rather than "shape
  matches, ids ignored"; (2) no id-generation state needs to be threaded
  through or reset between loads, since a fresh `layoutToNodeTree` call
  (one per route load, keyed by React's `key={route}` on `<Editor>`) always
  starts from the same, empty derivation.

- **Every declared slot gets a `SlotCanvas` node even when empty.** A
  freshly-placed `container` block (no children dropped into "content" yet)
  still gets a real, always-rendered drop target — `layoutToNodeTree`
  builds one `SlotCanvas` per slot name straight from the block's
  registered `Definition.Slots`, regardless of whether the specific
  `Layout.Block.Slots` map has an entry for it yet. This is what makes "drop
  a block into a freshly-placed container's slot" actually possible in the
  UI (there's a `SlotCanvas` connector to drop onto) rather than only
  appearing after the first save/reload round-trip.

- **An unregistered/removed block type's existing nested slot content is
  preserved on load, not silently dropped**, even though the builder can no
  longer offer prop-editing or new drops into an unknown block's slots.
  `layoutToNodeTree`'s slot-name resolution falls back to
  `Object.keys(block.slots ?? {})` when the registry has no matching
  `Definition` — so a stale layout saved against an older registry snapshot
  still round-trips its full structure through the builder untouched (see
  `serialize.test.ts`'s "preserves an unregistered block type's existing
  nested slot content on load" case). `BlockNode` renders such a block with
  a destructive-styled badge (`"<type> (unregistered)"`) so the gap is
  visible, not silent, and `PropsEditor` reports the same when that block
  is selected.

- **`nodeTreeToLayout` mirrors `contract.Block`'s own `props,omitempty`/
  `slots,omitempty` exactly**: an empty props object or a slot with nothing
  currently dropped into it serializes to no key at all, not `{}`/`[]`.
  This is asserted directly in `serialize.test.ts` (a bare `container`
  block round-trips with no `props`/`slots` keys whatsoever) so a
  round-tripped-with-no-edits layout is byte-for-byte identical to what was
  loaded, not merely structurally equivalent — a real concern given
  `internal/layout.Store.Save` currently has no optimistic-concurrency
  guard (slice 4.4a's own flagged Risk): an editor who opens a layout,
  changes nothing, and saves should not silently rewrite the document into
  a different (even if equivalent) JSON shape.

- **The props editor (`PropsEditor.tsx`) reuses `pages/content/fields.tsx`'s
  `FieldControl` directly, not a parallel per-kind switch.** `sdk-js`'s
  `BlockDefinition.props` is already typed `Record<string,
  ContentTypeField>` — the exact same shape `FieldControl` already renders
  per-kind for Layer-1 content-type fields (string/richtext/number/
  boolean/date/relation/media, including the relation/media fields' own
  live `client.content.list`/`client.media.list` option-loading). No new
  field-rendering convention was invented; every prop kind a first-party or
  future plugin block declares gets the same editing experience Layer-1
  fields already have, for free.

- **The block registry is threaded to Craft-mounted node components via
  React context (`registry-context.tsx`), not props.** `BlockNode` (and
  `Palette`/`PropsEditor`/`TreeView`) are mounted by `<Frame>`'s own
  internal rendering pipeline from the `SerializedNodes` graph, not from
  `BuilderPage`'s own JSX tree — there is no prop-drilling path from
  `BuilderPage` down into a Craft-managed node's render function. Context is
  the only mechanism that reaches both "ordinary" JSX-mounted components
  (`Palette`, `PropsEditor`, `TreeView`) and Craft-internally-mounted ones
  (`BlockNode`) with the same registry data.

- **`TreeView` derives its display directly from `nodeTreeToLayout`, the
  same function `SaveBar` calls to persist.** There is exactly one function
  that turns "the current Craft node graph" into "a `contract.Layout`" —
  what the structural tree view shows and what gets saved can never
  silently disagree, since they're the same call (`query.getSerializedNodes()`
  piped through `nodeTreeToLayout`), just at different times.

- **Route selection is a plain text input + `react-router` navigation
  (`/builder/:route*` via a `builder/*` wildcard route), not a "list every
  saved layout" page.** There is no `GET /api/v0/layouts` (list) endpoint —
  slice 4.4a only built get/put for one named route — so there is nothing
  to list; a route is a URL path segment an author already knows (matching
  a route in their site), typed directly, mirroring how `sdk-js`'s own
  `LayoutsResource` and the Go `{route...}` wildcard handle multi-segment
  routes (`"blog/index"`). Defaults to `"home"` when no route segment is
  given (`/builder` alone).

- **`Editor` is re-mounted (`key={route}`) on every route change**, rather
  than kept alive and re-hydrated via `actions.deserialize`. Craft's
  `<Frame data={...}>` only reads its `data` prop once, on mount — switching
  routes needs a fresh node graph built from the newly-loaded `Layout`, and
  a full remount is the simplest way to guarantee stale state from the
  previous route's tree (selection, node ids) never leaks into the next.

- **`layouts:manage` added to `admin-ui/src/lib/permissions.ts`'s
  UI-affordance capability list**, admin-only — mirrors
  `internal/permission.LayoutsManage`'s exact real grant (admin-only, per
  slice 4.4a). A non-admin viewing the builder sees the canvas/tree/props
  editor (all read-only operations, matching `GET /api/v0/layouts/{route}`'s
  no-capability-gate reads) but no Save button, with an explicit note
  explaining why — never a silently-disabled button with no
  explanation, matching this repo's existing empty/permission-gated-state
  conventions (`ContentFormPage`'s own `canWrite`/`canPublish` pattern).

## Open Questions — resolved

- **Should the builder validate required props client-side before
  allowing a save?** No — deferred, matching slice 4.4a's own already-
  documented Risk ("no prop-value type-checking... blocks.ValidateLayout
  only checks existence, not prop-value types... The visual builder's
  prop-editing UI is the natural place this gets addressed"). This slice's
  `PropsEditor` marks required fields with the same `*` convention
  `ContentFormPage` already uses, but does not block Save on an empty
  required field — the server's own validation (structural + registry-
  existence) is the actual gate, matching every other admin-ui form's
  reliance on server-side validation surfaced via `GlyphuxApiError`/
  `.issues` rather than duplicated client-side rules. A future slice
  could add this without changing `PropsEditor`'s per-field rendering.
- **Should reordering/moving blocks within a region or between slots be
  supported?** Yes, and it comes for free from Craft.js's own built-in
  drag-and-drop-within-canvas behavior (the `drag` connector on
  `BlockNode`, `RegionCanvas`/`SlotCanvas`'s `canvas` semantics) — no
  additional code was needed for this beyond what already exists for
  create-from-palette; not separately unit-tested (interaction-heavy
  drag-reorder is exactly the kind of thing the ticket's own TDD guidance
  says isn't worth force-fitting into a unit test) but structurally
  identical to Craft.js's own documented canvas-reorder behavior.
- **Live/theme-accurate preview?** Explicitly out of scope per the
  ticket's own text (P4.5). `TreeView` is the plain structural read-out
  asked for instead; no attempt was made to render blocks through
  `themes/starter` or any theme-accurate path.

## Files/Modules Changed

- `admin-ui/package.json`/`package-lock.json` — adds `@craftjs/core@0.2.12`.
- `admin-ui/src/pages/builder/serialize.ts` (new) — `emptyLayout`,
  `layoutToNodeTree`, `nodeTreeToLayout`: the pure tree↔`contract.Layout`
  translation layer, the seam this slice's TDD pass started from.
- `admin-ui/src/pages/builder/serialize.test.ts` (new) — 9 tests: empty
  round-trip, leaf-block round-trip, nested-slot round-trip, region-key-
  order independence, deterministic node ids, always-present empty-slot
  canvases, unregistered-block-type slot preservation, empty-props
  omission.
- `admin-ui/src/pages/builder/registry-context.tsx` (new) —
  `BlockRegistryProvider`/`useBlockRegistry`/`useBlockDefinition`: the
  context threading the live block registry into Craft-internally-mounted
  node components.
- `admin-ui/src/pages/builder/nodes.tsx` (new) — `Page`, `RegionCanvas`,
  `BlockNode`, `SlotCanvas` (the Craft.js "user components"), `resolver`
  (the `<Editor resolver={...}>` map).
- `admin-ui/src/pages/builder/Palette.tsx` (new) + `Palette.test.tsx` (3
  tests) — the block palette; drag-to-create via Craft's own `create`
  connector; a real empty state when no blocks are registered.
- `admin-ui/src/pages/builder/PropsEditor.tsx` (new) + `PropsEditor.test.tsx`
  (3 tests) — the selected-block props form, built on `FieldControl`
  reused from `pages/content/fields.tsx`.
- `admin-ui/src/pages/builder/TreeView.tsx` (new) + `TreeView.test.tsx` (4
  tests) — the plain structural draft read-out.
- `admin-ui/src/pages/builder/BuilderPage.tsx` (new) + `BuilderPage.test.tsx`
  (5 tests) — the top-level page: load (blocks + layout-or-empty), route
  switching, layout, Save.
- `admin-ui/src/App.tsx` — two new routes (`builder`, `builder/*`).
- `admin-ui/src/components/layout/AppShell.tsx` — a "Builder" sidebar link.
- `admin-ui/src/lib/permissions.ts` — `"layouts:manage"` capability,
  granted to `admin` only.
- `internal/adminui/dist/` — rebuilt (`npm run build`), committed per the
  existing "no Node runtime required in production" convention.

## Acceptance Criteria

- [x] Block palette fetched via `BlocksResource.list()`, showing each
      block's `DisplayName` and declared slots, built from design-system
      components (`Card`, `Badge`, `EmptyState`).
- [x] `LayoutsResource.get(route)` loads an existing Layout, or the builder
      starts from a structurally valid empty one on 404.
- [x] Blocks drag from the palette into named regions
      (`Layout.Regions`), and, for container-shaped blocks, into their
      declared named slot(s) — proven via Craft.js's node-tree/linked-node
      model, not flat dnd-kit sorting.
- [x] A props editor for the selected block, driven by
      `Definition.Props`, reusing `fields.tsx`'s existing per-field-kind
      `FieldControl` convention rather than a new one.
- [x] The in-editor tree serializes back to the exact `contract.Layout`
      JSON shape and persists via `LayoutsResource.save(route, layout)` —
      proven both by pure round-trip unit tests (`serialize.test.ts`) and
      an integration test asserting the literal payload
      `LayoutsResource.save` is called with (`BuilderPage.test.tsx`).
- [x] No theme-accurate live preview attempted (P4.5, out of scope); a
      plain structural tree view (`TreeView`) is provided instead.
- [x] Loading/error/empty states: block-registry-empty, layout-not-yet-
      saved-for-route (404→empty layout), and load-failure-for-another-
      reason (real `ErrorState` with retry) all covered by tests.
- [x] `cd admin-ui && npm run build` green; output committed to
      `internal/adminui/dist`.
- [x] `cd admin-ui && npm test` green — 95 tests, 21 files (24 new tests
      across 5 new files for this slice).
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide (no Go changes in this slice; confirmed, not assumed).
- [x] `cd sdk-js && npm test` green — 49 tests, unaffected (no `sdk-js`
      changes in this slice; confirmed, not assumed).
- [x] Craft.js-vs-Puck decision made and documented (see Current
      Decisions) — Craft.js chosen; dnd-kit's existing dependency
      deliberately left unused by this slice (Craft's own `create`/`drag`
      connectors already cover the required drag-and-drop, documented
      above rather than silently not mentioned).
- [x] TDD discipline followed: `serialize.ts`'s round-trip behavior
      (the ticket's own named highest-value seam) was written and green
      before any Craft.js-dependent UI component; each subsequent
      component (`Palette`, `PropsEditor`, `TreeView`, `BuilderPage`) has
      its own render/interaction test written against the mocked
      `@/lib/client` convention every other admin-ui page's test already
      follows (`vi.mock("@/lib/client")`), not a real backend — matching
      this ticket's own explicit "follow existing test patterns" guidance
      over the `/tdd` skill's generic "real dependencies" default, since
      `serialize.ts`'s own tests already are real-dependency (no mocks at
      all — plain functions over plain data).

## Risks

- **No client-side required-prop validation before Save** — see Open
  Questions; the server's structural/registry validation is the actual
  gate, matching every other admin-ui form's existing convention, but an
  author can save a `heading` block with no `text` yet.
- **No optimistic-concurrency guard on the save path** — inherited
  unchanged from slice 4.4a's own flagged Risk (`internal/layout.Store.Save`
  has no CAS); two admins editing the same route's layout at once still
  silently last-write-wins. This slice's props/slots round-trip fidelity
  (no incidental JSON changes on an unmodified reload-then-save) reduces
  how *often* an untouched save clobbers a concurrent edit but does not
  eliminate the race.
- **`admin-ui`'s production JS bundle crossed Vite's 500kB chunk-size
  warning threshold** (currently ~604kB minified / ~185kB gzipped) after
  adding `@craftjs/core` and its transitive dependencies
  (`@craftjs/utils`, `lodash`, `debounce`, `tiny-invariant`). Not code-split
  in this slice — the ticket didn't ask for a bundle-size budget, and no
  other admin-ui page currently uses dynamic `import()`-based code
  splitting to follow as a precedent. Flagged, not silently ignored: a
  future slice adding more heavy, page-specific dependencies (e.g. a rich
  media picker for P4.9) is a natural point to introduce route-based code
  splitting for the whole app, not just the builder page.
- **No automated test exercises actual pointer-drag-and-drop** (dragging a
  palette item onto a canvas, or reordering blocks within a region) — by
  design, per the ticket's own TDD guidance ("the drag-and-drop interaction
  itself is harder to meaningfully unit test"). What's covered instead:
  the full tree-graph transitions those interactions *produce*
  (`serialize.test.ts`'s nested-slot/multi-region cases), and that the
  palette/canvas/props-editor/save wiring is structurally correct
  end-to-end via `BuilderPage.test.tsx`'s mocked-client integration test.
  Manual/exploratory verification of the actual drag gesture in a real
  browser was not performed as part of this slice (no browser automation
  tool was available in this environment) — flagged as a gap a future
  manual QA pass or an e2e-browser-test slice should close, not silently
  assumed to work from the unit-level coverage alone.
- **No `GET /api/v0/layouts` list endpoint exists**, so the builder cannot
  offer a "browse saved layouts" picker — an author must already know the
  route name they want to edit (see Current Decisions). A future slice
  adding such an endpoint (and sdk-js method) would let this page grow a
  proper route picker instead of a bare text input.
