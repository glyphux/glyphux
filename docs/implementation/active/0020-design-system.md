# Implementation: Design System (premium UI, reusable components)

## Goal

Ticket DS (`docs/specs/phase3-4-spec.md`, "Ticket DS — Design System"):
audit `admin-ui`'s existing Radix-based component library (from Phase 1
slice 1.13) and extend it into a fuller, documented design system so
Phase 3/4's commerce/membership/marketplace and builder admin pages have
components to reuse instead of re-implementing. Scope: formalize design
tokens; fill in missing reusable components (form controls, pagination,
empty states, notifications); establish and verify an accessibility floor;
add a component-preview surface; migrate a handful of existing pages to
new/extended components as genuine drop-in improvements.

## Owning Contexts

- No dedicated `CONTEXT.md` exists for `admin-ui` yet (single-context repo
  per `docs/agents/domain.md`); this ticket extends
  `admin-ui/src/components/ui/` and `admin-ui/src/components/layout/`,
  established by Phase 1 slice 1.13, and touches three existing pages
  (`content`, `content-types`' sibling `content` list, `media`).

## Status

Complete. `npm test` (49 tests, 14 files) and `npm run build` are both
green in `admin-ui/`.

Slice 1.13 turned out to have already built a substantial amount of what
this ticket asked for: a full oklch-based light/dark token system with a
brandable accent, a type scale (display/h1/h2/body/small/mono), radius/
shadow/motion scales, 16 Radix-based `ui/` components (including toast-like
notifications via `lib/toast-context.tsx` and an empty-state/error-state/
list-skeleton set under `components/layout/`), and a global
`:focus-visible` + reduced-motion accessibility baseline. This ticket's
real, added work was narrower than the ticket's own text implies once that
was accounted for — see Current Decisions for what was genuinely missing
vs. already present.

## Current Decisions

- **Design tokens: extended, not duplicated.** Color palette (light/dark,
  brandable accent), typography scale, radius/shadow/motion scales all
  already existed in `admin-ui/src/index.css`'s `@theme` block from slice
  1.13. No spacing-token scale existed as a *named* token layer — but
  Tailwind v4's own default spacing scale (a consistent 0.25rem multiplier)
  is what every existing component already uses (`p-4`, `gap-2`, `px-3`,
  etc.); introducing a *second*, parallel `--space-*` custom-property scale
  on top of an already-consistent utility scale would be duplication, not
  formalization, for zero behavior change. Decision: the spacing scale is
  Tailwind v4's default scale, used consistently — documented as such
  rather than reinvented.

- **One real color-contrast bug found and fixed, not just "checked."**
  Wrote a standalone script (oklch → linear sRGB → WCAG relative luminance
  → contrast ratio, using the actual token values in `index.css`) rather
  than eyeballing swatches. It found `--muted-foreground` (light mode) at
  4.41:1 against `--muted` — under WCAG AA's 4.5:1 for normal text, used
  for every `text-small`/caption/description line in the app. Fixed by
  darkening `--gx-neutral-500` from `oklch(0.55 ...)` to `oklch(0.53 ...)`
  (the only place that token is referenced), landing at ~4.8:1 with a
  barely perceptible visual shift. Verified: script re-run after the fix,
  full `admin-ui` test suite and build still green.

  The same script also found `--border`/`--input` (both modes) at ~1.25:1
  against their surrounding background — under the 3:1 WCAG 1.4.11
  non-text-contrast threshold for UI-component boundaries. **Not fixed**:
  this is the single hairline border shared by every card, table row,
  input, and divider in the app (`--gx-neutral-200`/`--gx-neutral-800`);
  pushing it to 3:1 requires darkening it enough (light: L 0.92 → ~0.66,
  dark: L 0.24 → ~0.48) to visibly thicken/darken every existing border in
  the product — a global visual redesign, not a targeted accessibility
  fix, and outside a "formalize/extend" ticket's remit. Mitigating factors
  recorded here rather than silently dropped: (1) every bordered
  interactive control (`Input`, `Textarea`, `Select` trigger, buttons) also
  carries `shadow-xs` and a `focus-visible` ring — the ring alone measures
  ≥3.97:1 in both modes, so the control's *focused* boundary already
  clears the threshold; (2) plain dividers (card edges, table rows) are
  supplemented by spacing/elevation, not sole indicators of a boundary.
  Tracked as an open follow-up (see Risks), not silently assumed compliant.

- **Toast/notification system: refactored to expose a presentational `ui/`
  component, not rebuilt.** `lib/toast-context.tsx` already had a working
  queue + 5s auto-dismiss + `aria-live` region from slice 1.13, but rendered
  its markup inline rather than through `components/ui/`, and had no manual
  dismiss control — an auto-dismissing notification with no way to act on
  it before it disappears is a real WCAG 2.2.1 (Timing Adjustable) gap.
  Extracted the rendering into `components/ui/toast.tsx` (a `Toast`
  presentational component, `cva`-variant styled like `alert.tsx`/
  `badge.tsx`), added a labeled close button, and left the queue/timer
  logic in `toast-context.tsx` (state ownership stays where the rest of the
  app already depends on it via `useToast()`).

- **`EmptyState`/`ErrorState`/`ListSkeleton` stayed in `components/layout/`,
  not moved to `components/ui/`.** They already existed there from slice
  1.13, consumed that way by three pages already, and are one level less
  "generic primitive" than `ui/`'s Radix wrappers (each embeds specific
  product judgment — e.g. `ErrorState` special-cases `GlyphuxApiError`).
  Moving them would be a pure rename with no functional change; left in
  place and given a test file (`EmptyState.test.tsx`; `ErrorState`/
  `ListSkeleton` are already exercised indirectly via the three list pages'
  existing tests).

- **New components added, each with its own test file**: `textarea.tsx`
  (matches `input.tsx`'s exact styling/validation-state contract),
  `radio-group.tsx` (wraps `@radix-ui/react-radio-group`, newly installed —
  matches `checkbox.tsx`/`switch.tsx`'s forwardRef + Radix-primitive
  convention), `pagination.tsx` (a controlled, presentational page-of-N
  control — the consumer owns `page` and slices its own array, same as
  `Tabs`/`Select`'s controlled-value convention; renders nothing for a
  single page so wiring it in unconditionally is safe). "Sortable" data
  tables (mentioned in the ticket's `## Ticket DS` scope text, but not in
  its own definition-of-done's explicit item list) were **not** added —
  no existing page's table currently has more than one natural sort order
  users have asked for, and adding sortable-column machinery to
  `table.tsx` with no real consumer would be speculative scope; left as a
  documented gap, not a silent omission.

- **Component-preview surface: an internal `/design-system` route, not
  Storybook.** `admin-ui` is a single internal admin tool for one team, not
  a component library published or versioned for outside consumers.
  Storybook would add a second build toolchain, its own dev server, and a
  `.stories.tsx` file-per-component convention with no offsetting benefit
  here — nobody outside this repo consumes these components in isolation.
  A route (`DesignSystemPage`, wired into the existing router/providers/
  theming, reachable via a new "Design system" sidebar link in `AppShell`)
  gets the same "see every variant before you build a new one" outcome for
  near-zero marginal infrastructure, and is itself tested
  (`DesignSystemPage.test.tsx`) the same way every other page is.

- **Page migrations: three, not a full sweep.** Per the ticket's own
  "judgment call... document the choice" framing:
  - `pages/content/fields.tsx`'s `"richtext"` field case replaced a raw
    `<textarea>` (with hand-duplicated Input-matching Tailwind classes) with
    the new `Textarea` component — the clearest possible drop-in win: same
    element, same behavior, one less place `input.tsx`'s styling contract
    could drift out of sync.
  - `pages/content/ContentListPage.tsx` and `pages/media/MediaLibraryPage.tsx`
    both listed every row/asset with no pagination (`MediaLibraryPage`'s own
    existing comment already flagged this: "the library's list endpoint
    returns every item unpaginated ... at a scale this is fine for" — true
    today, not indefinitely). Both now slice their already-loaded array
    client-side (10/page for content, 20/page for the media grid) and wire
    in `Pagination`; a new test in each proves the page-of-N slicing and
    stepping.
  - Left alone: `UsersPage` (account lists are small — admin/editor counts
    in the tens, not hundreds — pagination there would be premature) and
    `ContentTypesListPage` (same reasoning: number of *declared types* is a
    schema-design decision, not user-generated data that grows unbounded).
    `RadioGroup` was not wired into any existing page — no current page has
    a genuinely mutually-exclusive small-option-set control (switches and
    selects cover every existing case); it is demonstrated in
    `/design-system` only, which is an honest reflection of "no real
    consumer yet" rather than a forced, contrived integration.

## Open Questions — resolved

- **Should the spacing scale get its own named token layer?** No — see
  Current Decisions; Tailwind v4's default scale already is one, formalized
  by documentation rather than a parallel set of custom properties.
- **Fix the border-contrast gap now, as part of this ticket?** No — it's a
  global, pre-existing (slice 1.13) visual property affecting every
  bordered element in the app; flagged as a real, verified gap with
  mitigating factors recorded (focus rings clear 3:1; borders are not the
  sole boundary indicator on interactive controls) rather than silently
  passed over. See Risks for the recommended follow-up.
- **Storybook or a simpler alternative?** Simpler alternative (an internal
  `/design-system` route) — see Current Decisions for the full reasoning.

## Files/Modules Changed

- `admin-ui/src/components/ui/textarea.tsx` (new) + `textarea.test.tsx`.
- `admin-ui/src/components/ui/radio-group.tsx` (new) + `radio-group.test.tsx`
  (adds `@radix-ui/react-radio-group` to `package.json`).
- `admin-ui/src/components/ui/pagination.tsx` (new) + `pagination.test.tsx`.
- `admin-ui/src/components/ui/toast.tsx` (new) + `toast.test.tsx` — the
  presentational half of the notification system, extracted from
  `lib/toast-context.tsx`.
- `admin-ui/src/lib/toast-context.tsx` (modified) — now renders `Toast`
  instead of inline markup; adds a manual dismiss path.
  `admin-ui/src/lib/toast-context.test.tsx` (new) — show/auto-dismiss/
  manual-dismiss interaction tests (none existed before).
- `admin-ui/src/components/layout/EmptyState.test.tsx` (new) — the
  component existed untested; added render/interaction coverage.
- `admin-ui/src/pages/design-system/DesignSystemPage.tsx` (new) +
  `DesignSystemPage.test.tsx` — the component-preview surface.
- `admin-ui/src/App.tsx` (modified) — registers the `/design-system` route.
- `admin-ui/src/components/layout/AppShell.tsx` (modified) — adds a
  "Design system" sidebar link.
- `admin-ui/src/pages/content/fields.tsx` (modified) — `"richtext"` field
  now renders `Textarea` instead of a raw, hand-styled `<textarea>`.
- `admin-ui/src/pages/content/ContentListPage.tsx` (modified) — paginates
  its table (10/page); `ContentListPage.test.tsx` (new).
- `admin-ui/src/pages/media/MediaLibraryPage.tsx` (modified) — paginates
  its grid (20/page, reset on search).
- `admin-ui/src/index.css` (modified) — `--gx-neutral-500` darkened
  (0.55 → 0.53 L) to clear WCAG AA text contrast; see Current Decisions.
- `admin-ui/package.json`/`package-lock.json` — adds
  `@radix-ui/react-radio-group`.
- `internal/adminui/dist/` — rebuilt (`npm run build`), committed per the
  existing "no Node runtime required in production" convention noted in
  `admin-ui/vite.config.ts`.

## Acceptance Criteria

- [x] Design tokens audited; spacing scale formalized-by-documentation
      (Tailwind default), typography/color/radius/shadow/motion confirmed
      already extended-not-duplicated from slice 1.13.
- [x] Missing components added: textarea, radio-group, pagination — each
      with a render + interaction test.
- [x] Toast/notification system extended with a `ui/`-level presentational
      component and a manual dismiss control; interaction-tested.
- [x] Existing `EmptyState` given test coverage (was previously untested).
- [x] Accessibility floor verified programmatically, not assumed: a real
      contrast-ratio script found and led to fixing one real AA text-
      contrast failure; found (and documented, not fixed) one non-text
      border-contrast gap with recorded mitigating factors. Keyboard
      navigation/focus-visible/ARIA labeling confirmed via the existing
      global `:focus-visible` rule, Radix primitives' built-in keyboard
      handling (exercised in `radio-group.test.tsx`), and `aria-live`
      regions on toasts/spinners.
- [x] Component-preview surface built (`/design-system` route) with
      documented Storybook-vs-simpler-alternative reasoning; itself tested.
- [x] At least a few existing pages migrated to new/extended components
      where genuinely drop-in (`fields.tsx` textarea; `ContentListPage`/
      `MediaLibraryPage` pagination); pages left alone are documented with
      reasoning, not silently skipped.
- [x] `cd admin-ui && npm test` green — 49 tests across 14 files.
- [x] `cd admin-ui && npm run build` green.

## Risks

- **Border/input non-text contrast (~1.25:1, both themes) remains under
  WCAG 1.4.11's 3:1 threshold.** Documented, not fixed, in this ticket —
  fixing it requires a visible global border-color change across every
  card/table/input in the product. Recommended follow-up: a small,
  dedicated slice that darkens `--gx-neutral-200`/`--gx-neutral-800`
  (or adds a distinct, slightly stronger `--border-strong` token for
  interactive-control boundaries specifically, leaving decorative dividers
  alone) and does a visual pass across every page afterward.
- **`MediaLibraryPage`/`ContentListPage` pagination is client-side over an
  already-fully-loaded list** (both endpoints return every item
  unpaginated today — a pre-existing backend property, not something this
  ticket changed). Fine at current scale; if either list grows into the
  thousands, real pagination needs a backend query-parameter change too
  (`internal/content`/`internal/media`'s `List` endpoints), which is out of
  this ticket's scope.
- **No sortable-table primitive was added.** Explicitly deferred (see
  Current Decisions) — no existing page needs it yet; a future slice
  wiring commerce/marketplace tables (Phase 3) may be the first genuine
  consumer and should design the sort-state contract against that real
  need rather than speculatively here.
- **`RadioGroup` has no production consumer yet**, only the `/design-system`
  demo — a normal consequence of building ahead of Phase 3/4's pages that
  will likely need it (e.g. a builder's layout-direction choice); not a
  sign the component is unfinished.
