# Implementation: Media pipeline transforms (crop/rotate/format) + library UI completeness

## Goal

PRD §1.6 requires the media pipeline to "upload, validate, transform
(crop/resize/rotate/format), serve" plus "a basic media library UI —
upload, browse, search, tag, preview, basic edit". A full audit
(2026-07-18) found `internal/media/media.go` implements only `Resize`
(scaling); no crop, no rotate, and output format is fixed by source
(jpeg stays jpeg, else forced to png) rather than a selectable transform.
The admin UI's media library (`admin-ui/src/pages/media/MediaLibraryPage.tsx`)
only implements upload/browse(grid)/delete — no search, no tag/alt-text
editing UI (the `alt_text` field exists on the model but nothing sets it),
no dedicated preview. §11.4 also requires per-asset metadata including
"tags" and "source/attribution", which the store doesn't persist at all
today (only alt_text, dimensions, mime type).

## Owning Contexts

- (no CONTEXT.md yet — core-kernel: `internal/media`; clients: `pkg/client`,
  `sdk-js`, `admin-ui`)

## Status

In-flight

## Current Decisions

- Transforms: add `Crop` (x/y/width/height) and `Rotate` (90/180/270 or
  arbitrary angle — check what the current image library already in
  go.mod supports before committing to arbitrary-angle, which is more
  expensive/lossy) alongside the existing `Resize`. Add an explicit format
  parameter (jpeg/png/webp if the library supports it) instead of the
  current "same as source, else png" inference — the caller should be able
  to request a specific output format regardless of source format. Compose
  these as a single pipeline (crop -> rotate -> resize -> format) matching
  how the existing Resize is invoked from HTTP query params, so a caller
  can request multiple transforms in one request rather than one at a
  time.
- Media metadata: add a `tags` column/field (string array or comma-joined —
  check how similar array-ish fields are modeled elsewhere in this schema
  first for consistency) and a `source`/`attribution` field per §11.4. Not
  required to wire up automatically (stock-media integration that would
  populate `source` is Phase-3 scope, §11.4a) — just have the field
  available for the library UI's tag/attribution editing to use.
- Library UI: add a search input (client-side filter over already-loaded
  items is fine for V1 — check current item-count scale assumptions before
  deciding server-side search is needed), tag and alt-text editing (a
  lightweight edit affordance — an inline field or a small edit
  dialog, doesn't need to be elaborate), and a real preview (larger
  image view, not just the grid thumbnail — a dialog/lightbox is
  sufficient).
- Both SDKs (`pkg/client/media.go`, `sdk-js/src/media.ts`) need matching
  typed methods for the new transform params and tag/attribution fields.

## Open Questions

- Whether server-side search (a query param on the list endpoint) or
  client-side filtering is sufficient for "search" at this scale — default
  to client-side unless the existing media list endpoint already supports
  pagination at a scale where that would be impractical (check
  `internal/media/media.go`'s List method first).
- Exact rotate semantics (fixed 90-degree increments vs. arbitrary angle) —
  decide based on what's cheap and correct with the existing image
  library, not what's theoretically most flexible.

## Files/Modules Expected

- `internal/media/media.go`, `store.go` (+ tests) — Crop, Rotate, format
  selection, tags/source columns (new migration)
- `internal/api/media.go` — updated transform query params, tag/attribution
  fields on upload/update
- `internal/graphql/*.go` — matching schema/resolver updates if the GraphQL
  media surface needs the new fields (check current schema.graphql scope
  first — media upload is documented as an intentional GraphQL gap; new
  fields on existing queries may still be in scope even if upload isn't)
- `pkg/client/media.go`, `sdk-js/src/media.ts` (+ tests)
- `admin-ui/src/pages/media/MediaLibraryPage.tsx` (+ tests) — search, tag/alt
  editing, preview
- Tracking doc moved to `docs/implementation/completed/` when done

## Acceptance Criteria

- [ ] Crop, rotate, and explicit format selection all work, each proven by
      a test that uploads a real image and asserts the transformed output's
      actual dimensions/orientation/format (not just that the call didn't
      error).
- [ ] Tags and source/attribution persist and round-trip through the API
      and both SDKs.
- [ ] The admin UI's media library supports search, tag/alt-text editing,
      and a real preview — verified in a real browser if feasible, or
      explicitly noted if not.
- [ ] `go build/vet/test ./...` (+ `-race` on touched packages), `sdk-js`
      and `admin-ui` test suites, all green.
- [ ] Real end-to-end verification against the compiled binary: upload a
      real image, apply each transform via curl, confirm the output file
      is actually correct (check dimensions/format with a tool like
      `file`/`identify` if available, not just HTTP 200).
- [ ] Tracking doc moved to completed with an honest account of what's
      verified.

## Risks

- Overlaps with 0006 (domain-boundary enforcement) on `internal/media` if
  both are in flight simultaneously — check `dev`'s log for 0006 merged
  before starting.
- Scope discipline: don't gold-plate the library UI into something
  builder-adjacent (Phase 4's in-builder media picker, §4.9, is explicitly
  future scope) — keep this a straightforward library management page, not
  a rich asset-management product.
