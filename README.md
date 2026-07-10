# Glyphux

A composable application platform: build websites, portals, and apps by
composing content, layout, capabilities, and themes through a single typed,
versioned **composition contract** — then self-host the result anywhere on a
single Go binary.

Full product definition: [docs/glyphux-prd.md](docs/glyphux-prd.md).

## Status: Phase 0 — Walking Skeleton

The spine, end to end: daemon boot → composition contract v0 → SQLite +
migrations → one contract-driven domain API → first-run web wizard.

## Quick start

```sh
go build -o glyphuxd ./cmd/glyphuxd
./glyphuxd
```

Open http://localhost:8080 — the first-run wizard creates your admin account
and writes the initial composition, then locks itself permanently. After
setup:

```
GET    /healthz                        liveness
GET    /api/v0/composition             the resolved composition document
GET    /api/v0/content/ping            contract-driven domain-API read

Authentication (Phase 1):
POST   /api/v0/auth/login              email+password → sets session cookie → 200 / 401
POST   /api/v0/auth/logout             revoke session (cookie or bearer)    → 204
GET    /api/v0/auth/me                 current principal                    → 200 / 401

Content CRUD (Phase 1) — every write validated against the composition-declared type.
Reads are public; mutations require an authenticated session holding the
content:write capability (admin role, v1):
POST   /api/v0/content/{type}          create an item        → 201 / 401 / 403
GET    /api/v0/content/{type}          list items of a type  → 200
GET    /api/v0/content/{type}/{id}     read one item         → 200 / 404
PUT    /api/v0/content/{type}/{id}     replace an item       → 200 / 401 / 403 / 404 / 422
DELETE /api/v0/content/{type}/{id}     delete an item        → 204 / 401 / 403 / 404

Drafts, publish, versioning (Phase 1) — every write is recorded as an immutable
version; items are created as drafts:
POST /api/v0/content/{type}/{id}/publish            mark published        → 200 / 401 / 403 / 404
POST /api/v0/content/{type}/{id}/unpublish           revert to draft       → 200 / 401 / 403 / 404
GET  /api/v0/content/{type}/{id}/versions            full version history  → 200 / 404
POST /api/v0/content/{type}/{id}/rollback/{version}  restore an old version → 200 / 401 / 403 / 404 / 422
```

Localization (Phase 1): a field declared `"localized": true` in the composition
stores a JSON object of locale → value (e.g. `{"en": "Hello", "fr": "Bonjour"}`).
Reads without `?locale=` return that raw locale map; add `?locale=xx` to a
`GET .../content/{type}` or `GET .../content/{type}/{id}` request to resolve
every localized field to a single value for that locale, falling back to any
available locale if the requested one is missing.

Media pipeline + library (Phase 1) — images only in v1 (png/jpeg/gif),
stored on a local-FS adapter under `<data-dir>/media`; upload/delete require
content:write, reads are public:
POST   /api/v0/media                   upload (multipart "file" field) → 201 / 400 / 401 / 403 / 413 / 415
GET    /api/v0/media                   list metadata                    → 200
GET    /api/v0/media/{id}              one item's metadata              → 200 / 404
GET    /api/v0/media/{id}/file         serve the stored bytes           → 200 / 404
GET    /api/v0/media/{id}/file?w=&h=   resize (aspect-preserving)        → 200 / 404
DELETE /api/v0/media/{id}              delete metadata + file           → 204 / 401 / 403 / 404
Uploads are capped at 10 MiB and exempt from the generic 1 MiB request cap.

Validation failures return `422` with the offending field issues; an undeclared
content type or missing item returns `404`; malformed JSON returns `400`.
Sessions are sent as an `Authorization: Bearer <token>` header or the
`glyphux_session` HttpOnly cookie set at login.

Security baseline (Phase 1): every response carries `X-Content-Type-Options`,
`X-Frame-Options`, and `Referrer-Policy` headers; there is no CORS opt-in, so
browsers deny cross-origin reads by default; request bodies are capped at 1
MiB (`413` over the limit); and repeated failed logins from one address are
throttled (`429` after 10 failures/minute).

Configuration via environment: `GLYPHUX_ADDR` (default `:8080`),
`GLYPHUX_DATA_DIR` (default `data/`). On a remote server, first-run requires
the setup token printed to the log at boot.

Database backend: SQLite (embedded, default) or Postgres, selected before
boot — `GLYPHUX_DB_DRIVER=postgres` and `GLYPHUX_DB_DSN=postgres://...`. Pool
sizing is auto-sized but tunable: `GLYPHUX_DB_MAX_OPEN_CONNS` (default 20),
`GLYPHUX_DB_MAX_IDLE_CONNS` (default 5), `GLYPHUX_DB_CONN_MAX_LIFETIME`
(default 30m, Go duration syntax e.g. `1h`). Every domain package writes
portable `?`-placeholder SQL; the db package rewrites placeholders and swaps
in dialect-specific migration SQL where the two engines diverge (e.g.
`AUTOINCREMENT` vs. `GENERATED ALWAYS AS IDENTITY`) — nothing above the db
package needs to know which engine is running.

The optional developer CLI:

```sh
go build -o glyphux ./cmd/glyphux
glyphux composition validate composition.json
```

## Layout

```
cmd/glyphuxd/       the server daemon (primary entrypoint)
cmd/glyphux/        developer CLI (secondary, optional)
pkg/contract/       the public composition contract types + validator
internal/           kernel: composition store, content, identity, db, setup wizard, http
```

## License

Apache-2.0 (core).
