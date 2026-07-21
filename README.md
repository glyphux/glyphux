# Glyphux

A composable application platform: build websites, portals, and apps by
composing content, layout, capabilities, and themes through a single typed,
versioned **composition contract** — then self-host the result anywhere on a
single Go binary.

Full product definition: [docs/glyphux-prd.md](docs/glyphux-prd.md).

## Status: v0.1.0 — Phases 1–4 complete

- **Phase 1 — Headless core**: composition contract, content/media/identity
  domain APIs, drafts/versioning/localization, REST + GraphQL transports,
  admin shell, security baseline (CSRF, MFA, OAuth, rate limiting).
- **Phase 2 — Extension system**: `sdk.Plugin`/`HostAPI`, Tier-B (WASM/Wazero)
  and Tier-C (RPC) sandboxed runtimes, capability dependency graph, event
  bus, install-time consent engine, audit logging.
- **Phase 3 — First-party capabilities**: `notifications`, `seo`, `commerce`,
  `membership`, `marketplace` (signing + entitlement tokens), and `ai`
  (provider-agnostic `generate`/`embed`/`classify` behind swappable
  Claude/OpenAI/Gemini/OpenAI-compatible adapters).
- **Phase 4 — Layout composition + visual builder**: block registry, theme
  rendering contract, drag-and-drop builder UI, live preview, composition
  presets/bundles with an import-time compatibility check, marketplace
  distribution of presets/bundles, an in-builder media picker, and
  AI-assisted authoring (prompt → validated Layer-2 composition fragment,
  never raw markup).

See `docs/glyphux-prd.md` for the full product definition, `docs/specs/`
for phase/ticket specs, and `docs/implementation/completed/` for every
shipped slice's design notes.

## Install a release

```sh
curl -fsSL https://raw.githubusercontent.com/glyphux/glyphux/main/scripts/release/install.sh | sh
glyphuxd
```

Installs the latest release's `glyphuxd`/`glyphux` binaries to
`~/.local/bin` (override with `INSTALL_DIR`; pin a version with
`GLYPHUX_VERSION=vX.Y.Z`). Release archives for linux/darwin (amd64/arm64)
and windows/amd64 are published on the
[Releases page](https://github.com/glyphux/glyphux/releases), each with a
`SHA256SUMS` file the install script verifies against before extracting.
See `scripts/release/build.sh` for how a release is produced.

## Quick start (build from source)

```sh
go build -o glyphuxd ./cmd/glyphuxd
./glyphuxd
```

Open http://localhost:8080 — the first-run wizard creates your admin account
and writes the initial composition, then locks itself permanently. After
setup, the admin/builder UI is served at `/` (React SPA, embedded via
`go:embed` — no separate Node process required in production).

The endpoint list below covers Phase 1's core content/media/identity
surface. Phase 2–4 add a great deal more (layout/block transport, presets
and bundles, the marketplace, first-party capabilities, AI compose) — see
`docs/glyphux-prd.md` and `docs/specs/` for the full API surface; this list
is deliberately not exhaustive.

```
GET    /healthz                        liveness
GET    /api/v0/composition             the resolved composition document
GET    /api/v0/content/ping            contract-driven domain-API read

Authentication (Phase 1):
POST   /api/v0/auth/login              email+password → sets session cookie → 200 / 401
POST   /api/v0/auth/logout             revoke session (cookie or bearer)    → 204
GET    /api/v0/auth/me                 current principal                    → 200 / 401

User management (Phase 1, admin-only via users:manage):
POST   /api/v0/users                   create an account with a role → 201 / 401 / 403 / 422
GET    /api/v0/users                   list every account            → 200 / 401 / 403

Roles and capabilities (v1's fixed matrix — no dynamic role editing yet):
  admin:  content:read, content:read_drafts, content:write, content:publish, media:write, users:manage
  editor: content:read, content:read_drafts, content:write, media:write   (cannot publish or manage users)
  viewer: content:read only (cannot see drafts, cannot write)

Content CRUD (Phase 1) — every write validated against the composition-declared type.
Reads without content:read_drafts (anonymous, or an authenticated viewer) only
see published items — drafts are invisible, not just unlisted. Mutations
require content:write; publish/unpublish require content:publish:
POST   /api/v0/content/{type}          create an item        → 201 / 401 / 403
GET    /api/v0/content/{type}          list items of a type  → 200 (published-only unless content:read_drafts)
GET    /api/v0/content/{type}/{id}     read one item         → 200 / 404 (404 for a draft you can't see)
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
media:write, reads are public:
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
pkg/sdk/            the plugin contract: Manifest, HostAPI, Plugin
pkg/blocks/         block definitions (names/prop schemas/slot names) — importable by plugin/theme authors
pkg/theme/          the read-only theme rendering contract (CompositionView)
pkg/compat/         import-time compatibility checking for presets/bundles
pkg/runtime/wasm/   Tier-B sandboxed plugin runtime (Wazero)
pkg/runtime/rpc/    Tier-C sandboxed plugin runtime (gRPC)
internal/           kernel: composition store, content, identity, db, setup wizard, http, layout, preset, bundle
capabilities/       first-party plugins built on pkg/sdk: notifications, seo, commerce, membership, marketplace, forms, ai
themes/             first-party themes built on pkg/theme: headless, starter
blocks/firstparty/  first-party block implementations
admin-ui/           the admin/builder SPA (React + TS + Vite + Tailwind), embedded into glyphuxd via go:embed
sdk-js/             the public, typed JS/TS client SDK admin-ui itself consumes (no privileged access)
scripts/release/    cross-platform release build + install scripts
docs/                PRD, specs, ADRs, and per-slice implementation notes
```

## License

Apache-2.0 (core).
