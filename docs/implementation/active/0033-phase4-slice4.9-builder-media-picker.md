# Implementation: Phase 4 slice 4.9 — in-builder media picker

## Status

Complete — PR opened onto `dev`, awaiting `/code-review` and merge.

## Scope (recap)

Per `docs/specs/phase3-4-spec.md`'s "Tickets P4.5–P4.9" section, P4.9 is:
"extends the Phase-1 media library into the builder; stock-media
integration deferred (3.7 deferred)." This is explicitly UI reuse of the
existing Phase-1 media capability (`internal/media`, `internal/api/media.go`,
`sdk-js/src/media.ts`) — no new backend endpoints, no stock-media/external-
image-provider integration (that's the separately-deferred ticket 3.7).

## What existed before this ticket

- `internal/media`/`internal/api/media.go`: full upload/list/get/delete/
  update-metadata/transform API, unchanged by this ticket.
- `sdk-js/src/media.ts`'s `MediaResource`: `upload`, `get`, `list`, `delete`,
  `updateMetadata`, `file` — unchanged, already sufficient for a picker
  (confirmed by reading it and its 14-test suite, `sdk-js/test/media.test.ts`,
  which still passes untouched).
- `admin-ui/src/pages/media/MediaLibraryPage.tsx`: the standalone Media
  admin page — grid, search, upload, delete, metadata-edit dialog. This
  ticket reuses its upload flow and search predicate rather than
  reimplementing them (see "Shared vs. duplicated" below).
- `admin-ui/src/pages/content/fields.tsx`'s `FieldControl`: already had a
  `"media"` case (`MediaField`), but it was a bare `<Select>` populated with
  every media item's raw `id`/`filename` as a dropdown option — no
  thumbnails, no search, no upload path. This is the concrete gap P4.9
  closes.
- `admin-ui/src/pages/builder/PropsEditor.tsx`: already renders block props
  (including the `image` block's `src` prop, `contract.FieldMedia` kind —
  confirmed in `blocks/firstparty/firstparty.go`) through the *same*
  `FieldControl`, per slice 4.4b's established reuse discipline (see its
  own doc comment: "rendered one FieldControl per prop exactly the way
  pages/content/fields.tsx already does").

## What this ticket built

1. **`admin-ui/src/pages/media/MediaPicker.tsx`** (new): a real media-picker
   dialog — lists the library via `client.media.list()`, thumbnails via the
   existing file-serving route (`/api/v0/media/{id}/file?w=...&h=...`,
   already used by `MediaLibraryPage`'s grid), client-side search (reused
   `matchesSearch` — see below), pagination (reused `usePagination`,
   `Pagination`), and an upload affordance calling the exact same
   `client.media.upload(file, file.name)` the standalone Media page calls.
   Clicking an item's "Select …" button calls the picker's `onSelect(item)`
   prop with the full `MediaItem` and closes the dialog — the caller
   decides what to store (here, `item.id`, matching the pre-existing
   dropdown's stored value shape and what `image.src` — a `contract.
   FieldMedia` — already expected).
2. **`admin-ui/src/pages/content/fields.tsx`'s `MediaField`** rewritten: now
   resolves the current `value` (a media id) via `client.media.get(id)` to
   show a real thumbnail + filename (not a raw id), with "Choose media" /
   "Change" opening the new `MediaPicker`, and a "Clear" button that calls
   `onChange(undefined)` directly (no picker round-trip needed to clear).
3. **`admin-ui/src/pages/media/MediaLibraryPage.tsx`**: exported three things
   that already existed as private helpers/consts (`formatBytes`,
   `matchesSearch`, `IMAGE_MIME`) so `MediaPicker` and `fields.tsx` reuse the
   exact same search predicate, byte-formatting, and image-MIME allowlist
   the standalone Media page already uses — no reimplementation, no drift
   between "search" meaning something different in two places.
4. Tests: `MediaPicker.test.tsx` (6 cases — renders items with thumbnails,
   doesn't fetch while closed, search filters, selecting calls `onSelect`
   and closes, upload calls `client.media.upload` and reloads, empty state)
   and `fields.test.tsx` (4 cases — new file, `FieldControl`'s media case
   had no direct test before this ticket — empty state, resolves/displays
   the selected item, picker-driven selection flows through `onChange`,
   clear flows through `onChange(undefined)`).

## Shared-vs-builder-specific `FieldControl` decision

**Decision: fix the shared `FieldControl` (`pages/content/fields.tsx`), not
a builder-specific control.** Both Layer-1 content-type fields and the
Layer-2 builder's `PropsEditor.tsx` render a `FieldMedia`-kind field through
the identical `FieldControl` switch — confirmed by reading
`PropsEditor.tsx` (`import { FieldControl } from "@/pages/content/fields"`)
and its own doc comment describing this as deliberate reuse, matching this
codebase's established anti-duplication discipline (slice 4.4b explicitly
reused `FieldControl` rather than building a parallel props-rendering
switch for the builder). No concrete reason was found for the two contexts
to diverge: an `image` block's `src` prop and a content type's `media`
field both ultimately want "pick or upload an existing library asset, get
back its id" — same interaction, same backend, same stored value shape.
Fixing `MediaField` once inside the shared switch means the builder's
`PropsEditor` gets the real picker automatically, with zero changes to
`PropsEditor.tsx` itself (verified: its existing test suite,
`PropsEditor.test.tsx`, still passes unmodified).

## Go changes

**None.** Confirmed via `git diff --stat -- '*.go'` (empty) before opening
the PR — this ticket is explicitly UI reuse of Phase-1's already-complete
media backend; `go build ./... && go vet ./... && go test -race ./...`
were run repo-wide as a regression check, not because any Go source
changed.

## Explicitly out of scope

Stock-media/external-image-provider integration — deferred to ticket 3.7,
per the spec's own note. `MediaPicker` only ever talks to the existing
in-tree media library.

## Verification

- `admin-ui`: `npm run build` green; `npm test` — 23 files / 105 tests
  passed (was 22 files / ~95 tests before this ticket's two new/changed
  test files).
- `sdk-js`: `npm test` — 7 files / 49 tests passed, including the
  pre-existing 14-test `media.test.ts` suite, unmodified and unaffected.
- Go: `go build ./...`, `go vet ./...`, `go test -race ./...` all green
  repo-wide (see above — no Go files changed).
