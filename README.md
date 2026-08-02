# Glyphux

**Glyphux is a composable, self-hosted application platform** for building
websites, portals, and content-driven apps — a headless content engine, a
visual drag-and-drop builder, a sandboxed plugin/extension system, and a
growing set of first-party capabilities (commerce, membership, SEO,
notifications, AI), all built on one typed, versioned **composition
contract**. It ships as a single Go binary with an embedded database — no
managed cloud dependency, no vendor lock-in, deploy it anywhere you can run
a binary.

## Features

- **Headless content engine** — typed content models, drafts and
  versioning, localization, and a media pipeline (upload, transform,
  library), all behind a stable REST + GraphQL API.
- **Visual builder** — compose pages by dragging and dropping blocks into
  layouts, with live preview, reusable presets, and starter bundles (a
  full starter site: pages, sample content, and a theme, ready to
  customize).
- **Extensible by design** — a sandboxed plugin system (WASM and RPC
  runtimes) with capability-scoped permissions and install-time consent, so
  third-party extensions can be installed safely without trusting the
  extension author with more than they explicitly declare.
- **First-party capabilities, out of the box**:
  - **Commerce** — products, orders, checkout, payment events.
  - **Membership** — tiers, subscriptions, content gating, recurring
    billing.
  - **SEO** — content-derived meta tags, sitemaps, JSON-LD structured data.
  - **Notifications** — event-driven, pluggable delivery.
  - **AI** — text generation, embeddings, and classification behind a
    provider-agnostic adapter (Claude, OpenAI, Google Gemini, or any
    self-hosted OpenAI-compatible model like Ollama) — swap providers
    without touching your integration.
- **AI-assisted authoring** — describe the section you want in plain
  language and the builder proposes a real, validated page fragment (never
  opaque markup) that you preview and accept like any other import.
- **Marketplace-ready distribution** — signed packages and offline
  verifiable entitlement tokens, so licensed extensions/themes work even in
  air-gapped or offline deployments.
- **Built for production** — CSRF protection, MFA, OAuth login, role-based
  permissions, per-endpoint rate limiting, and a real security boundary
  between the platform and anything it extends with.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/glyphux/glyphux/main/scripts/release/install.sh | sh
glyphuxd
```

Installs the latest release's `glyphuxd` (the server) and `glyphux` (an
optional developer CLI) to `~/.local/bin` — override with `INSTALL_DIR`, or
pin a version with `GLYPHUX_VERSION=vX.Y.Z`. Prebuilt archives for Linux,
macOS (amd64/arm64), and Windows are on the
[Releases page](https://github.com/glyphux/glyphux/releases), each with a
`SHA256SUMS` file the install script verifies before extracting.

Then run `glyphuxd` and open the address it prints (default
`http://localhost:8080`) — a first-run setup wizard walks you through
creating your admin account; after that it locks itself permanently.

### Build from source

```sh
go build -o glyphuxd ./cmd/glyphuxd
./glyphuxd
```

Requires Go 1.26+. The admin/builder UI ships pre-built and embedded in
the binary (`internal/adminui/dist/`); to rebuild it from source you'll
also need Node.js — see `scripts/release/build.sh`.

## Configuration

Glyphux is configured via environment variables (or a JSON config file
passed with `-config`):

| Variable | Default | Purpose |
|---|---|---|
| `GLYPHUX_ADDR` | `:8080` | listen address |
| `GLYPHUX_DATA_DIR` | `data/` | where SQLite, uploaded media, etc. live |
| `GLYPHUX_DB_DRIVER` | `sqlite` | `sqlite` (embedded) or `postgres` |
| `GLYPHUX_DB_DSN` | — | connection string, required for `postgres` |
| `GLYPHUX_DB_MAX_OPEN_CONNS` | `20` | Postgres pool sizing |
| `GLYPHUX_DB_MAX_IDLE_CONNS` | `5` | Postgres pool sizing |
| `GLYPHUX_DB_CONN_MAX_LIFETIME` | `30m` | Postgres pool sizing (Go duration) |
| `GLYPHUX_OAUTH_GITHUB_CLIENT_ID`/`_SECRET` | — | enables GitHub OAuth login |

On a remote (non-localhost) first boot, the setup wizard requires a token
printed to the log at startup, so a stranger who reaches the port before
you can't complete setup themselves.

## API and SDKs

Every domain (content, media, identity, layout, presets, the capabilities
above) is exposed over REST and GraphQL, both built on the same typed
composition contract — nothing is admin-UI-only. A typed JS/TS client SDK
(`sdk-js/`, published as `@glyphux/sdk`) wraps the REST API and is the same
SDK the admin UI itself uses — it holds no privileged access beyond what
any third-party integration gets.

```sh
go build -o glyphux ./cmd/glyphux
glyphux composition validate composition.json   # optional dev CLI
```

## Repository layout

```
cmd/glyphuxd/       the server daemon (primary entrypoint)
cmd/glyphux/        optional developer CLI
pkg/contract/       the public composition contract types + validator
pkg/sdk/            the plugin contract: Manifest, HostAPI, Plugin
pkg/blocks/         block definitions, importable by plugin/theme authors
pkg/theme/          the read-only theme rendering contract
pkg/compat/         import-time compatibility checking for presets/bundles
pkg/runtime/wasm/   sandboxed WASM plugin runtime (Wazero)
pkg/runtime/rpc/    sandboxed RPC plugin runtime (gRPC)
internal/           core engine: content, identity, db, layout, presets, setup wizard, http
capabilities/       first-party plugins: notifications, seo, commerce, membership, marketplace, forms, ai
themes/             first-party themes: headless, starter
blocks/firstparty/  first-party block implementations
admin-ui/           the admin/builder single-page app (React + TS), embedded into glyphuxd
sdk-js/             the public, typed JS/TS client SDK
scripts/release/    release build + install scripts
docs/               ticket specs and per-slice design notes
```

## Contributing

Contributions are welcome. The workflow:

1. Fork the repo and branch off `dev` (not `main`).
2. Make your change with tests — `go build ./... && go vet ./... && go test -race ./...` (plus `npm run build && npm test` under `admin-ui/` or `sdk-js/` if you touched either) should be green.
3. Open a pull request against `dev`. Every change goes through review before merging.

## License

Apache-2.0 (core).
