# Implementation: Phase 4 slice 4.5 — live preview

## Goal

PRD §9 (Themes/Surface 3 rendering; §9.1 "themes never mutate composition";
§9.3 the `pkg/theme.Theme.Render(ctx, CompositionView)` rendering contract) +
`docs/specs/phase3-4-spec.md`'s Ticket P4.5, whose entire spec text is one
line: "editor renders the same composition the API serves." Per this
project's recurring pattern (0027/0028/0029's tracking docs each note
discovering a ticket's true scope from the real code, not the terse spec
line), this ticket required deriving concrete scope: slice 4.4b's own
tracking doc explicitly deferred live preview ("Live theme-accurate preview
is explicitly out of scope (P4.5)... a plain structural tree view of the
current draft is what this ticket asks for instead") and its `TreeView.tsx`
doc comment says the same. This slice adds the real thing: a new,
read-only, non-persisting HTTP endpoint that renders an UNSAVED builder
draft through the actual `themes/starter` theme, plus an admin-ui panel
that calls it, debounced, and embeds the result via an iframe's `srcDoc`.

No route-resolution/theme-selection/public-site-serving system exists yet
anywhere in this repo (grepped: no `theme` reference at all in
`internal/api`, `internal/server`, or `cmd/glyphuxd` before this slice —
`themes/starter`/`themes/headless` were built and tested in isolation by
slice 4.3, never wired to an HTTP route). This ticket does not build that
general system; it wires `themes/starter` into exactly one new,
narrowly-scoped preview endpoint, which is all "editor renders the same
composition the API serves" requires for the builder specifically. A
general public-site-serving/route-resolution feature (if the PRD calls for
one) is a distinct, larger ticket this slice does not attempt.

## Owning Contexts

No `CONTEXT.md` exists yet (see `docs/agents/domain.md`). Modified:
`internal/api` (`handleLayoutPreview`, one new route — the first place in
the whole repo that renders through a `pkg/theme.Theme` over HTTP),
`sdk-js` (`LayoutsResource.preview`, `LayoutPreview` type), `admin-ui`
(`pages/builder/LivePreview.tsx`, wired into `BuilderPage.tsx`). No new
package, no new migration, no changes to `pkg/theme`, `themes/starter`,
`internal/layout`, or `pkg/contract` — this ticket is pure transport +
UI on top of infrastructure slices 4.3/4.4a already built and proved.

## Status

Complete. Built TDD, real dependencies throughout: `internal/api`'s new
handler is proven against a real SQLite-backed `layout.Store`, a real
`blocks.Registry` with real first-party blocks, and the real
`themes/starter.Theme.Render` (no fake theme, no mocked rendering) —
`internal/api/layouts_test.go`'s 7 new preview tests. `sdk-js`'s new
`preview()` method is proven against a real compiled `glyphuxd` subprocess
over real HTTP (the existing `global-setup.ts` harness), not a mock.
`admin-ui`'s `LivePreview` component is proven with a mocked `@/lib/client`
(matching every other admin-ui component test's convention — real fetch
would mean spinning up a browser+backend for a unit test, which this
repo's admin-ui suite doesn't do anywhere).

- `go build ./... && go vet ./... && go test -race ./...` green repo-wide.
- `cd sdk-js && npm test` green — 53 tests, 7 files (49 pre-existing + 4 new
  `preview()` tests in `layouts.test.ts`, which went from 6 to 10 tests).
- `cd admin-ui && npm test` green — 99 tests, 22 files (95 pre-existing + 4
  new `LivePreview.test.tsx` tests; `BuilderPage.test.tsx`'s existing 5
  tests updated to mock `client.layouts.preview` since `BuilderPage` now
  renders `LivePreview` for a `layouts:manage`-holding user).
- `cd admin-ui && npm run build` green (`tsc -b && vite build`, output
  committed to `internal/adminui/dist`).
- Smoke-tested via the real compiled binary: `sdk-js`'s test harness
  (`global-setup.ts`) builds and boots the real `glyphuxd` binary as a
  subprocess and every `preview()` test drives it over real HTTP fetch —
  this satisfies "always smoke-test the real compiled binary, not just `go
  test`" for this slice's new endpoint without a separate manual pass.

## Current Decisions

- **New endpoint: `POST /api/v0/layouts/preview`, not a query param on the
  existing `GET /api/v0/layouts/{route}`, and not tied to any route at
  all.** The thing being previewed is an in-memory, possibly-never-saved
  builder draft — it has no natural URL of its own, and a draft's JSON body
  (an arbitrary `contract.Layout` document) is too large and not URL-safe
  to pass as a query string, so POST (body-carrying) is the only sane
  transport shape. The request body is a bare `contract.Layout` — the exact
  same shape `PUT /api/v0/layouts/{route}` already accepts — so a caller
  previews precisely the document it would otherwise PUT, with zero
  translation between "what I'd save" and "what I'm previewing".
- **Response is JSON (`{"html": "...", "content_type": "..."}`), not a raw
  `text/html` body.** Every other endpoint in this transport is JSON
  (mirrors `internal/api`'s whole convention and lets `sdk-js` return a
  typed `LayoutPreview` object through its existing `requestJSON` helper
  rather than adding a second, one-off "raw body" request path just for
  this endpoint). The admin-ui embeds `result.html` via `<iframe
  srcDoc={html}>` — `srcDoc` wants a string, which the JSON envelope
  already gives it directly, no extra `Response.text()` step needed.
  `content_type` travels alongside the HTML (currently always
  `"text/html; charset=utf-8"` since starter is the only theme wired in)
  rather than being assumed, mirroring `pkg/theme.Theme.Render`'s own
  contract of returning `contentType` next to `output` rather than letting
  a caller guess it.
- **Renders through `themes/starter` specifically, hardcoded, not a
  pluggable "active theme" concept.** `themes/headless` is JSON-only and
  has no meaning as a *visual* preview (there is nothing to look at); no
  theme-selection/site-configuration mechanism exists anywhere in this repo
  yet to pick "the" active theme for a real deployment. Rather than
  invent one a slice early just for this endpoint, `handleLayoutPreview`
  constructs `starter.New(s.blocks)` directly, per request (a Theme value
  is a cheap, stateless struct wrapping a registry pointer — no reason to
  cache or field-store it). If/when a real theme-selection system is built
  (a natural future ticket), this handler is the one place that would
  switch from "always starter" to "whatever the running instance's active
  theme is."
- **No Layer-1 content item is passed into the `CompositionView`** —
  `theme.NewCompositionView(nil, &l)`, items always nil. Verified against
  the real code, not assumed: `themes/starter.Render`'s only use of
  `view.Item()` is an optional HTML `<title>` (`if item := view.Item();
  item != nil { ... item.Data["title"] ... }`) — every block's actual
  content comes from the block's own `Props` in the Layer-2 `Layout`
  itself, which the preview request already carries in full. A preview
  request previews a route's *structural* draft in isolation, not any one
  specific Layer-1 content item bound to that route (a route's Layer-2
  layout can render many different Layer-1 items over its lifetime, and
  the builder has no "pick a sample content item to preview against" UI —
  out of this ticket's scope, flagged below). The only visible cost is an
  empty `<title>` in the previewed document; every block's own rendering is
  unaffected.
- **Preview performs full validation before rendering — structural
  `contract.Layout.Validate()` then registry-existence
  `blocks.ValidateLayout()` — identical to what `PUT` does inside
  `layout.Store.Save`, but called directly rather than through the store**
  (there is nothing to persist, so there is no `Store.Save` call at all;
  only the two pure validation functions it also calls are reused).
  `writeLayoutError` — the same error-to-HTTP mapping `handleLayoutGet`/
  `handleLayoutPut` already use — is reused unchanged, so a preview's
  422-with-issues response for an invalid draft is byte-for-byte the same
  shape a real save's failure would be, letting `sdk-js`/`admin-ui` code
  handle both failure modes identically (no new error shape introduced).
- **Never calls `layout.Store.Save` or any other write path.** This is the
  ticket's own hard constraint ("must NOT persist the draft — that's what
  Save already does") and is asserted directly by
  `TestLayoutPreviewDoesNotPersistTheDraft` (Go) and its `sdk-js` mirror
  ("does not persist the previewed draft" in `layouts.test.ts`) — both
  preview a layout for a route, then assert `GET` for that same route still
  404s.
- **Gated behind `layouts:manage` + CSRF, the identical gate `PUT
  /api/v0/layouts/{route}` uses** — not left as a public read like `GET
  /api/v0/layouts/{route}` and `GET /api/v0/blocks` are. This is a
  deliberate departure from "reads are public, only writes are gated": a
  preview request is functionally read-only (no persistence), but it
  accepts and renders an arbitrary, caller-supplied block tree — an
  unauthenticated caller could otherwise use this endpoint as a free
  "render arbitrary layout JSON through the server's real theme" oracle
  with no relationship to anything actually saved. Since preview only ever
  needs to be called from the builder UI (which is already `layouts:manage`
  -gated end to end per slice 4.4b), gating it identically costs nothing
  real and closes that surface. CSRF applies for the same reason it applies
  to every other cookie-authenticated POST in this file, even though this
  one performs no persistence — matching the existing "state-changing-HTTP
  -method-shaped POST gets `requireCSRF`" convention rather than carving out
  a one-off exception.
- **`admin-ui`'s `LivePreview` debounces 400ms
  (`PREVIEW_DEBOUNCE_MS`) after the serialized Craft node tree changes**,
  using a plain `useEffect` + `setTimeout` + a `cancelled` flag (no new
  dependency — `lodash.debounce` is already transitively present via
  `@craftjs/core` per slice 4.4b's own Risk about bundle size, but adding a
  *direct* dependency on it for one `setTimeout` call would be needless).
  400ms was chosen as a middle ground between "feels instant" and "doesn't
  fire one request per keystroke while typing into a `PropsEditor` text
  field" — no strict UX research backs the exact number; it is easily
  tunable (single named export) if a future pass finds it too
  slow/twitchy.
- **The debounced request re-fires on every editor state change with no
  diffing** (no "did the serialized tree actually change" comparison
  beyond React's own reference-equality-triggered effect re-run) — Craft's
  `useEditor` collector already returns a fresh `serialized` object
  reference on every relevant state change, so the effect's dependency
  array does the right thing for free; adding a deep-equality check on top
  would be optimizing a request that's already both cheap (no persistence,
  server-side rendering of a typically-small `Layout` document) and already
  debounced.
- **The preview iframe uses `sandbox=""`** (no `allow-scripts`, no
  `allow-same-origin`, no other token) — defense in depth alongside
  `themes/starter`'s own `html/template` auto-escaping (slice 4.3's fix for
  the stored-XSS hole in an earlier draft of that theme). Even though
  starter's escaping already means no live `<script>` can execute from an
  untrusted block prop, the iframe itself is additionally denied any
  script execution or DOM access to the parent origin — two independent
  layers, neither relied on alone.
- **`LivePreview` is gated behind the same `canManage` (`layouts:manage`)
  check `SaveBar` already uses** — a non-manage viewer sees an explanatory
  note (extended from slice 4.4b's existing "can view but can't save" text)
  instead of a component that would fire debounced requests destined to
  403 forever. Matches this repo's existing "no silently-broken affordance,
  explain the gap" convention (`ContentFormPage`'s `canWrite`/`canPublish`
  pattern, cited by slice 4.4b's own tracking doc for the identical
  `SaveBar` gate).
- **`LivePreview` is added ALONGSIDE `TreeView`, not instead of it** — per
  the ticket's own framing ("add a REAL preview alongside or instead of
  that"). The two serve different, complementary purposes: `TreeView` shows
  what's *placed where* (structure, at a glance, no network round trip);
  `LivePreview` shows what the draft *renders as* (accurate, but a network
  round trip away, debounced). Removing `TreeView` would lose the
  zero-latency structural glance slice 4.4b built for exactly the moments
  `LivePreview`'s debounce hasn't caught up yet.

## Open Questions — resolved

- **Should preview accept a sample Layer-1 content item (for the `<title>`
  and any future Layer-1-aware first-party block) instead of always passing
  `nil` items?** Deferred. Verified against real code (see Current
  Decisions) that no current first-party block reads anything from
  `CompositionView.Items()` — only the page `<title>` is affected, which is
  a cosmetic gap, not a rendering-correctness one. A future slice adding a
  first-party block that reads Layer-1 content directly (there is none
  today) is the natural point to revisit this, likely by letting the
  builder UI optionally pick a sample content item and pass its id in the
  preview request.
- **Should preview be a public, ungated read, matching `GET
  /api/v0/layouts/{route}` and `GET /api/v0/blocks`'s existing
  precedent?** No — see Current Decisions' capability-gating rationale
  (arbitrary-render-oracle concern). This is a considered departure from
  the "reads are public" convention, not an oversight, and is called out
  explicitly here rather than silently deviating from precedent.
- **Should this slice add a general theme-selection/public-site-serving
  system (so a real visitor's `GET /some/route` renders through a theme
  too), since that's arguably a prerequisite for "the API serves" half of
  the ticket's own sentence?** No — out of scope. No such system exists
  anywhere in the repo yet (verified: zero references to `theme`/`Theme` in
  `internal/api`, `internal/server`, or `cmd/glyphuxd` before this slice),
  and building one is a materially larger, separate concern (route
  matching against real Layer-1 content + Layer-2 layouts, theme
  discovery/selection/configuration, static asset delivery — all flagged as
  future work by slice 4.3's own tracking doc). This ticket's actual,
  concrete scope — proven by re-reading the real `themes/starter`/
  `TreeView.tsx`/slice-4.4b-tracking-doc code rather than the terse spec
  line alone — is specifically the *builder's* live preview of an unsaved
  draft, which needs only a narrow, purpose-built endpoint, not a general
  rendering pipeline.
- **Debounce vs. an explicit "Refresh preview" button?** Debounce, matching
  the ticket prompt's own suggested shape ("refreshed on draft changes,
  likely debounced") and this repo's general preference for affordances
  that don't require an extra manual step when the underlying state
  (`TreeView`, in the exact same panel) already updates live with no button
  press.

## Files/Modules Changed

- `internal/api/layouts.go` — `handleLayoutPreview` (new): validates
  (structural + registry), renders via `starter.New(s.blocks).Render`, no
  persistence; reuses `writeLayoutError` for validation failures.
- `internal/api/api.go` — one new route: `POST /api/v0/layouts/preview`
  (`requireCSRF` + `requireCapability(permission.LayoutsManage, ...)`).
- `internal/api/layouts_test.go` — 7 new tests: real-starter-theme
  rendering, untrusted-prop auto-escaping proof, no-persistence proof,
  unregistered-block-type 422, structurally-invalid 422, anonymous 401,
  404-when-`WithLayouts`-not-configured (using `authedServer`, not
  `testServer`, so the 404 path is actually exercised rather than shadowed
  by `requireCapability`'s own 401 for an anonymous caller).
- `sdk-js/src/layouts.ts` — `LayoutsResource.preview(layout)`, `LayoutPreview`
  type (`{html, contentType}`).
- `sdk-js/src/index.ts` — exports `LayoutPreview`.
- `sdk-js/test/layouts.test.ts` — 4 new tests under `describe("preview()")`,
  against the real running `glyphuxd` harness (no mocks): real-render,
  no-persistence, 422-unregistered-block-type, 401-unauthenticated.
- `admin-ui/src/pages/builder/LivePreview.tsx` (new) — the debounced
  preview panel: `useEditor` collector → `nodeTreeToLayout` → debounced
  `client.layouts.preview` → `<iframe sandbox="" srcDoc={html}>`, with a
  real loading spinner and `ErrorState` on failure.
- `admin-ui/src/pages/builder/LivePreview.test.tsx` (new) — 4 tests:
  debounce timing, HTML embedded via `srcDoc`, error state on a failed
  preview request, `sandbox=""` present.
- `admin-ui/src/pages/builder/BuilderPage.tsx` — renders `<LivePreview />`
  alongside `SaveBar`, both gated on `canManage`; extends the existing
  no-permission note to mention preview too.
- `admin-ui/src/pages/builder/BuilderPage.test.tsx` — mock `client.layouts`
  gains `preview: vi.fn()`, defaulted to a resolved value in `beforeEach` so
  the pre-existing 5 tests (which now also render `LivePreview`, since the
  mocked user is admin/`canManage`) keep passing unchanged in intent.
- `internal/adminui/dist/` — rebuilt (`npm run build`), committed per the
  existing "no Node runtime required in production" convention.

## Acceptance Criteria

- [x] A new endpoint accepts an unsaved draft `contract.Layout` and renders
      it through the real `themes/starter` theme, returning HTML —
      `POST /api/v0/layouts/preview`.
- [x] The endpoint performs NO persistence — proven directly
      (`TestLayoutPreviewDoesNotPersistTheDraft` and its `sdk-js` mirror).
- [x] Rendering goes through the REAL theme code, not an admin-ui-side
      reimplementation — proven by `TestLayoutPreviewRendersDraftThrough
      RealStarterTheme` and `TestLayoutPreviewEscapesUntrustedProps`
      exercising `themes/starter.Theme.Render` itself over real HTTP, and by
      `LivePreview.tsx`'s own doc comment/design explicitly rejecting a
      client-side re-implementation.
- [x] Validation failures (structural, unregistered block type) return the
      same 422-with-issues shape `PUT` already does.
- [x] `admin-ui`'s builder embeds the preview via an iframe, debounced on
      draft changes, alongside (not replacing) `TreeView`.
- [x] Endpoint/UI design decisions documented with reasoning (this file),
      matching slice 4.4b's Craft.js-vs-Puck precedent.
- [x] `sdk-js` gained a typed `preview()` client method with its own tests.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] `cd admin-ui && npm run build && npm test` green.
- [x] `cd sdk-js && npm test` green (real-daemon integration tests,
      including the new `preview()` coverage).
- [x] TDD discipline followed: Go handler tests written before the
      implementation (verified failing first: `TestLayoutPreviewIs404
      WhenNotConfigured` failed against `testServer`'s anonymous-caller
      shadowing before being corrected to use `authedServer`, an actual
      red-then-fixed cycle, not a retrofitted test); `sdk-js` tests against
      the real running-daemon harness (no mocks); `admin-ui` component test
      against the existing mocked-`@/lib/client` convention every other
      admin-ui page's test already follows.
- [x] Migration version check: grepped `Version:` across `internal/*/*.go`
      immediately before starting — highest is still 16 (`internal/layout`,
      slice 4.4a); this slice adds no new migration (stateless render-only
      endpoint, no new persistence), so no new version number was needed.

## Risks

- **No sample-Layer-1-content-item support in preview** — see Open
  Questions. Only cosmetic (`<title>` is always empty in a preview); no
  first-party block today reads Layer-1 content, so no block's actual
  rendered output is affected.
- **No general theme-selection/public-site-serving system** — inherited,
  not introduced, by this slice (see Open Questions); `themes/starter` is
  still not reachable by an ordinary site visitor's `GET` request anywhere
  in this repo. This ticket only wires it into the new preview endpoint.
- **400ms debounce is a judgment call, not a measured UX number** — easily
  retuned via the single `PREVIEW_DEBOUNCE_MS` export if it proves too slow
  or too twitchy in real use.
- **No test exercises the actual iframe's rendered visual output in a real
  browser** (only `srcDoc`'s string value is asserted, per this repo's
  existing jsdom-based admin-ui test environment) — consistent with slice
  4.4b's own flagged gap around real-browser drag-and-drop verification;
  no browser-automation tool is available in this environment.
- **Every debounced draft change triggers a real server round trip with no
  request cancellation of an in-flight previous request** (only a
  `cancelled`-flag guard against applying a stale response, not an
  `AbortController`-based cancellation of the network request itself) — a
  reasonable simplification for a `Layout` document that renders cheaply
  server-side today; a future slice adding a slow/expensive first-party
  block could revisit this with real request cancellation.
