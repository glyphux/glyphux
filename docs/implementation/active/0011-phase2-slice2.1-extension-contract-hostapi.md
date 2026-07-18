# Implementation: Phase 2 slice 2.1 — unified extension contract + HostAPI

## Goal

PRD §8.3 ("The Unified Extension Contract") and §7.3 ("Two-Axis Manifest")
require a public `Plugin`/`HostAPI` surface in `pkg/sdk`: capability-gated,
scoped domain APIs, so that no plugin/theme code ever needs kernel
internals. This is the foundation slice for the rest of Phase 2 (event bus,
capability registry, WASM/RPC runtimes, permissions, consent engine all
build on top of this contract) — it needs to be solid and match the PRD's
documented shape before anything else starts.

## Owning Contexts

- (no CONTEXT.md exists yet in this repo — see docs/agents/domain.md; this
  is core-kernel work: new package `pkg/sdk`)

## Status

In progress (implemented and verified in this session; not yet PR'd to
`staging` — that happens once more of Phase 2 lands, per the established
"dev → PR → staging" convention).

## Current Decisions

- **Manifest is a plain struct with a `Validate() error` method**, not a
  YAML/JSON-parsing type — parsing a manifest file format is a later
  concern (likely the capability registry, slice 2.3, or the loader tiers,
  2.4/2.5); this slice only defines and validates the in-memory shape.
- **Two axes kept as genuinely separate fields** (`API []APIScope`,
  `Permissions []Permission`), per §7.3's explicit instruction that they
  "carry different trust models, enforcement points, and review criteria."
  `APIScope{Capability, Scopes}` and `Permission{Name, Args}` — scopable,
  not boolean, from v1 (§7.4).
- **`sdk`'s API-scope model is deliberately separate from
  `internal/permission.Capability`.** They answer different questions:
  `internal/permission` gates *which end-user role* may call a kernel
  domain API through a transport; `pkg/sdk`'s API/Permission axes gate
  *which capabilities a plugin PACKAGE itself* was granted, independent of
  which user is acting through it. Conflating them would have made a
  plugin's own trust boundary depend on the role of whoever happens to be
  using it, which is not what the PRD describes.
- **`RuntimeTier` has three values**: `inprocess` (Tier A), `wasm` (Tier B),
  `rpc` (Tier C) — `inprocess` added beyond the PRD's own `runtime: rpc |
  wasm` example specifically so this slice's own real, testable HostAPI
  implementation (see below) has a tier to declare itself under; the PRD is
  explicit that Tier A is compiled-in/first-party-only, so nothing enforces
  that restriction yet (there is no loader to restrict) — that belongs to
  whichever slice adds the WASM/RPC loaders and decides how Tier A plugins
  are actually registered into a running daemon.
- **`Requires.Core` is validated as a constraint string** (`>=`, `<=`, `==`,
  `>`, `<` prefix, or an exact version with no prefix), using
  `golang.org/x/mod/semver` (already an indirect dependency via gqlgen's
  toolchain — promoted to direct in `go.mod` by this slice) rather than
  hand-rolling semver parsing or adding a new constraint-parsing
  dependency. Actually *checking* the constraint against the running
  kernel's version is the capability registry's job (slice 2.3); this only
  validates the constraint's own shape.
- **HostAPI's real implementation is built now, backed by the actual
  kernel domain APIs** (`internal/content`, `internal/composition`,
  `internal/media`, `internal/identity`), not stubbed — per this slice's
  brief, so later slices extend a real foundation instead of rewriting one.
  Every Tier-A HostAPI call runs as a fixed internal `hostPrincipal`
  (`permission.Principal{Role: RoleAdmin}`): Tier A is first-party/fully
  trusted (§8.1), so the kernel's own per-user-role check is satisfied by
  construction — the plugin's actual gate is its own manifest's declared
  scopes, checked by `hostAPI` itself *before* the call ever reaches the
  kernel API. This means every scoped method has two checks in sequence,
  same defense-in-depth shape as slice 0006 established at the
  content/composition/media boundary.
- **Capability gating returns `nil` for a whole surface, an error for a
  single denied call**: `Content()`/`Users()`/`Media()` return `nil` if the
  manifest declared no capability at all for that domain (matching §8.3's
  "not present in the HostAPI surface"); if the capability *is* declared
  but a specific method needs a scope the manifest didn't grant (e.g.
  `content: [read]` only, then `Create` is called), that individual method
  call returns `ErrScopeNotDeclared` rather than the whole surface being
  absent — a plugin with partial scopes still gets a real, partially-usable
  API, not an all-or-nothing nil.
- **`RegisterContentType` gated on `content:write`; `RegisterBlock` gated on
  any declared `content` capability at all (read is enough) — a judgment
  call.** The PRD sketches both under "Composition / content domain" in the
  same interface without specifying separate scopes for them. Content-type
  registration is a structural schema change with exactly the same
  blast radius as `content:write` item mutations (both go through
  `composition.Store`'s capability-checked `DefineContentType`), so it was
  given the same gate. Block registration (Phase 4's extension point) is a
  lighter declarative registration with no Phase-4 rendering/slot mechanics
  built yet, so it was given the lighter gate. Revisit when Phase 4 defines
  what a block registration actually does.
- **`Store()` (scoped KV) is always present, ungated** — per §8.3 it "carries
  no api capability of its own to declare." Implemented as a real,
  namespaced-per-plugin in-memory backend (`MemoryKVBackend`), proven
  genuinely namespaced (not just "two separate Go maps") by a test that
  constructs two different plugins' `HostAPI`s from the *same*
  `KernelDeps.KV` backend and asserts one plugin cannot see another's keys.
  **Deferred: persistence.** This backend is process-lifetime only — a
  real DB-backed scoped-KV table is a nontrivial addition (a new migration,
  and this session already hit two real migration-version collisions this
  week from independently-numbered slices; adding one more migration inside
  this already-large foundation slice was judged not worth the risk). The
  `ScopedKV` interface (`Get`/`Set`/`Delete`, never a raw handle) is the
  contract shape later slices should persist behind, not replace.
- **`On`/`Emit` are a minimal in-process stub, deliberately ungated.** The
  real event bus (subscribe/emit semantics across plugins, ordering
  guarantees, per-event capability requirements per §8.4 — "subscribing to
  a sensitive event requires the corresponding capability scope") is slice
  2.2's entire job. This slice only established the interface shape
  (`On(event string, handler EventHandler) error`, `Emit(ctx, event,
  payload) error`) with a same-process, registration-order dispatch, so
  2.2 has a real shape to extend rather than invent from scratch.
- **`UsersAPI`/`MediaAPI` mirror `internal/identity`/`internal/media`'s own
  method shapes** (`List`, `UpdateRole`/`Deactivate`/`Reactivate` for users;
  `Get`/`List`/`Upload`/`Delete` for media), gated `read` vs `manage`
  (users) / `read` vs `write` (media) — the same read/write-grade split
  `internal/permission.Capability` already uses, kept consistent rather
  than inventing new scope vocabulary per domain.

## Open Questions — resolved

- **Should `pkg/sdk` import `internal/*` packages directly, or should it
  talk over some other seam?** Resolved: yes, direct import. Go's
  `internal/` visibility rule only restricts imports from *outside* the
  module tree rooted one level above `internal` — `pkg/sdk` shares
  `github.com/glyphux/glyphux`'s module root with every `internal/*`
  package, exactly like `internal/api`, `internal/graphql`, and `pkg/client`
  already do. `pkg/sdk` is the sanctioned bridge a plugin's *own* code never
  crosses (a plugin only ever sees the `HostAPI` interface, never an
  `internal/content.API` value) — this is different from and does not
  weaken the "plugins/themes only import pkg/sdk and pkg/contract" rule
  (§12's repo-structure decision) that this slice's tests were checked
  against.

## Files/Modules Changed

- `pkg/sdk/manifest.go` (new) — `Manifest`, `RuntimeTier` (+ 3 constants),
  `Requires`, `APIScope`, `Permission`, `Validate() error`,
  `ErrInvalidManifest`.
- `pkg/sdk/manifest_test.go` (new) — 17 tests covering name/version/runtime/
  requires/API-scope/permission validation, both accept and reject cases.
- `pkg/sdk/host.go` (new) — `HostAPI` interface, `hostAPI` implementation,
  `KernelDeps`, `ContentAPI`/`UsersAPI`/`MediaAPI` (scoped wrappers),
  `ScopedKV`/`MemoryKVBackend`, `AdminPageDef`/`JobDef`/`BlockDef`,
  `EventHandler`, `NewHostAPI(manifest, deps) (HostAPI, error)`,
  `ErrScopeNotDeclared`.
- `pkg/sdk/host_test.go` (new) — 26 tests covering every HostAPI method's
  gating behavior, backed by real `internal/content`/`internal/composition`/
  `internal/media`/`internal/identity` instances over a fresh SQLite DB
  (no mocks), including a namespacing test with two plugins sharing one
  `MemoryKVBackend`.
- `go.mod`/`go.sum` — `golang.org/x/mod` promoted from indirect to direct
  (now imported directly by `pkg/sdk/manifest.go`); `go mod tidy` also
  corrected `github.com/microcosm-cc/bluemonday` (used directly by slice
  0008's `internal/content/sanitize.go`, previously mis-marked indirect).

## Acceptance Criteria

- [x] `Manifest.Validate()` rejects malformed `requires`/`api`/`permissions`
      and accepts well-formed ones — provable by `pkg/sdk/manifest_test.go`'s
      17 tests (empty name/version, malformed version, unknown runtime,
      malformed `requires.core`, empty `requires.contract`, unknown/duplicate
      API capability, unknown scope, capability with no scopes, unknown/
      forbidden permission, network permission with no allowlist, non-network
      permission carrying args, duplicate permission).
- [x] A plugin that declares no `content` (or `users`, or `media`) capability
      gets `nil` from the corresponding `HostAPI` method — verified by
      `TestHostAPIContentIsNilWithoutDeclaredCapability`,
      `TestHostAPIUsersIsNilWithoutDeclaredCapability`,
      `TestHostAPIMediaIsNilWithoutDeclaredCapability`.
- [x] A plugin declaring a read-only scope can read but a write/manage/
      publish call on that same surface is denied — verified by
      `TestHostAPIContentWriteDeniedWhenOnlyReadDeclared`,
      `TestHostAPIUsersManageDeniedWhenOnlyReadDeclared`,
      `TestHostAPIMediaWriteDeniedWhenOnlyReadDeclared`, each paired with a
      "works when declared" test proving the positive case isn't
      accidentally also broken.
- [x] `RegisterAdminPage`/`RegisterJob` are denied without their respective
      `admin_ui`/`scheduled_jobs` permission and succeed with it —
      `TestHostAPIRegisterAdminPageDeniedWithoutPermission` /
      `...WorksWithPermission`, `TestHostAPIRegisterJobDeniedWithoutPermission`
      / `...WorksWithPermission`.
- [x] `RegisterContentType` is denied without `content:write` and, when
      allowed, really lands in the composition (not a no-op success) —
      verified by loading the composition back via `Compositions.Load` and
      asserting the new type is present, not just checking `err == nil`.
- [x] `Store()` is always present regardless of declared capabilities, real
      values round-trip through `Set`/`Get`/`Delete`, and two different
      plugins built from the same shared backend cannot see each other's
      keys — `TestHostAPIStoreRoundTripsAValue`,
      `TestHostAPIStoreDeleteRemovesAValue`,
      `TestHostAPIStoreIsNamespacedPerPlugin`.
- [x] `go build ./...`, `go vet ./...`, and `go test -race ./...` all green
      across the whole repo (18 testable packages, all `ok`), plus `go mod
      tidy` run clean with no unexpected diff beyond the two direct-dependency
      corrections noted above.

## Risks

- The read/write/manage/publish scope vocabulary chosen here for
  `content`/`users`/`media`/`events` is this slice's own judgment call, not
  dictated verbatim by the PRD's necessarily-abbreviated §7.3 example. If
  slice 2.3 (capability registry) or 2.9 (dogfooding `forms`) need a
  different/finer scope shape, expect a follow-up revision to
  `knownAPIScopes` — it is a single map literal, not spread across the
  codebase, specifically so that revision stays cheap.
- `On`/`Emit`'s minimal same-process dispatch is *not* what slice 2.2 will
  ship (no cross-plugin bus, no ordering guarantees, no capability
  requirements on subscribing to sensitive events per §8.4) — it exists
  only so the interface has a real, testable body today. Expect 2.2 to
  replace the internals of `On`/`Emit` while keeping the `HostAPI` method
  signatures stable (that stability is the actual point of building this
  now).
- `MemoryKVBackend`'s lack of persistence is a known, explicit gap (see
  Current Decisions) — a plugin's stored state does not survive a daemon
  restart. Not acceptable for production plugins; deferred deliberately
  rather than rushed into this slice.
