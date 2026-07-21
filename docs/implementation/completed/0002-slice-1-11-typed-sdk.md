# Implementation: Slice 1.11 — Typed SDK (Go + JS)

## Goal

Ship generated/typed client SDKs consuming the existing `/api/v0` content API — the second client of the contract (after the raw HTTP API), proving multi-client per the PRD's central thesis (§3.3). The JS SDK is also the surface the Phase-1.13 admin UI will consume.

## Owning Contexts

- `docs/glyphux-prd.md` §5.3 (repository structure), §16 (Phase 1, slice 1.11)

## Status

Completed

## Current Decisions

- **Go SDK lives at `pkg/client`**, not `pkg/sdk`. `pkg/sdk` (per §5.3) is reserved for the Phase 2 in-process Plugin/HostAPI surface — a different contract (host-to-plugin) from an HTTP client library (external app to daemon). `pkg/client` is public, importable, kernel-free, parallel to `pkg/contract`.
- **JS SDK lives at `sdk-js/`** at the repo root, per the explicit §5.3 repository layout, as its own npm package (`@glyphux/sdk`).
- Both SDKs wrap the existing HTTP surface as-is (content CRUD + drafts/publish/versions/rollback, media upload/get/list/delete/file, auth login/logout/me, users create/list). No new server-side endpoints in this slice.
- **Seams (confirmed with user):**
  - Go SDK: tests call the exported client's methods against `httptest.NewServer` wrapping the real `internal/api.Server` + real sqlite-backed domain stores. No mocked HTTP, no mocked domain layer.
  - JS SDK: tests call the exported TS client's methods against a real running `glyphuxd` (spawned as a subprocess for the test run), real `fetch` calls. No mocked fetch.
- Auth: bearer-token style (`Authorization: Bearer <token>`) is the SDK's auth mechanism for both Go and JS (cookie-based session is the browser-only path already used by future admin-ui; SDK exposes the token explicitly since it's meant for server-to-server/script use too).
- GraphQL (slice 1.12) explicitly out of scope here — REST/JSON only.
- **Server-side gap found and fixed (TDD)**: `POST /api/v0/auth/login` previously only delivered the session token via `Set-Cookie`, unusable by a cookie-jar-less programmatic client. Added a `token` field to the login JSON response (`internal/api/auth.go`, `loginResponse` wrapping `*identity.User`); cookie behavior for browser clients is unchanged. Covered by `TestLoginResponseIncludesBearerToken`.

## Open Questions

- None blocking; npm publish / module naming beyond `@glyphux/sdk` is out of scope for this slice (no registry work).

### JS SDK implementation findings (appended by the sdk-js build pass)

- **Auth-token gap — investigated, found already resolved.** At the start of
  this pass, `POST /api/v0/auth/login` returned only `identity.User` in its
  JSON body (`{id, email, role}`) and carried the session token solely via
  the `glyphux_session` HttpOnly cookie — no way for a non-browser client to
  obtain a bearer token from the login response. Mid-pass, `internal/api/auth.go`
  was updated (concurrently, by another part of this effort — confirmed via
  `git status`/`git diff` showing it newly modified, plus a new
  `TestLoginResponseIncludesBearerToken` in `internal/api/auth_test.go`) to
  return `{id, email, role, token}` via a `loginResponse` wrapper type. The
  JS SDK's `auth.login()` was built and tested against this fixed shape —
  `sdk-js/src/auth.ts` sets the client's bearer token from `result.token`
  automatically on a successful login. No server code was modified by this
  JS SDK work itself.
- **New gap found: no HTTP-level way to declare a content type after
  first-run setup.** The wizard's `POST /setup` form
  (`internal/setup/setup.go`) only collects site name, admin credentials,
  and the database choice — `contract.Composition.ContentTypes` is never
  populated by it, and there is no other route (REST or otherwise) that
  writes to the composition once it exists. The Go-level test suite
  sidesteps this by seeding the composition directly in-process
  (`internal/api/auth_test.go` `testServerWithAuth`), which a black-box JS
  test spawning a real subprocess cannot do. Per this slice's instruction to
  not modify server code and to report gaps rather than guess: the JS SDK
  test harness adds a small fixture-only Go helper,
  `sdk-js/testdata/seedcomposition/`, that calls the same sanctioned
  `internal/composition.Store.Save` domain API (not a raw DB write) to add a
  `post` content type to the daemon's SQLite file while it is stopped,
  between the real `/setup` HTTP call and the long-lived daemon instance the
  SDK tests run against (see `sdk-js/test/global-setup.ts`). This is
  test-only tooling inside `sdk-js/`; it does not change any product
  behavior. Worth deciding, for a future slice: should the composition
  gain an authenticated HTTP write path (e.g. `PUT /api/v0/composition` or a
  content-types admin endpoint) so this is no longer HTTP-inaccessible in
  production either?

## Files/Modules Expected

- `pkg/client/` (new): Go SDK — `client.go`, `content.go`, `media.go`, `auth.go`, `users.go` + `_test.go` siblings.
- `sdk-js/` (new): TS SDK package — `src/client.ts`, `src/content.ts`, `src/media.ts`, `src/auth.ts`, `src/users.ts`, `package.json`, `tsconfig.json`, tests.

## Acceptance Criteria

- [x] Go SDK (`pkg/client`): typed methods for content CRUD/publish/unpublish/versions/rollback, media upload/get/list/delete/file/resize, auth login/logout/me, users create/list — each behavior-tested against a real `internal/api.Server` (`httptest`), plus re-verified against the actually-compiled `glyphuxd` binary over real TCP (login, create, publish, get, media upload, user create all round-tripped correctly).
- [x] JS SDK (`sdk-js`, `@glyphux/sdk`): same surface, typed (content/media/auth/users), tested against a real running `glyphuxd` subprocess for every test (not just spot-checked) — 25/25 tests pass (`npm test`), reconfirmed independently.
- [x] Both SDKs surface domain errors (validation issues, 404s, 401/403) as typed/inspectable errors: `*client.APIError` (Go), `GlyphuxApiError` (JS) — both carry status + issues.
- [x] `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./pkg/client/... ./internal/api/...` all green.
- [x] `sdk-js` package builds (`tsc`, clean `.d.ts` output) and its test suite passes (reconfirmed independently: `npm test` and `npm run build`).

## Risks

- None yet encountered.
