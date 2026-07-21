# Implementation: Slice 1.13 — Admin shell

## Goal

A usable, minimal admin surface for managing content types, content, media,
and identity through the browser — a client of the contract, no privileged
access. Per PRD §16 Phase 1, slice 1.13, and §5.6/§5.7.

## Owning Contexts

- `docs/glyphux-prd.md` §5.6 (Surface 2: admin/wizard/builder is one React
  stack), §5.7 (quality bar + configurability), §16 Phase 1 slice 1.13

## Status

Completed. Built end-to-end (login, content-type management, content
CRUD/publish/versions, media library, user management), verified against
the real compiled `glyphuxd` binary with a real browser (Playwright), all
Go and frontend tests passing, rebased cleanly onto latest `dev` (including
the merged slice 1.12 GraphQL transport — no conflicts of substance in
`internal/server/server.go`, which now carries both changes).

## What's built and working (page by page)

- **Login** (`admin-ui/src/pages/LoginPage.tsx`): email/password via
  `sdk-js`'s `auth.login()`, bearer token persisted to `localStorage`
  (`admin-ui/src/lib/client.ts`), attached to the shared `GlyphuxClient` on
  reload. Unauthenticated users are redirected to `/admin/login` and back
  to where they came from on success (`ProtectedRoute.tsx`).
- **Content types** (`pages/content-types/`): list (with per-type field
  badges), create/edit via a dynamic field-row form (name, type, required,
  localized, relation target), drag-to-reorder fields via `@dnd-kit`, and
  delete with a confirm dialog. All through `client.contentTypes`.
- **Content** (`pages/content/`): per-type list, create/edit via a form
  generated from the content type's declared fields (string/richtext/
  number/boolean/date/relation/media each get an appropriate control),
  publish/unpublish, delete, and a version-history tab with restore
  (rollback). All through `client.content`.
- **Media** (`pages/media/MediaLibraryPage.tsx`): grid view, multi-file
  upload, thumbnails via the media file endpoint, delete. Through
  `client.media`.
- **Users** (`pages/users/UsersPage.tsx`, admin-only): create (dialog form)
  and list, gated on the `users:manage` capability both in the nav (hidden)
  and on the page itself (shows a permission notice instead of erroring if
  navigated to directly). Through `client.users`.
- **Quality floor**: a real design-token layer in `admin-ui/src/index.css`
  (color scale, semantic role tokens, radius/elevation/motion scales, a
  5-role type scale) consumed via Tailwind v4's `@theme inline`, not ad-hoc
  values in components. Light/dark theming (`lib/theme-context.tsx`,
  persisted, a `.dark` class + `color-scheme`) plus a density preference
  (comfortable/compact) persisted the same way. Hand-authored shadcn-style
  components (`components/ui/*`) on Radix primitives + `class-variance-
  authority` — not pulled via the shadcn CLI, since that requires live
  registry network access; the components follow the same structure/
  conventions shadcn generates. Visible focus rings, `aria-live` toasts and
  spinners, real empty/error/loading states on every list/detail view
  (`components/layout/{EmptyState,ErrorState,ListSkeleton}.tsx`), reduced-
  motion respected globally, responsive down to a 390px mobile viewport
  (collapsible sidebar drawer).

## Not built / explicitly out of scope

- Saved views, column configuration, plugin-registered admin pages,
  the visual builder — all explicitly deferred to Phase 2/4 per §5.7's
  sequencing note, not attempted.
- Localized field editing is minimal: a localized field currently edits a
  single `en` locale (stored as `{"en": value}`), not a full per-locale
  management UI. This wasn't specified in the brief; flagging it as a
  scope decision made to keep the pass bounded, not a known defect.
- Delete flows for content types, content items, and media assets are
  implemented (with confirm dialogs) and covered by a component test for
  content-type delete, but were **not** driven in the live-browser
  Playwright pass — the brief's stated e2e minimum ("login → create a
  content type → create a content item → publish it → upload a media
  asset → create a user") doesn't include delete, so that's the bar this
  pass verified against. Unpublish is implemented but likewise not
  e2e-driven (publish was).
- No dedicated relation-target / media-picker preview beyond a plain
  `<select>` of id → title/filename — functional, not visually rich.

## SDK gap found and fixed (flagged per the task's instructions)

`GET /api/v0/content-types` (`internal/api/contenttypes.go`) serializes a
fresh install's unset `composition.ContentTypes` — a nil Go map — as JSON
`null`, not `{}`. The SDK's `ContentTypesResource.list()` return type
promised `Record<string, ContentType>`, but at runtime returned `null` on
any fresh/empty install. This is exactly what a first-run admin session
hits (login → dashboard, before any content type exists), and it crashed
the admin UI (`Object.keys(null)` inside `useContentTypes`) on first load
after login — found via the real end-to-end Playwright run, not a unit
test. Fixed narrowly in `sdk-js/src/content-types.ts`: `list()` now
normalizes `null` to `{}` before returning. This is a one-method, ~8-line
change (see `git diff dev...HEAD -- sdk-js`), left the return type
contract intact (callers were always promised a `Record`, now they
actually get one), and all 30 existing `sdk-js` tests still pass — the
existing test harness always seeds a `post` content type, so it never
exercised the empty-map case; no new automated regression test was added
for it specifically, since sdk-js's own convention is real-server-only
tests (no mocked fetch) and reproducing "genuinely zero content types on a
live server" cleanly wasn't worth restructuring their fixture for in this
pass — this is a disclosed, honest gap, not a hidden one. The `internal/
api/contenttypes.go` server-side nil-map behavior itself was **not**
touched (out of this slice's lane; the SDK-side fix is sufficient and
narrower).

I also found and fixed a real Go bug of my own while wiring the SPA
fallback: `internal/adminui.Handler()`'s first version rewrote the request
path to `/index.html` and delegated to `http.FileServerFS`, but
`http.FileServer`'s implementation 301-redirects any request whose
resolved file is literally named `index.html` to a trailing-slash URL
(its directory-index heuristic) — which broke every deep client-side route
(`/admin/content-types`, `/admin/content/post/abc`, etc.) on a hard
navigation or reload. Fixed by serving `index.html` directly via
`http.ServeContent` for the SPA-fallback case instead of routing through
`FileServer`, which has no such special-casing. Caught by the real-browser
e2e pass, not by `go test`.

## Where things are mounted

- `internal/adminui/adminui.go`: `go:embed all:dist` of `admin-ui`'s
  `npm run build` output (built straight into `internal/adminui/dist` by
  `admin-ui/vite.config.ts`, since `go:embed` can't reach outside its own
  package directory). `Handler()` serves real files at their real path and
  falls back to `index.html` for anything else (SPA routing).
- `internal/server/server.go`'s `Handler()`: mounts `GET /admin/` →
  `requireSetupComplete(wizard, http.StripPrefix("/admin", adminui.Handler()))`.
  `requireSetupComplete` mirrors the existing root-redirect pattern —
  redirects to `/setup` if `!wizard.Complete()` — so the SPA is never
  reachable (showing a broken, data-less UI) before first-run setup has
  actually run. This merged cleanly with slice 1.12's GraphQL transport
  change to the same function (`graphqlHandler ...http.Handler`, variadic)
  — no manual conflict resolution needed beyond the automatic merge.
- `cmd/glyphuxd/main.go`: **untouched** — `buildFullHandler` already called
  `server.Handler(apiServer, wizard)`, which now includes the admin mount
  with no signature change needed on `main.go`'s side (the GraphQL slice's
  variadic param is optional and main.go doesn't pass it, which is fine).
- `internal/setup` and `internal/bootstrap`: **untouched**, confirmed via
  `git diff dev...HEAD --stat -- internal/setup internal/bootstrap`
  (empty).

## Verification performed

- `go build ./...`, `go vet ./...`, `go test ./...`: all clean, all
  passing, on the final rebased-onto-`dev` state (includes the merged
  GraphQL slice 1.12 packages).
- `admin-ui`: `npm run build` clean; `npm test` (Vitest + Testing Library),
  4 test files / 12 tests, all passing — `lib/permissions.test.ts` (pure
  role-capability logic), `pages/LoginPage.test.tsx` (render, successful
  login + redirect, failed login + error surfaced), `components/layout/
  ProtectedRoute.test.tsx` (redirect when unauthenticated, renders when
  authenticated), `pages/content-types/ContentTypesListPage.test.tsx`
  (empty state, populated list, delete-with-confirm flow). All mock at the
  `@/lib/client` seam (the shared `GlyphuxClient` instance), never `fetch`.
- `sdk-js`: `npm test`, 5 files / 30 tests, all passing against a real
  `glyphuxd` binary (unchanged from before this slice, confirms the
  `content-types.ts` normalization fix didn't regress anything).
- **Real end-to-end browser verification** (this project's standing rule):
  built the real `glyphuxd` binary with `internal/adminui` wired in, ran it
  as a subprocess against a scratch data dir (`GLYPHUX_DATA_DIR`), completed
  first-run setup for real via `POST /setup`, then drove the actual running
  admin shell with a real headless Chromium browser (Playwright) through:
  1. Navigate to `/admin/` → redirected to `/admin/login` (unauthenticated
     gate confirmed).
  2. Log in with the admin account created by `/setup` → redirected to the
     dashboard, "Welcome back" visible.
  3. Create a content type `post` with fields `title` (string, required)
     and `body` (richtext), including exercising the drag-reorder-capable
     field list UI.
  4. Create a `post` content item with real field values.
  5. Publish it — status badge flips `draft` → `published`; confirmed the
     version-history tab shows version 1.
  6. Upload a real PNG file to the media library — thumbnail renders,
     appears in the grid.
  7. Create a new user (`editor@example.com`, role `editor`) — appears in
     the users table.
  8. Toggled dark mode — full UI re-themes correctly (screenshot).
  9. Resized to a 390×844 mobile viewport — sidebar collapses to a
     hamburger drawer, cards stack in a single column, fully usable
     (screenshot).
  - Screenshots for every step are in the agent's scratch directory
    (`glyphux-e2e/screenshots/01-dashboard.png` through `10-mobile.png`);
    not committed to the repo (scratch verification artifacts, not
    product code).
  - This run is what caught both real bugs described above (the SDK
    null-map gap and the `index.html` 301-redirect bug) — component tests
    alone did not surface either, which is exactly why this project's
    standing rule requires the real-binary/real-browser pass.
  - Re-ran a smaller smoke pass (login → dashboard) after rebasing onto
    latest `dev` (which merged in the GraphQL transport slice and changed
    `server.Handler()`'s signature) to confirm `/admin/*` still serves
    correctly post-merge; passed.
  - All scratch processes, the scratch binary, and scratch data
    directories were killed/removed after verification.

## Deviations / decisions not pre-specified

- **Component library**: hand-authored shadcn-style components instead of
  running the `shadcn` CLI, since that CLI fetches component source from a
  live registry and there was no guarantee of that network access in this
  environment. The components follow the same conventions (Radix
  primitives, `cva` variants, the same prop/className shape) so swapping in
  CLI-managed components later is a mechanical, low-risk change if desired.
- **Workspace wiring**: `admin-ui/package.json` depends on `"@glyphux/sdk":
  "file:../sdk-js"` (a local `file:` dependency, per the brief's suggestion)
  rather than a root npm workspace — no root `package.json` was added.
- **`vite.config.ts` build output**: set to `../internal/adminui/dist`
  directly (not a plain `dist/` needing a separate copy step), since
  `go:embed` patterns can't contain `..` and so must embed a directory
  physically inside the Go package. The built output is **committed** to
  git (`internal/adminui/dist/`) so `go build ./...` works out of the box
  for anyone who hasn't run `npm run build` in `admin-ui/` — matching the
  PRD's "no Node runtime required in production" framing. Anyone changing
  `admin-ui/` source must re-run `npm run build` there and commit the
  result; `go build` alone will not regenerate it.
- **Media thumbnails use a direct `<img src="/api/v0/media/{id}/file?w=&h=">`
  URL**, not the SDK's `media.file()` (which returns a `Response` for
  programmatic consumption, not a URL). This isn't a `fetch` call in admin
  code — the browser issues the request when it renders the `<img>` tag —
  and the endpoint requires no auth capability server-side (same as other
  public-read GET routes), so this is the standard way to point an `<img>`
  at a same-origin asset endpoint, not a bypass of the SDK.

## Acceptance Criteria

- [x] Login page authenticates via `sdk-js` and persists the session.
- [x] Content-type management: list/create/edit/delete content types through the UI.
- [x] Content: list/create/edit/delete/publish/unpublish for any declared content type.
- [x] Media library: upload/browse/delete.
- [x] User management: create/list users (admin-only, matching `users:manage`).
- [x] Design-token system + light/dark theming + WCAG 2.1 AA floor in place from the start, not retrofitted.
- [x] `admin-ui` builds cleanly; `internal/adminui` embeds it; `go build ./...` still produces one binary that serves `/admin/*` once setup is complete.
- [x] Verified end-to-end: the real compiled `glyphuxd` binary, real browser interaction, covering at minimum login → create a content type → create a content item → publish it → upload media → create a user.
- [x] `internal/setup` and `internal/bootstrap` are unmodified.

## Risks / follow-ups for a future pass

- Delete/unpublish flows would benefit from their own e2e coverage in a
  future pass (implemented, component-tested, just not browser-driven here).
- Localized-field editing is single-locale (`en`) only; a real per-locale
  editor is future work if/when the localization story needs it in the
  admin UI specifically.
- The `internal/api/contenttypes.go` nil-map-to-`null` behavior itself is
  still there server-side; the SDK now papers over it for every SDK
  consumer, but a non-SDK HTTP client of `/api/v0/content-types` would
  still see `null` on a fresh install. Worth a small server-side fix
  (`comp.ContentTypes` initialized non-nil, or handler-level `?? {}`) in a
  future slice if it's judged worth the churn — not done here since it's
  outside this slice's lane and the SDK fix is the right layer for SDK
  consumers today.
