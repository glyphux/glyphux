# ADR-0001: Repository boundary — separate glyphux/registry/installer repositories

## Status

Accepted. Owner decisions recorded from `docs/mono-multi-repo-parens.md` (the
"do now" list): record the repository-boundary ADR, implement
`pkg/packagefmt`, make trust configuration key-ID aware, finish T6/T7/T8, and
create registry/installer repositories when work begins.

The owner note suggested recording this under `docs/decisions/registry-boundary.md`;
per `docs/agents/domain.md` and `AGENTS.md` the repo convention is `docs/adr/` at the
root, so this is the first ADR there: `docs/adr/0001-repository-boundary.md`.

## Context

The repo is a single Go module with a public root layout that contributors
already consume:

```text
cmd/  internal/  pkg/  capabilities/  themes/  admin-ui/  sdk-js/  scripts/
```

A prior agent proposal restructured core into a nested monorepo (a container
`glyphux/` repo holding `glyphux/`, `registry/`, `installer/` behind one
`go.work`). The owner rejected that: it creates churn without solving a real
dependency, and `go:embed`, the `admin-ui` ⇄ `sdk-js` local dependency
(`file:../sdk-js`), and one-PR-per-change all keep working as long as tightly
coupled components stay in core.

Two future applications — a registry/marketplace service
(`docs/glyphux-installer-update.md`) and OS installers
(`docs/one-click-installer.md`, `docs/OS-installer.md`) — are on the horizon.
They deploy, sign, release, and operate independently of core. T8 (marketplace
HTTP/UI surface, `docs/specs/phase5-gap-closure-spec.md` Ticket T8, in-flight
note `docs/implementation/active/0044-phase5-marketplace-surface.md`) installs
packages, which makes the package-format seam relevant now. The `internal/`
wall already says core internals are not public API; repo separation is the
organizational expression of that wall.

## Decision

### 1. Separate GitHub repositories, not a nested monorepo

```text
github.com/glyphux/glyphux       core (this repo, unchanged root layout)
github.com/glyphux/registry      registry/marketplace service (future)
github.com/glyphux/installer     OS packaging + launcher (future)
github.com/glyphux/docs          public docs website (optional, future)
```

- No parent container repo; no content-identical "move core under `/glyphux`"
  commit. Core keeps its public root layout (`cmd/`, `internal/`, `pkg/`,
  `capabilities/`, `themes/`, `admin-ui/`, `sdk-js/`, `scripts/`).
- `admin-ui`, `sdk-js`, `capabilities`, `themes`, and `pkg/packagefmt` stay in
  core — their coupling does not require registry/installer to live in the
  same repository.

### 2. Go modules

- `glyphux/glyphux`: one primary Go module (no multi-module core — separate
  dependency graphs, tags, Dependabot/Renovate behavior, CI combinations, and
  cross-module `replace` directives buy nothing today).
- `glyphux/registry`: its own module only when Go code exists — do not add an
  empty `go.mod` as an artificial implementation commitment.
- `glyphux/installer`: no Go module unless launcher/provisioning code requires it.
- No committed organization-wide `go.work`. A contributor may keep a private
  local workspace across clones (`~/src/glyphux/go.work`), but it is not part
  of the repository architecture.

### 3. Core owns the platform and daemon behavior

Core keeps `glyphuxd` (`cmd/glyphuxd`), the CLI (`cmd/glyphux`), `admin-ui`,
`sdk-js`, `capabilities`, `themes`, `pkg/packagefmt`, and the platform
behaviors in `internal/service`, `internal/update`, `internal/instance` —
including stable CLI commands:

```bash
glyphux service install
glyphux service uninstall
glyphux service status
glyphux instance open
```

### 4. Registry: no server this round

No deployable registry server this round (Phase 2+). T8 stays an
embedded/local catalog (embedded sample JSON + optional operator file-path
override, pre-loaded at boot) shaped as if it were fetched from a registry
later, but remaining local. The boundary is recorded here. Whether to reserve
`glyphux/registry` now (README/LICENSE/SECURITY/docs/architecture.md only) or
defer creation until implementation starts is an open item (see below).

### 5. Installer repo owns OS packaging and the launcher

`glyphux/installer` owns:

```text
windows/{wix, innosetup, powershell}
macos/{app, dmg, signing, notarization}
linux/{appimage, deb, rpm, desktop}
```

plus the minimal desktop launcher, preferred at `installer/launcher/` (job:
locate `glyphuxd` → start process → poll health → open browser). A temporary
`cmd/glyphux-launcher` in core is acceptable only as a deliberate, recorded
exception (it adds another official core binary and release surface). The
installer consumes released artifacts — `glyphuxd.exe`, `glyphux.exe`,
checksums, version metadata — not core source. If a future Wails launcher
needs shared types, expose a tiny stable protocol rather than importing
`internal/*`.

### 6. Boundary: released contracts and artifacts only

Registry and installer consume released contracts/artifacts — published
package-format definitions, JSON schemas, signing/verification specifications,
compatibility metadata, and (once the API stabilizes) possibly a small public
Go library. Neither ever imports core `internal/*`. The `internal/` wall is
respected by repo separation.

### 7. Key-ID-aware trust configuration (replaces scalar `marketplace.public_key`)

Replace the scalar `marketplace.public_key` with structured, key-ID-aware
`trusted_keys` records:

```json
{
  "trusted_keys": [
    {
      "id": "glyphux-packages-prod-2026-01",
      "algorithm": "ed25519",
      "public_key": "...",
      "purpose": ["package", "catalog", "revocation"],
      "issuer": "glyphux",
      "status": "active",
      "not_before": "2026-08-01T00:00:00Z",
      "not_after": null
    }
  ]
}
```

- **Key classes:** official Glyphux package roots, publisher keys,
  registry catalog-signing keys, entitlement-signing keys, development keys,
  organization/private-registry roots.
- **Statuses:** `active`, `deprecated`, `revoked`, `expired`.
- **Purpose separation:** `package-signing`, `catalog-signing`,
  `entitlement-signing`, `revocation-signing`. A package key is not
  automatically trusted to sign revocation metadata.
- **Operator keys are additive by default** (official roots + operator roots +
  private registry roots). A full-replacement mode exists only as explicit
  advanced configuration: `trust_mode = "custom-only"`.
- **Production never implicitly trusts the dev root.** Best: dev builds embed
  the dev public key under `//go:build dev`; acceptable: production embeds it
  with `status: "disabled"`, trusted only after explicit developer-mode
  activation with a warning.
- **Key custody:** root package keys stay offline, org-custodied, used rarely
  to sign intermediates or trust metadata, never in CI secrets. Automated
  official releases use separate intermediate operational keys via
  HSM/KMS-backed signing (cloud KMS, HSM, sigstore-style keyless, or a managed
  signing service). CI may request signatures using short-lived identity but
  never receives exportable private-key bytes. Authenticode uses a
  hardware/cloud code-signing provider (no `.pfx` + password in GitHub
  Secrets; OIDC or short-lived credential; restricted to protected
  tags/environments). Apple Developer ID, notarization credentials, and App
  Store Connect API keys use secure automation, not raw files in CI.
- **Revocation limitations recorded:** an offline instance cannot learn
  instantly that a key was revoked. Mechanisms: signed trust metadata, signed
  revocation list, catalog refresh, package update check, manual trust-bundle
  import. Each trust record carries key ID, validity period, status,
  sequence/version, and signed issuer metadata. Revocation does not
  auto-uninstall already-running packages; policy severity levels
  (compromised key, malicious package, publisher dispute, expired publisher
  certificate) decide quarantine vs. advisory. An expired key does not
  retroactively invalidate packages validly signed during its validity —
  that requires trusted timestamps or signed publication metadata (open item).

### 8. Package format: deterministic ZIP containers now; `SignedPackage` stays internal

`.gxp` / `.gxt` / `.gxb` are deterministic ZIP containers **now — with T8,
not T10** (T8 owns package ingestion; T10 consumes the format, it does not
establish it). `SignedPackage` is the verified **in-memory/internal**
representation, explicitly **not** a JSON distribution format:

```go
type SignedPackage struct {
    Manifest   Manifest
    Files      []PackageFile
    Checksums  map[string]string
    Signatures []Signature
}
```

```text
.gxp/.gxt/.gxb archive
        ↓
pkg/packagefmt decode/verify
        ↓
SignedPackage
        ↓
transactional install
```

Do not implement "install SignedPackage JSON" in T8 and wrap it in ZIP later —
that would make the wrong representation observable in tests, APIs, persisted
state, admin upload behavior, fixtures, and documentation. v1 container scope:

```text
forms.gxp
├── manifest.json
├── payload/plugin.wasm
├── checksums.json
└── signatures/package.sig
```

Themes and bundles use the same envelope with different package types. Schema
versions are independent of repository versions (`glyphux-package/v1`,
`glyphux-plugin/v1`, `glyphux-theme/v1`, `glyphux-bundle/v1`); core `v1.2.0`
does not imply package format v1.2. `pkg/packagefmt` lands with T8.

### 9. Versioning and promotion

Each repository uses independent ordinary `v*` SemVer tags — no `registry-v*`
prefix (that prefix is only needed inside a monorepo with independently
versioned products):

```text
glyphux/glyphux@v1.2.0
glyphux/registry@v0.3.0
glyphux/installer@v0.5.0
```

Promotion is per-repo (`dev` → `staging` → `main`) with per-slice branch
hygiene.

### 10. Docs split

- `glyphux/docs` (separate, optional repo) = public docs website.
- `glyphux/glyphux/docs` = internal ADRs, specs, implementation notes, agent
  docs (`docs/adr/`, `docs/specs/`, `docs/implementation/`, `docs/agents/`).
- Existing repo-root `docs/` stays in core.

## Consequences

- Registry incidents do not block core releases; registry and installer have
  independent release schedules and security boundaries.
- Installer signing changes (Authenticode, Apple, package keys) do not trigger
  core test pipelines.
- GitHub permissions can later differ per repo (infrastructure, release, core
  teams).
- Cross-repo changes become multiple PRs by design — accepted, because there
  is no proven atomic-change requirement between core, registry, and installer.
- Contributors working across repos need a private local `go.work`.
- `go:embed`, the `admin-ui` ⇄ `sdk-js` local dependency, and one-PR-per-change
  inside core are preserved.
- Nothing moves during T6/T7/T8; the only eventual moves are
  `scripts/release/{windows,macos,linux}/*` → `glyphux/installer/...`, and
  only once native packaging code exists.

## Open items (TBD)

- **Registry repo: reserve now vs. defer.** Option A: create only when
  implementation starts. Option B: reserve `glyphux/registry` with
  README/LICENSE/SECURITY/docs/architecture.md — but no empty `go.mod`.
- **Registry hosting scope:** self-hosted only vs. third-party hostable.
- **Launcher v1 home:** `installer/launcher/` (preferred) vs. temporary
  `cmd/glyphux-launcher` in core.
- **Docs website repo (`glyphux/docs`) timing.**
- **`pkg/packagefmt` scoping:** T8-scope-expansion vs. T8a.
- **Signed publication metadata vs. RFC3166 timestamps** (needed so an expired
  key does not retroactively invalidate validly signed packages).
- **`capabilities/marketplace` crypto split/rename timing** — deferred; use
  neutral `pkg/` seams (`pkg/packageauth`, `pkg/trust`, `pkg/entitlement`) for
  new types, and do not conflate the Glyphux package marketplace with the
  marketplace capability for applications built on Glyphux.

## References

- `docs/mono-multi-repo-parens.md` — owner decision note this ADR records.
- `docs/one-click-installer.md` — single-user launcher first; separate
  dev/prod signing.
- `docs/OS-installer.md` — Linux/macOS one-click installers; shared launcher
  responsibilities.
- `docs/glyphux-installer-update.md` — registry/marketplace service and
  graphical Windows installer motivation.
- `docs/specs/phase5-gap-closure-spec.md` — Ticket T8 (marketplace HTTP/UI
  surface), which owns package ingestion and lands `pkg/packagefmt`.
- `docs/implementation/active/0044-phase5-marketplace-surface.md` — T8
  in-flight implementation note (embedded/local catalog, no remote server).
