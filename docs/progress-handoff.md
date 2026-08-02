# Progress Handoff — Phase 5 Close-out (9/9 gaps CLOSED)

**Branch:** `dev` @ `5531619` (Ticket T8 — the final Phase 5 commit). `main` and
`staging` exist; per-repo promotion is `dev` → `staging` → `main` (ADR-0001 §9).
**Phase 5 state:** CLOSED — all 9 gaps from the exploration handoff are landed,
tested, and recorded. Tracking notes 0037–0045 were moved from
`docs/implementation/active/` to `docs/implementation/completed/`.

## Phase 5 artifacts

- **Spec:** `docs/specs/phase5-gap-closure-spec.md` — Tickets T1–T9, staged
  T1/T2/T3/T5 → T4 → T6 → T7/T8 → T9.
- **ADR:** `docs/adr/0001-repository-boundary.md` — the repository-boundary
  decision (separate `glyphux/registry` / `glyphux/installer` repos, `.gxp/.gxt/.gxb`
  package format, key-ID-aware trust model). First ADR per `docs/agents/domain.md`.
- **Implemented with T8 (ADR-0001 §7–8):**
  - `pkg/packagefmt` — deterministic ZIP containers (`.gxp/.gxt/.gxb`,
    schema `glyphux-package/v1`), `SignedPackage` as the verified in-memory
    representation (not a distribution format), key-ID-aware signature
    verification; single `Decode` entry point → verified `SignedPackage` → install.
  - Key-ID-aware `marketplace.trusted_keys[]` config replacing the scalar
    `public_key`: structured records (id/algorithm/public_key/purpose[]/issuer/
    status/validity), additive trust by default, `trust_mode="custom-only"`
    full-replacement escape hatch, embedded dev root as the default anchor
    (era/prod root split deferred to T10 — a config-level change).

## Phase 5 gates — all 9 closed

| # | Gap (from the exploration handoff) | Ticket | Commit | Note |
|---|------------------------------------|--------|--------|------|
| 1 | Plugin KV was process-lifetime (in-memory `ScopedKV`) | T1 | `e894421` | 0037 |
| 2 | WASM sandbox lacked resource limits (fuel, memory cap, execution timeout) | T2 | `b7e127c` | 0038 |
| 3 | `AllowsNetworkHost` was a decision primitive, not enforced at call time | T3 | `43e5684` | 0039 |
| 4 | Consent engine not wired into the daemon; no consent-screen UI | T4 | `8d73a9b` | 0040 |
| 5 | Capabilities unregistered; AI compose not enabled / no provider config | T5 | `4d807a7` | 0041 |
| 6 | No plugin loader wired the WASM/RPC runtimes into `glyphuxd` | T6 | `e4a2fb6` | 0042 |
| 7 | Audit logging dormant; item-level CRUD auditing missing | T7 | `deb40c4` | 0043 |
| 8 | Marketplace had no HTTP/UI surface (catalog / install / entitlements) | T8 | `5531619` | 0044 |
| 9 | Process/build caveats (VERSION file, migration guard, admin dist freshness, branch hygiene) | T9 | `5f923cc` | 0045 |

**Evidence:** each tracking note (0037–0045, now under `docs/implementation/completed/`)
records the acceptance criteria and definition-of-done proven for its ticket in
`docs/specs/phase5-gap-closure-spec.md` (key proofs per ticket, e.g. T8: catalog
list/install/entitlements endpoints with auth + CSRF, tampered-package 422,
`requires.core` rejection, §12.5 by-design semantics preserved).

## Validation gates (green at close-out)

- `go build ./...` and `go vet ./...` — clean.
- `go test -race ./...` — all 43 packages pass.
- `internal/boundary` — boundary-verify static-analysis gate green (runs as part
  of the test suite).
- `sdk-js` — 72/72 tests pass (`npm run build` clean).
- `admin-ui` — 125/125 tests pass (`npm run build` clean).

## Verified architecture (reference — unchanged by Phase 5)

- Two-layer typed contract: `contract.Composition` (`content-composition/v0`) +
  `contract.Layout` (`layout-composition/v1`); artifacts `CompositionPreset`/
  `CompositionBundle`; single `contract.WalkBlocks` block-tree walk shared by
  validation, `pkg/blocks`, `pkg/compat`.
- `internal/db` `?`→`$n` rebind choke point; `Queryer` transaction-agnostic
  stores; global ordered idempotent migrations (duplicates rejected).
- `internal/permission` fixed role matrix enforced at transport + domain-API
  boundary; `internal/content` draft/published gating, snapshots, localization,
  relation integrity; `internal/identity` PBKDF2-SHA256 @ 600k, TOTP MFA,
  GitHub OAuth; `internal/setup`+`internal/bootstrap` atomic admin+composition
  creation with in-process handler swap.
- HTTP surface: Go 1.22 ServeMux, CORS allowlist, CSRF double-submit,
  300 req/min general limit, 1 MiB body cap, security headers; REST `/api/v0/*`
  + GraphQL + admin SPA `/admin/` + setup wizard.
- Extension system now **wired into the daemon** (was the #1 gap): plugin
  loader (T6), consent engine + consent-screen UI (T4), persistent SQL-backed
  plugin KV (T1), WASM resource limits (T2), network policy enforcement (T3),
  capability registration + AI config/enablement (T5), audit activation +
  item-level CRUD auditing (T7), marketplace HTTP/UI surface (T8), process/build
  caveats (T9). Details per note 0037–0045.

## Branch state

- `dev` @ `5531619` — Phase 5 head.
- `main` / `staging` exist; ADR-0001 §9 promotion is per-repo `dev` → `staging`
  → `main`.
- The 12 stale `worktree-agent-*` branch cleanup was in T9's scope (note 0045);
  confirm the current branch list with `git branch` before any new slice.

## Forward-looking — future work, NOT done

### T10 installer round (per `docs/one-click-installer.md` + `docs/OS-installer.md`, ADR-0001)
- **Separate repos** `glyphux/registry` and `glyphux/installer` (optional
  `glyphux/docs`) — **not created yet**; core keeps its root layout
  (`cmd/ internal/ pkg/ capabilities/ themes/ admin-ui/ sdk-js/ scripts/`), no
  nested monorepo, no content-identical move commit.
- **`.gxp/.gxt/.gxb` deterministic ZIP + `SignedPackage`** — **already landed**
  with T8 via `pkg/packagefmt`; T10 consumes the verified container rather than
  establishing it.
- **Key-ID trust config** — **already landed** with T8 (`trusted_keys[]`,
  additive default, `custom-only` escape hatch). Era/prod root split and root
  custody process (offline org-custodied roots, HSM/KMS-backed intermediate
  signing, short-lived CI identity) remain for the release pipeline.
- **Installer repo ownership** (per ADR-0001 §5): OS packaging
  (`windows/{wix,innosetup,powershell}`, `macos/{app,dmg,signing,notarization}`,
  `linux/{appimage,deb,rpm,desktop}`) + launcher at `installer/launcher/`
  (preferred; temporary `cmd/glyphux-launcher` in core only as a deliberate
  exception); consumes released artifacts, never core `internal/*`.
- **Core-owned daemon-behavior CLI** (`glyphux service install/uninstall/status`,
  `glyphux instance open`) is defined by ADR-0001 §3 but **not yet implemented** —
  the current CLI exposes `version` and `composition validate` only.

### Restructure decision (ADR-0001)
- Recorded only; nothing moved. Registry/installer repos are created when work
  begins, not before.

### Open owner decisions (TBD, recorded in ADR-0001)
- Registry repo: reserve-now vs defer.
- Registry hosting scope: self-hosted only vs third-party hostable.
- Docs website repo (`glyphux/docs`) timing.
- Also recorded there: launcher v1 home; `pkg/packagefmt` T8-scope vs T8a
  (resolved in practice — it landed with T8); signed publication metadata vs
  RFC3166 timestamps; `capabilities/marketplace` crypto split/rename timing
  (use neutral `pkg/` seams for new types).

### Registry / Phase 2+
- No registry server exists. T8's catalog is embedded/local (embedded sample
  JSON + optional operator file override, pre-loaded at boot) shaped for future
  remote sync. Remote catalog/registry server, update-fetch service, publish
  pipeline, and storefront remain explicit non-goals (T8 scope-out; note 0044) —
  that is registry Phase 2+.

## Suggested skills for the next session

- **tdd** — implementing the T10 installer round or any new slice (repo
  convention: test-first, all suites `-race`).
- **code-review** — reviewing the Phase 5 consolidation to `main`/`staging`.
- **domain-modeling** — if reopening ADR-0001 open items (registry boundary,
  trust model) or adding `CONTEXT.md` terms.
- **research** — verifying installer/registry decisions against
  `docs/one-click-installer.md`, `docs/OS-installer.md`, `docs/glyphux-installer-update.md`.
- **handoff** — for future session compaction.

## Sensitive material note

No secrets, API keys, or PII encountered or recorded. Runtime-generated values
(setup token, admin credentials at install time) are created fresh per install
and are not present in the repo.
