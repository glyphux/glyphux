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
```

Validation failures return `422` with the offending field issues; an undeclared
content type or missing item returns `404`; malformed JSON returns `400`.
Sessions are sent as an `Authorization: Bearer <token>` header or the
`glyphux_session` HttpOnly cookie set at login.

Configuration via environment: `GLYPHUX_ADDR` (default `:8080`),
`GLYPHUX_DATA_DIR` (default `data/`). On a remote server, first-run requires
the setup token printed to the log at boot.

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
