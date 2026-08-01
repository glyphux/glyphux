# Progress Handoff — Glyphux Codebase Exploration

**Date:** 2025-07-21
**Session type:** End-to-end codebase exploration (read-only; no code changes made)
**Branches:** working on `dev` (current checkout); `main`, `staging`, and many stale `worktree-agent-*` branches also exist.

## What this session did

Explored the glyphux codebase end-to-end with an explicit instruction to **not trust docs/comments** — every claim below was verified against the actual implementations and by running the full test suites. No files were modified.

## Verification results (all green)

- `go build ./...` and `go vet ./...` — clean
- `go test -race ./...` — **every package passes** (notable: `internal/api` ~190s, `internal/graphql` ~67s of race-tested integration tests)
- `sdk-js`: `npm run build` clean, **66 tests pass**
- `admin-ui`: `npm run build` clean, **119 tests pass** (vite reports one large-chunk warning, non-blocking)

## Verified architecture summary

Reference points for the full picture: `README.md` (product/market doc — treat as aspirational, not implementation truth), `go.mod` (dependency reality), `docs/implementation/completed/` (36 slice notes; `active/` is currently **empty**), `docs/specs/phase3-4-spec.md`.

### Core model — two-layer typed contract (`pkg/contract`)
- Layer 1: `contract.Composition` (`content-composition/v0`) — content types/fields; stored as one JSON row with optimistic-concurrency CAS (`internal/composition/store.go`, max 50 retries).
- Layer 2: `contract.Layout` (`layout-composition/v1`) — regions → blocks → nested slots per route.
- Artifacts: `CompositionPreset` (`composition-preset/v1`), `CompositionBundle` (`composition-bundle/v1`) = pages + presets + sample content + theme ref.
- `contract.WalkBlocks` is the single shared block-tree walk (used by structural validation, `pkg/blocks.ValidateLayout`, `pkg/compat`).

### Verified mechanisms
- `internal/db`: `?`→`$n` rebind as portability choke point; `Queryer` interface (transaction-agnostic stores); global ordered idempotent migrations (duplicate versions rejected); SQLite = single-conn WAL, Postgres = pooled (pgx).
- `internal/permission`: fixed role matrix (admin/editor/viewer → capabilities incl. `content_types:manage`, `layouts:manage`, `presets:manage`); `Principal{Role}` keeps domain packages decoupled from identity; enforced at BOTH transport and domain-API boundary (defense-in-depth).
- `internal/content`: JSON items validated per declared type; draft/published gating (drafts need `content:read_drafts`); version snapshots, rollback, localization with fallback, relation integrity, richtext sanitization.
- `internal/identity`: PBKDF2-SHA256 @ 600k iters, constant-time compare, sessions, TOTP MFA, GitHub OAuth (opt-in), deactivation.
- `internal/setup` + `internal/bootstrap`: atomic admin+composition creation; token-gated remote setup; `Gateway` swaps the full handler in-process after wizard Postgres selection (no restart).
- HTTP surface (`internal/server`, `internal/api`): Go 1.22 ServeMux routing; CORS (explicit allowlist, none by default), CSRF double-submit, 300 req/min general rate limit (health/ready exempt), 1 MiB body cap (media exempt), security headers; REST `/api/v0/*` + GraphQL (gqlgen, mirrors REST over same domain APIs) + admin SPA `/admin/` + wizard `/setup`.

### Extension system — built and tested, but NOT wired into the daemon
This is the single most important finding for any future work on plugins:

- `pkg/sdk`: `Manifest` (API scopes + Permissions axes); `NewHostAPI` enforces `requires.core` vs `pkg/kernel.Version` (`0.2.0`) and `requires.contract`; `HostAPI` methods return nil / deny unless the scope was declared; sensitive-event gating on `On` (`user.created`→`users:read`, payments/membership events→their capabilities); shared `EventBus`, in-memory namespaced KV, audit logging.
- `pkg/runtime/wasm`: Wazero, **per-instance private engines** (strict isolation); deny-by-default via absent host functions (link error); no fuel/memory caps yet.
- `pkg/runtime/rpc`: gRPC over Unix socket; subprocess supervision with `GLYPHUX_RPC_PLUGIN_READY` handshake (no polling); crash isolation.
- `internal/consent`: SHA-256 fingerprint of canonicalized API+Permissions (stale-consent detection); partial consent; `ErrGrantExceedsRequest` (cannot grant more than requested).
- **Import analysis (ground truth):** nothing outside `capabilities/` + the runtimes' own tests imports `pkg/runtime` or `internal/consent`. `cmd/glyphuxd` wires content/composition/identity/media/layout/preset/bundle/graphql/blocks-firstparty/themes-starter + `pkg/compat` (via preset/bundle stores) — and **no plugin loader, no consent check, no capability registration, and no `WithAI`** (the AI endpoint exists in `internal/api/ai.go` but `cmd/glyphuxd` never enables it).

### Capabilities (`capabilities/*`) — standalone `sdk.Plugin` implementations
- commerce: real `stripe-go/v81` against fake-gateway test server; real webhook signature verification.
- ai: provider-agnostic `Adapter` (Claude/OpenAI/Gemini); 4-step gate per call (scope → `AllowsNetworkHost` → rate limit → adapter).
- marketplace: Ed25519-signed packages + offline-verifiable entitlement tokens (test statically proves no networking imports).
- notifications (mailer adapter), seo, membership (content gating + billing), forms (dogfood on public SDK only).

### Other verified pieces
- `pkg/theme`: `CompositionView` has unexported fields + read-only accessors — themes structurally cannot mutate composition; `themes/headless` (JSON), `themes/starter` (HTML, used by live preview `POST /api/v0/layouts/preview`).
- `blocks/firstparty`: heading/paragraph/image/container — pure declarations, deliberately logic-free.
- `pkg/compat`: preset/bundle compatibility vs live registry + theme regions; used by preset/bundle Save and AI compose.
- `internal/boundary`: **`go/types` static analysis** (not text greps) proves domain packages never leak raw `*sql.DB`/`*os.File` through exported signatures — runs as part of the test suite.
- `admin-ui`: React 19 + TS + Vite + Tailwind 4, @craftjs/core builder, Radix UI; embedded via go:embed under `/admin`.
- `sdk-js`: typed client over `/api/v0`, used by admin-ui itself; 10 resources (auth, content, content-types, media, users, blocks, layouts, presets, bundles, ai).

## Known gaps / deferred items (honest end-to-end state)

1. **No plugin loader** wires the WASM/RPC runtimes into `glyphuxd` — nothing outside `capabilities/` and the runtimes' own tests imports `pkg/runtime`, so no plugin can actually be loaded today.
2. **Consent engine not wired** — `internal/consent.IsConsented` is never called by anything outside its own tests; the `ConsentChecker` seam in `pkg/runtime/wasm` is unused ("declared == consented" today). Also deferred per `internal/consent`'s own doc: rendering a consent-screen UI, and marketplace review (PRD §10.2 mechanism #1).
3. **Capabilities unregistered** — forms/seo/commerce/membership/notifications are standalone `sdk.Plugin` implementations not registered anywhere; the AI compose endpoint (`internal/api/ai.go`, `WithAI`) exists but `cmd/glyphuxd` never enables it (no operator-facing AI provider credential config exists in `internal/config`).
4. **Audit logging dormant + incomplete** — only fires if a `HostAPI` with `KernelDeps.Audit` is built (nothing in the daemon does); item-level Content/Users/Media CRUD auditing is also deferred (only boundary-gate + consent decisions are logged).
5. **`AllowsNetworkHost` is a decision primitive, not an enforcement point** — no outbound-network interception exists in the WASM host or RPC broker, so the `network` permission's allowlist is not yet actually enforced at call time.
6. **Plugin KV is process-lifetime** — `pkg/sdk`'s `ScopedKV` is backed by in-memory `MemoryKVBackend`; persistence to a real table is deferred.
7. **WASM sandbox lacks resource limits** — no fuel/gas metering, no explicit memory cap, no execution-timeout wiring (only what Wazero provides by default); the host-function ABI is hand-rolled (no formal WIT contract yet).
8. **Marketplace has no HTTP/UI surface** — `capabilities/marketplace` is only reachable via `internal/preset`/`internal/bundle` `InstallFromPackage` (verified: `internal/api` has zero marketplace references). `InstallFromPackage` deliberately skips the live-registry compat gate, and — by design per PRD §12.5 (see `internal/preset/marketplace.go`) — skips the entitlement check (entitlements gate updates, not first install). No marketplace server, catalog, or consent-screen UI exists.
9. **Process/build caveats** — `pkg/kernel.Version` is hand-bumped (no VERSION file / git-tag derivation); the admin SPA is pre-built and committed under `internal/adminui/dist/` (rebuilding requires Node — `go build` does not rebuild it); migration version numbers are manually kept globally unique across packages (this has already caused collisions twice, per `internal/consent/store.go`); repo has **12 stale `worktree-agent-*` branches** (hygiene).

## Suggested next steps (if continuing exploration/implementation)

- Decide whether the next session is (a) continue read-only exploration, (b) start the missing "plugin loader" slice, or (c) a specific bug/feature. The repo's own convention: check `docs/implementation/active/` first, follow `docs/agents/implementation-tracking.md`, branch off `dev`, and open a PR against `dev`.
- If starting a plugin-loader slice: the natural wiring point is `cmd/glyphuxd/main.go`'s `buildFullHandler` (where `firstparty.RegisterAll` and the stores are already assembled), plus a consent check before `NewHostAPI`, per `docs/agents/domain.md` and the boundary/import invariants in `internal/boundary/`.

## Suggested skills

- **code-review** — if the next session reviews any changes since a commit/PR.
- **tdd** — if implementing the plugin-loader slice or any feature/fix (repo convention is test-first; all suites run with `-race`).
- **diagnosing-bugs** — if a failure/regression is reported instead.
- **domain-modeling** — if touching the composition contract, layout contract, or plugin capabilities (keeps the single `CONTEXT.md`/ADR conventions consistent).
- **research** — if verifying a claim about the PRD/phase specs (treat `README.md` and docs as claims to verify against code, as done this session).
- **handoff** — for future session compaction.

## Sensitive material note

No secrets, API keys, or PII were encountered or recorded. Runtime-generated values (setup token, admin credentials at install time) are created fresh per install and are not present in the repo.
