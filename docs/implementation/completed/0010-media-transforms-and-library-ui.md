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

Completed.

## Current Decisions

- Transforms: added `Crop` (x/y/width/height) and `Rotate` alongside the
  existing scaling, replacing the old `Resize` method with a single
  `Transform(ctx, id, TransformOptions)` that composes crop -> rotate ->
  resize -> format-encode in one call. Rotate is fixed to 90/180/270 —
  the increments the stdlib `image` package (the only image library in
  go.mod; no third-party codec) makes cheap and lossless via plain pixel
  remapping. Arbitrary angles would need interpolation and real quality
  loss, and PRD §11.4 only asks for "rotate" as a basic operation, so
  fixed increments satisfy the requirement without the added complexity.
  Format is now an explicit, always-honored parameter (`"jpeg"` or
  `"png"` — the two the stdlib supports for encoding) rather than
  inferred from source; omitting it preserves the old default behavior
  (jpeg source stays jpeg, everything else becomes png) for backward
  compatibility. Crop/rotate/format all validate and return
  `ErrInvalidTransform` (mapped to HTTP 400) for out-of-bounds rects,
  unsupported angles, or unsupported formats. Reused the existing single
  `GET /api/v0/media/{id}/file` endpoint with additional query params
  (`crop_x/y/w/h`, `rotate`, `format`, alongside the existing `w`/`h`) so
  a caller can request any combination in one request, exactly matching
  how resize already worked.
- Media metadata: added `tags` (`[]string`, JSON-encoded in a single TEXT
  column — no other field in this schema uses a join table for a small,
  not-independently-queried array, so this matches the existing pattern
  rather than introducing a new one) and separate `source`/`attribution`
  TEXT columns, via migration version 8 (ALTER TABLE ADD COLUMN,
  portable across SQLite/Postgres, same style as composition's version-8
  predecessor). Not wired up automatically anywhere (stock-media
  integration that would populate `source` is explicitly Phase-3 scope,
  §11.4a) — the fields are available for manual tagging/attribution via
  a new `UpdateMetadata` domain-API method and `PATCH /api/v0/media/{id}`.
  `UpdateMetadata` follows the exact capability-check pattern
  `Upload`/`Delete` already established in slice 0006: it accepts a
  `*permission.Principal` and checks `permission.AllowsPrincipal(...,
  MediaWrite)` itself, independent of the transport layer's own check.
- Library UI: added a search input that filters the already-loaded grid
  client-side across filename/alt-text/tags. `internal/media.API.List`
  returns every item unpaginated with no scale guard, so server-side
  search wasn't justified for this pass. Clicking a thumbnail opens a
  detail dialog with a real, larger preview (the unresized `GET .../file`
  route, not the grid's `w=240&h=240` thumbnail) plus inline
  alt-text/tags/source/attribution fields for `media:write` holders
  (read-only holders see the same metadata without the form). Tags
  render as badges on both the grid card and the dialog.
- Both SDKs updated: `pkg/client/media.go` gained `MediaTransform`,
  `MediaMetadataUpdate`, `FileTransformed`, and `UpdateMetadata`;
  `sdk-js/src/media.ts` widened `file()`'s options to the full transform
  set and added `updateMetadata()`. `MediaItem` gained `tags`/`source`/
  `attribution` in both SDKs and in the GraphQL schema's `MediaItem` type
  (regenerated via gqlgen; upload remains REST-only/GraphQL-absent by
  existing documented design, but reads on the existing `mediaItem`/
  `mediaItems` queries now surface the new fields).

## Resolved Questions

- Server-side vs. client-side search: client-side, per the above — the
  list endpoint has no pagination/scale guard suggesting otherwise.
- Rotate semantics: fixed 90/180/270 via stdlib pixel remapping, not
  arbitrary angles — see Current Decisions.

## Files/Modules Changed

- `internal/media/media.go`, `store.go` (+ `media_test.go`) — `Transform`
  (crop/rotate/resize/format pipeline replacing `Resize`),
  `UpdateMetadata`, tags/source/attribution columns (migration v8).
- `internal/api/media.go`, `api.go` (+ `media_test.go`) — widened
  `handleMediaFile` query params, new `PATCH /api/v0/media/{id}` ->
  `handleMediaUpdateMetadata`, `ErrInvalidTransform` -> 400 mapping.
- `internal/graphql/schema/schema.graphql`, `models.go`,
  `schema.resolvers.go`, `generated/*` (+ `graphql_test.go`) — `MediaItem`
  gains `tags`/`source`/`attribution`. (Regenerating via gqlgen also
  surfaced and fixed an unrelated pre-existing bug: a prior `generate` run
  had left `errContentTypeHasItemsGraphQL`'s declaration commented out as
  dead code, which only became a compile error once `generate` ran again —
  restored as a real package-level `var`.)
- `pkg/client/media.go` (+ `media_test.go`) — `MediaTransform`,
  `MediaMetadataUpdate`, `FileTransformed`, `UpdateMetadata`.
- `sdk-js/src/media.ts`, `types.ts` (+ `test/media.test.ts`,
  new `test/png.ts` test-only PNG encoder/decoder) — widened `file()`
  options, `updateMetadata()`, matching wire types.
- `admin-ui/src/pages/media/MediaLibraryPage.tsx` (+ new
  `MediaLibraryPage.test.tsx`) — search, tag/alt/source/attribution
  editing dialog, real preview.
- `internal/adminui/dist/*` — rebuilt bundle reflecting the UI change.

## Acceptance Criteria

- [x] Crop, rotate, and explicit format selection all work, each proven by
      a test that uploads a real image and asserts the transformed output's
      actual dimensions/orientation/format (not just that the call didn't
      error). Covered at three layers: `internal/media` (Go, direct pixel
      sampling via `image.Image.At`), `internal/api` (HTTP, same pixel
      sampling over a real `httptest` server), and `sdk-js` (a
      hand-rolled dependency-free PNG encoder/decoder against the real
      daemon subprocess — see the note below on a real bug this surfaced
      and fixed in the test harness itself, not the transform).
- [x] Tags and source/attribution persist and round-trip through the API
      and both SDKs. Covered by `internal/media`, `internal/api`,
      `internal/graphql`, `pkg/client`, and `sdk-js` tests, plus a live
      curl round-trip against the compiled binary (see below).
- [x] The admin UI's media library supports search, tag/alt-text editing,
      and a real preview. Verified via `@testing-library/react` +
      `jsdom` (18/18 admin-ui tests green) and `tsc -b && vite build`
      producing a clean bundle; **not** click-driven in an actual
      browser — no browser-automation tool was available in this
      environment, so this is an honest gap rather than a claimed
      full browser verification.
- [x] `go build/vet/test ./...` (+ `-race`), `sdk-js` and `admin-ui` test
      suites, all green — reconfirmed as the final step before writing
      this doc.
- [x] Real end-to-end verification against the compiled binary: built
      `cmd/glyphuxd`, ran it against a fresh SQLite data dir, completed
      first-run setup and logged in over real HTTP, uploaded an 8x4
      red/blue PNG, and applied each transform via curl:
      - `crop_x=4&crop_y=0&crop_w=4&crop_h=4` -> `file` reports
        `PNG image data, 4 x 4`; independent Go-decode of the response
        bytes confirms all four corners are pure blue (the cropped
        right half).
      - `rotate=90` -> `file` reports `PNG image data, 4 x 8`; decode
        confirms the top row is red and the bottom row is blue (matching
        the source's left-to-top rotation).
      - `format=jpeg` -> `file` reports a real
        `JPEG image data, baseline, ... 8x4`, `Content-Type: image/jpeg`.
      - Composed `crop+rotate+resize(20x20)+format=jpeg` in one request ->
        `file` reports `JPEG image data, ... 20x20`; decode confirms
        all four corners are blue, proving the full four-stage pipeline
        ran in the declared order.
      - Out-of-bounds crop and an unsupported rotate angle (45°) both
        returned HTTP 400.
      - `PATCH /api/v0/media/{id}` with alt_text/tags/source/attribution
        persisted and round-tripped through a subsequent `GET`, and
        through a GraphQL `mediaItem` query for the same fields.
      - Anonymous `PATCH` was rejected with 401.
- [x] Tracking doc moved to completed with an honest account of what's
      verified (this document).

## Risks

- Overlapped with 0006 (domain-boundary enforcement) on `internal/media`:
  this worktree's branch predated 0006 landing on `dev`, so `dev` was
  merged into this branch first (a clean fast-forward-style merge, no
  conflicts) before any implementation started, picking up `Upload`/
  `Delete`'s principal-checked signatures that `UpdateMetadata` then
  mirrored.
- Scope discipline held: no bulk actions, no folders, no crop/rotate UI
  wired into the library page itself (that's real and exercised via the
  API/SDKs and curl, but not surfaced as a builder-style crop tool in
  this page) — kept to a straightforward library management page, not
  the Phase-4 in-builder picker or an asset-management product.
