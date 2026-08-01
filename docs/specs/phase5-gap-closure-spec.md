# Spec: Phase 5 (Gap Closure — extension system goes live)

Manually derived breakdown of `docs/progress-handoff.md`'s nine "Known gaps /
deferred items" into implementable tickets. "Phase 5" here means the fifth
implementation round of the glyphux repo — NOT the PRD's far-future Phase 5
(multi-tenancy, Layer-3, AI agents, stock-media, marketplace hosting service),
which remains explicitly excluded. Every ticket is delivered as a commit on
branch `phase5-gap-closure` off `dev` (PR-ready against `dev`, no direct merge
in this environment) and passes an independent review + a full `-race`
validation pass before it is considered done. Repo convention: test-first /
TDD, behavior-first tests, all suites run with `-race`.

## Scope decisions (confirmed with the dispatcher before this doc was written)

- "Phase 5" = fifth implementation round, not PRD Phase 5 (excluded).
- Stale `worktree-agent-*` branch cleanup IS in scope: all 12 tips verified
  ancestors of `dev` with zero unique commits; deleted only after that
  verification.
- AI config shape: `ai{provider, model, api_key, base_url, rate_limit}` via
  `GLYPHUX_AI_*` env; `api_key` resolved only through the secrets seam (never
  plain config); unknown provider => fail-fast startup error naming valid set;
  `api.WithAI` applied iff provider configured.
- Network enforcement: no OS-level interception this round; optional
  `rpc_outbound_proxy_url` -> `HTTP_PROXY`/`HTTPS_PROXY` env (default off);
  WASM host surface NOT extended (Tier B stays `kv_get`/`kv_set`/`kv_delete`/
  `emit_event`); enforcement = `FilterManifest` (granted allowlist wins) +
  broker env injection + loader wiring.
- Consent: no `AlwaysConsent` anywhere in the daemon load path; consent
  decisions are the gate before `NewHostAPI`; optional operator knob
  `plugins.auto_consent_first_party` (default false, dev convenience only).
- Migration-list single owner: T1 adds migration 19; T4 adds migrations 14+15;
  T7 adds none; T9 adds a global-uniqueness registry guard test (repo has been
  bitten twice by collisions).
- Notifications capability boots with a log-mailer default adapter (no SMTP
  credentials required to boot); SMTP config optional.
- Marketplace catalog = embedded sample JSON + optional operator file-path
  override; NO remote catalog server; entitlement gates updates only
  (first-install entitlement skip preserved per PRD §12.5); consent hook for
  capability-kind packages; admin API + admin-ui page in scope; no storefront.
- Audit: `GET /api/v0/audit` endpoint in scope; audit admin UI deferred.
- Plugin KV: plain BLOB values, no TTL/size caps; `MemoryKVBackend` retained as
  default when `KernelDeps.KV` is nil (all existing tests byte-identical).
- WASM default limits: memory cap 1024 pages (~64 MiB), execution timeout 30s,
  fuel 0 = unlimited unless configured.
- Kernel version: committed root `VERSION` file as single source of truth,
  embedded at build time, `-ldflags` override for releases; unit test asserts
  semver + match.
- admin-ui `dist` freshness checked in CI (fresh build diff vs committed
  `internal/adminui/dist`).
- One tracking note per ticket, numbers 0037-0045 (verified free).
- Delivered as branch `phase5-gap-closure` off `dev`; committed per ticket;
  PR-ready against `dev`.

## Goals

Close all 9 gaps so the extension system is real in a running daemon: plugins
load (Tier A in-process first-party; Tier B wasm; Tier C rpc), install-time
consent enforced and admin-decidable, first-party capabilities registered and
AI operator-configurable, audit logging fires from a real daemon, the network
permission is enforced at the boundary, plugin KV persists, the wasm sandbox
has resource limits, marketplace packages have an admin surface, and the
process/build caveats are resolved.

## Non-goals

- PRD Phase 5 (multi-tenancy, Layer-3, AI agents, stock-media, marketplace
  hosting service).
- Extending the WASM host-function surface (content/users/media/register host
  funcs) — Tier B stays kv+events.
- OS-level network interception (seccomp/namespaces); hot-reload; remote plugin
  fetch; audit admin UI; marketplace storefront; KV TTL/versioning/size caps; a
  formal WIT contract.

## Sequencing

```
Stage 1 — T1 KV persistence ‖ T2 wasm limits ‖ T3 network filter/broker ‖ T5 registration+AI   (parallel packages)
Stage 2 — T4 consent wiring + adapter (needs T5)
Stage 3 — T6 plugin loader (needs T1,T2,T3,T4,T5)
Stage 4 — T7 audit activation ‖ T8a marketplace API (T7 needs T4+T6; T8a needs T5+T4)
Stage 5 — T8b marketplace admin UI (needs T8a + T4 consent screen)
Stage 6 — T9 process/build caveats (anytime; migration-registry guard test lands early)
Stage 7 — final validation: go build/vet/test -race, npm (admin-ui, sdk-js), boundary-verify, red-team adversarial pass
```

## Ticket T1 — Persistent plugin KV (SQL-backed ScopedKV)  [gap 6]

**Goal:** replace process-lifetime `MemoryKVBackend` with a DB-backed backend
so plugin state survives restarts, exposed through the unchanged
`HostAPI.Store()` surface.

**Scope IN:** `KVBackend` interface exported from `pkg/sdk` (namespaced
Get/Set/Delete); `MemoryKVBackend` retained (default when `KernelDeps.KV`
nil); new `internal/pluginstore` package over `internal/db.Queryer` (never
`database/sql` directly); table `plugin_kv(plugin_name TEXT, key TEXT, value
BLOB, updated_at TEXT, PRIMARY KEY(plugin_name,key))`; upsert on Set;
migration 19 appended to `cmd/glyphuxd` main.go list.

**Scope OUT:** TTL/expiry, per-key versioning, size caps, kernel
queryability, any change to guest-facing ABI (`kv_get`/`kv_set`/`kv_delete`
host funcs + RPC `StoreGet`/`Set`/`Delete` stay byte-identical).

**Dependencies:** none.

**Files:** `pkg/sdk/host.go` (+host_test.go), `internal/pluginstore/store.go`
(+store_test.go, new), `cmd/glyphuxd/main.go` (one line).

**Acceptance criteria (given/when/then):**
- plugin writes `Store().Set("token", v)`; daemon restarts against same
  SQLite; Get returns value (persistence across restarts).
- two plugins write same key; each reads only its own value (namespacing by
  `manifest.Name`).
- key Delete'd; Get returns `(nil,false,nil)`.
- second Set on existing key; latest value wins (upsert).
- real wasm guest (`testdata/kv_guest.wasm`) using `kv_set`/`kv_get` backed by
  DB backend; instance closed and reloaded; guest reads its own value
  (end-to-end host-func bridge).
- `db.Migrate` full daemon list; migration 19 applies; `plugin_kv` exists with
  composite PK; global-numbering guard test reports no duplicates.

**Test plan:** real SQLite open->write->close->reopen (consent_test pattern);
wasm round-trip through reloaded instance; rpc hostServer
StoreGet/Set/Delete parity; no mocks; `-race`.

**Risks:** `pkg/sdk` public must not import `internal/db` (interface in sdk,
impl in `internal/pluginstore`); `MemoryKVBackend` semantics byte-identical;
migration 19 re-grepped free before landing.

**Definition of done:** restart persistence proven through HostAPI, wasm, and
rpc paths; migration 19 in daemon list; build/vet/test -race + boundary green.

## Ticket T2 — WASM resource limits (fuel, memory cap, execution timeout)  [gap 7]

**Goal:** close no-fuel/no-memory-cap/no-timeout gap in `pkg/runtime/wasm`
using wazero native support.

**Scope IN:** `wasm.New(ctx, checker, opts...)` with
`Limits{MaxMemoryPages uint32, Fuel uint64, ExecutionTimeout time.Duration}`;
per-instance engine config via `WithMemoryLimitPages`/`WithFuel`/per-Call
context deadline; non-breaking defaults (1024 pages, 30s, fuel 0=unlimited);
new committed fixtures `busy_loop` + `memory_hog` (.rs source + .wasm, rebuild
command documented).

**Scope OUT:** WIT contract/ABI replacement; RPC-tier limits; fuel-cost
tuning; per-instance growth tuning knobs.

**Dependencies:** none.

**Files:** `pkg/runtime/wasm/runtime.go`, `runtime_test.go`, `limits_test.go`
(new), `testdata/busy_loop.{rs,wasm}` + `memory_hog.{rs,wasm}` (new
committed).

**Acceptance criteria:**
- guest loops forever; `Instance.Call` under `ExecutionTimeout` returns error;
  sibling instance + host unaffected (isolation, reuse trap-isolation tests).
- guest grows memory beyond `MaxMemoryPages`; cap enforced as error not host
  crash.
- Fuel finite budget; guest burns it; deterministic fuel-exhausted error.
- existing fixtures + new defaults; full package suite stays green.
- Runtime built without new options; behavior identical to today.

**Test plan:** real wazero + new fixtures;
`TestGuestTrapDoesNotCrashHostOrSiblingInstance` pattern re-run; `-race`.

**Risks:** defaults judgment call (documented); committing .wasm requires
toolchain — commit binaries + source + rebuild command (repo already commits
.wasm fixtures).

**Definition of done:** all three axes enforced with behavior-first tests;
defaults non-breaking; fixtures committed with rebuild docs; `-race` green.

## Ticket T3 — Network policy enforcement  [gap 5]

**Goal:** network permission allowlist becomes an enforcement point, not just a
decision primitive.

**Scope IN:** `sdk.FilterManifest(m, granted)` producing manifest whose network
args = granted subset (`AllowsNetworkHost` answers from granted, deny-by-
default, exact-match — `pkg/sdk/manifest.go` semantics unchanged);
`rpc.Broker` sets `GLYPHUX_NETWORK_ALLOWLIST` in subprocess env at launch;
optional config `rpc_outbound_proxy_url` -> `HTTP_PROXY`/`HTTPS_PROXY` (default
off); test proving Tier B has no network host functions (deny-by-absence,
`hostfuncs.go` unchanged).

**Scope OUT:** OS-level interception; wasm network host funcs;
wildcard/subdomain; per-plugin proxies; in-daemon filtered-host proof (that is
a T6 acceptance criterion).

**Dependencies:** package-level none; T6 consumes `FilterManifest`.

**Files:** `pkg/sdk/filter.go` (+filter_test.go); `pkg/runtime/rpc/broker.go`
(+env, broker_test.go); `pkg/runtime/wasm/hostfuncs_test.go` (structural-
denial proof); `internal/config/config.go` (+`rpc_outbound_proxy_url`).

**Acceptance criteria:**
- manifest declares `network:[a.example]`; consent grants
  `network:[a.example]`; filtered HostAPI `AllowsNetworkHost(a.example)==true`,
  `b.example==false`.
- manifest declares `[a.example,b.example]`; consent grants only `a.example`;
  filtered => `b.example==false` (granted wins).
- consent drops network permission entirely; filtered => every host denied.
- rpc fixture plugin launched via broker with granted `{a.example}`;
  `AllowsNetworkHost` returns true for `a.example` false for `b.example`;
  subprocess env `GLYPHUX_NETWORK_ALLOWLIST` contains exactly `a.example`
  (denial survives subprocess boundary).
- `rpc_outbound_proxy_url` set => `HTTP_PROXY`/`HTTPS_PROXY` set; absent =>
  unset.
- any wasm guest attempts outbound dial => no host function exists (deny-by-
  absence proven by test).

**Test plan:** real consent store (granted set source); real committed wasm
fixtures; real subprocess broker fixtureplugin; no mocks; `-race`.

**Risks:** hostile Tier C can ignore env+query without operator egress proxy —
documented advisory bound.

**Definition of done:** filter proven (granted>declared, exact-match); broker
env proven; proxy optional/off; Tier B structural denial proven+documented;
`-race` + boundary green.

## Ticket T4 — Consent wiring + consent-screen UI  [gap 2]

**Goal:** `internal/consent` goes live; a single adapter implements both
runtime consent seams backed by `consent.Engine`; loader gates on
`IsConsented` before `NewHostAPI`; admin API + consent-screen UI; decisions
audited.

**Scope IN:** `internal/plugin/consent_adapter.go` implementing
`wasm.ConsentChecker{Consented(plugin,capability) bool}` AND
`rpc.ConsentChecker{Allowed(plugin,capability,scope) bool}` both derived from
`Engine.IsConsented` granted subsets (no `AlwaysConsent` in daemon path);
loader flow: IsConsented -> filter granted -> build host (consumed in T6,
adapter ships here); API `GET /api/v0/plugins`, `GET
/api/v0/plugins/consent-requests`, `POST
/api/v0/plugins/consent-requests/{plugin}/decide` (granted-subset body;
admin-only + CSRF; `DecidedBy` = acting identity.User.ID; 422 on
`ErrGrantExceedsRequest`); daemon constructs
`consent.NewEngine(db, consent.WithAudit(audit.NewLogger(db)))`; T4 adds
`consent.Migrations(14)`+`audit.Migrations(15)` to main.go migration list
(single owner); sdk-js `plugins.ts`; admin-ui `ConsentScreen.tsx`
(Approve/Deny/per-scope partial checkboxes; pending re-consent state).

**Scope OUT:** marketplace review flow (T8 hooks in later); policy engines
beyond the auto-consent knob; audit listing UI (T7).

**Dependencies:** T5 (first-party plugins = consent subjects). Loader
consumption = T6.

**Files:** `internal/plugin/consent_adapter.go` (+test); `internal/api/consent.go`
(+consent_test.go); `internal/api/api.go` (WithConsent, routes);
`cmd/glyphuxd/main.go` (engine + migrations 14/15); `sdk-js/src/plugins.ts`
(+client/index); `admin-ui/src/pages/plugins/ConsentScreen.tsx` (+test,
routing).

**Acceptance criteria:**
- undecided manifest; `IsConsented` => false => plugin refused at load (never
  reaches `NewHostAPI`).
- manifest whose API axis gained scope at same version after approval;
  `IsConsented` => false => stale consent forces re-consent (fingerprint path
  end-to-end).
- admin approves strict subset of commerce's request; `Decision.Status==partial`;
  `IsConsented` grants exactly that subset; grant exceeding request => 422
  `ErrGrantExceedsRequest`.
- consent decision with `WithAudit(logger)`; `audit_records` row
  action=`consent.decide`; no logger => decision persists, audit optional.
- wasm plugin declaring `events:emit` but not consented;
  `ConsentChecker.Consented("p","events:emit")==false` (wasm seam denies even
  declared capabilities when unconsented).
- non-admin/anonymous => 403/401; mutations require CSRF.
- decision made in UI; daemon restarts (real SQLite); decision persists;
  re-consent surfaces as pending.

**Test plan:** real SQLite consent+audit stores incl. persistence-across-
reopen; httptest 401/403/422 + partial-grant round-trip; admin-ui render tests
with mocked sdk-js; `-race`.

**Risks:** `DecidedBy` resolved from session Principal; two seams different
signatures — adapter is the only mapping point; migration-list ownership must
not collide with T7.

**Definition of done:** unconsented refused at load; stale consent re-consents;
partial grants enforced transport->engine->adapter; decisions audited when
logger present; consent screen live; migrations 14+15 in daemon list; `-race`
green.

## Ticket T5 — Capability registration + AI config/enablement  [gap 3]

**Goal:** the 5 first-party capability plugins registered in the daemon; AI
operator-configurable via `api.WithAI`; the endpoint's 404-until-configured
opt-in replaced by a real configured path.

**Scope IN:** new `internal/plugin` package: in-process (Tier A) registrar
`RegisterPlugin(p sdk.Plugin)`/`Registered() []sdk.Plugin`, building shared
`KernelDeps{Compositions, Content, Media, Identities, Bus, KV
(MemoryKVBackend for now; T6 swaps), Blocks, Audit(nil until T7)}` +
`NewHostAPI` + `Register(host)`; `cmd/glyphuxd` registers
forms,seo,commerce,membership,notifications at boot (fatal-fast on invariant
violation); config `ai{provider,model,api_key,base_url,rate_limit}` +
`GLYPHUX_AI_*` env, `api_key` via `secrets.go`; `cmd/glyphuxd` calls
`api.WithAI` when configured.

**Scope OUT:** plugin enable flags per capability (all first-party enabled at
boot); anything beyond the 5 plugins.

**Dependencies:** `internal/config` only.

**Files:** `internal/plugin/registrar.go` (+test); `internal/config/config.go`
+ `secrets.go` (+ai section); `cmd/glyphuxd/main.go`; `internal/api/ai.go`
unchanged (consumes WithAI).

**Acceptance criteria:**
- `ai.provider` configured valid adapter+model; `buildFullHandler` runs;
  `api.WithAI(ai.NewService(adapter))` applied; `POST /api/v0/ai/compose` live
  (200 via fake adapter/httptest fake provider).
- `ai.provider` unset; AI disabled; endpoint 404s; startup succeeds without
  key.
- `ai.provider=unknown-adapter`; startup fails fast with clear error naming
  valid set (`claude|openai|gemini|openai-compatible`).
- `GLYPHUX_AI_API_KEY` set; key resolved via `secrets.go` seam; plain
  config/serialized config never contains it (redaction asserted).
- each first-party capability `Manifest()`/behavior unchanged; registered
  through registrar; existing per-capability suites stay green
  (`capabilities/*` read-only).
- duplicate or empty plugin name; fails fast matching `firstparty.RegisterAll`
  invariant style.
- booted daemon; `GET /api/v0/content-types` + `GET /api/v0/blocks`; the 5
  plugins' content types (`form_submission`, `product`, `order`,
  `membership_tier`, `membership_subscription`) and only-first-party blocks
  present.

**Test plan:** real SQLite daemon-level httptest (content-types/blocks/ai-
compose); in-process fake `ai.Adapter` + one httptest fake provider; config
tests provider matrix + secrets redaction; `-race`; sdk-js `ai.test.ts` flip;
admin-ui green.

**Risks:** emitter host placement transport-level in `internal/api`
(`aiCallerManifest` precedent); notifications needs log-mailer default;
`rate_limit` maps to `Service.Limits`.

**Definition of done:** all 5 manifests registered + API-provable; `WithAI`
iff configured with clear unknown-provider failure; `api_key` only via
secrets; per-capability suites green; `-race` + boundary green.

## Ticket T6 — Plugin loader wiring  [gap 1]

**Goal:** generalize T5 registrar into a full 3-tier loader wired into
`cmd/glyphuxd`/`buildFullHandler`; `pkg/runtime` + `internal/consent` +
`internal/pluginstore` become real in the daemon for the first time.

**Scope IN:** `internal/plugin` loader extends T5 registrar: Tier A in-process
first-party (`RegisterPlugin`, unchanged); Tier B wasm loads `.wasm` from
configured plugins dir (`GLYPHUX_PLUGINS_DIR` or config
`plugins[]{name,tier,source}`); per-instance wazero engine with T2 Limits;
`ConsentChecker` = T4 adapter; `KernelDeps.KV` = T1 DB backend; host built from
T3 `FilterManifest(granted)`; `Subscribe` when guest exports
`event_buf_ptr`/`on_event`; Tier C rpc subprocess broker from
manifest/config; T3 env injection; consent via T4 adapter (rpc seam);
supervision crash->dead daemon unaffected. Daemon boot flow in
`buildFullHandler`: assemble shared `KernelDeps{Compositions, Content, Media,
Identities, Bus, KV(pluginstore), Blocks, Audit(nil until T7)}` -> load
configured plugins -> fatal-fast on invariant violation (duplicate names,
failed consent, tier misconfig). Loader lives in `internal/`;
`internal/boundary` gates enforced after wiring.

**Scope OUT:** WASM host-surface extension; hot-reload; remote fetch; rpc
plugin registry beyond config.

**Dependencies:** T5 (registrar + KernelDeps assembly + config), T4 (consent
adapter + engine), T1 (DB KV), T3 (filter + broker env), T2 (limits).

**Files:** `internal/plugin/loader.go` + `loader_test.go` (new);
`internal/plugin/consent_adapter.go` (T4); `internal/config/config.go`
(+plugins section, `GLYPHUX_PLUGINS_DIR`); `cmd/glyphuxd/main.go` (wire loader
into `buildFullHandler`; migrations 14/15/19 already in list);
`internal/boundary` (re-verify only, no relaxation).

**Acceptance criteria:**
- first-party capability registered at boot (T5 path); `GET /api/v0/content-types`
  => its types reachable through daemon host (Tier-A host wiring end-to-end).
- wasm plugin in plugins dir with approved decision; booted; `Store().Set/Get`
  survives daemon restart against same SQLite (T1 persistence through real host
  bridge).
- wasm plugin manifest declares `network:[a.example,b.example]`, consent
  granted only `a.example`; booted; `HostAPI.AllowsNetworkHost(b.example)==false`
  (T3 filter active in loaded host).
- wasm plugin with no consent decision on file (default policy); booted; load
  refused before `NewHostAPI` — daemon logs and continues or fails fast per
  policy.
- wasm plugin loops forever (T2 fixture) with `ExecutionTimeout` set;
  killed/errored; sibling instances + daemon survive (T2 limits wired per-
  instance).
- rpc plugin (`testdata/fixtureplugin`); launched via loader; `Broker.State()`
  reaches running; `Register` round-trips; subprocess killed; transitions to
  dead without taking down daemon.
- rpc plugin granted allowlist; launched; env carries exactly granted hosts
  (T3).
- loader wiring lands; `internal/boundary` tests green: no `*sql.DB`/`*os.File`
  leak through exported signatures; no raw `database/sql` import outside
  `internal/db`.

**Test plan:** real SQLite (consent+KV+audit); real committed wasm fixtures
(`kv_guest`, `events_guest`, `busy_loop`, `memory_hog`); real subprocess
broker fixtureplugin built in-test; daemon-level httptest; boundary-verify
suite; `-race`; no mocks.

**Risks:** two consent seams (different signatures) — T4 adapter single
mapping point; `RegisterAdminPage`/`RegisterJob` recorded but nothing renders
them (documented); Tier B cannot register content types (documented); loader
must not import `database/sql`.

**Definition of done:** a real `glyphuxd` boots with first-party + >=1 wasm +
>=1 rpc plugin loaded from config under real consent; restart persistence,
network filter, limits, supervision proven by tests; boundary-verify green;
`-race` green repo-wide.

## Ticket T7 — Audit activation + item-level CRUD auditing  [gap 4]

**Goal:** audit goes live: `KernelDeps.Audit` wired so loaded plugins'
boundary gates log; deferred item-level Content/Users/Media CRUD auditing
delivered.

**Scope IN:** daemon constructs `audit.NewLogger(db)`; pass as
`KernelDeps.Audit` into T6 loader's shared KernelDeps (every loaded plugin's
HostAPI logs boundary allow/deny:
`RegisterContentType`/`RegisterBlock`/`RegisterAdminPage`/`RegisterJob`/`On`/`Emit`
+ sensitive events `user.created`, `payment.*`); item-level: `internal/audit`
gains domain-facing recorders
`RecordContent(action created|updated|deleted|published|unpublished|rolled_back,
principal, typeName, id)`, `RecordMedia(uploaded|updated|deleted, principal,
id)`, `RecordUser(created|role_changed|deactivated|reactivated, principal,
userID)` persisting via `Logger.Log` with `Detail=JSON {actor_id, role,
item_id, type}`; wire via additive `WithAudit(*audit.Logger)` constructor
options (nil-safe) at `content.NewAPI`
Create/Update/Delete/Publish/Unpublish/Rollback; `media.NewAPI`
Upload/Delete/UpdateMetadata; `identity.NewService`
CreateUser/UpdateRole/Deactivate/Reactivate; `GET /api/v0/audit?plugin=`
(admin-only) via `ListByPlugin`.

**Scope OUT:** read auditing; retention/compaction; request-middleware
auditing; plugin-authored records; audit admin UI.

**Dependencies:** T4 (logger + migrations in daemon list), T6 (`KernelDeps.Audit`
consumers).

**Files:** `internal/audit/recorders.go` (new) + tests; `internal/content/content.go`
(+WithAudit opt, 6 sites) + content_test.go; `internal/media/media.go` (3
sites) + tests; `internal/identity/identity.go` + `users_manage.go` (4 sites)
+ tests; `internal/api/audit.go` (new) + audit_test.go; `cmd/glyphuxd/main.go`
(logger -> KernelDeps + WithAudit on three constructors).

**Acceptance criteria:**
- `KernelDeps.Audit` set; loaded plugin's `RegisterContentType` denied (missing
  scope); `audit_records` row action=`hostapi.register_content_type`
  allowed=false.
- `KernelDeps.Audit` set; plugin emits sensitive event (`user.created`,
  `payment.*`) via `HostAPI.Emit`; row recorded.
- content item created via `contentAPI.Create`; row action=`content.created`
  Detail contains actor id+role+item id+type; same for
  Update/Delete/Publish/Unpublish/Rollback; media
  Upload/Delete/UpdateMetadata; identity
  CreateUser/UpdateRole/Deactivate/Reactivate.
- read (Get/List/Open); no row written.
- `WithAudit(nil)`; any write; no audit writes, no panic, behavior
  byte-identical.
- rows written; daemon restarts same SQLite; rows survive (`ListByPlugin`
  returns them).
- non-admin `GET /api/v0/audit` => 403; anonymous 401.
- wiring lands; boundary-verify green.

**Test plan:** real SQLite (audit + content/media/identity stores); daemon-
level httptest driving real write paths; per-domain unit tests nil and real
logger; `-race`.

**Risks:** additive Option constructors keep existing tests compiling; Detail
payload stable/parseable; append-only growth documented; define 13 action
constants once in `recorders.go`.

**Definition of done:** boundary gates + sensitive events + every item-level
write audited in a real daemon; reads un-audited; nil-logger safe; audit
endpoint live + 401/403; rows survive restart; boundary-verify + `-race`
green.

## Ticket T8 — Marketplace HTTP/UI surface  [gap 8]

**Goal:** a real admin surface over existing marketplace primitives: catalog
listing, install via `InstallFromPackage`, offline entitlement status — with
PRD §12.5 by-design semantics preserved and proven.

**Scope IN:** `internal/api/marketplace.go` (+test): `GET
/api/v0/marketplace/catalog` (packages from configured/embedded catalog
source; version + `requires.core` compatibility via
`CoreConstraintSatisfied(kernel.Version)`); `POST
/api/v0/marketplace/packages/{id}/install` (fetch package bytes from catalog;
call `preset.InstallFromPackage` / `bundle.InstallFromPackage(ctx, principal,
sp, pub, kernelVersion)` — presets:manage-gated, admin-only + CSRF); `GET
/api/v0/marketplace/entitlements` (offline verify entitlement token:
`CanFetchUpdate` + `DescribeExpiry` -> active/expired/not_yet_valid/invalid);
config `marketplace.public_key` (hex ed25519, baked-in default + operator
override); sdk-js `marketplace.ts`; admin-ui `pages/marketplace/` page:
catalog list, install button, entitlement/expiry badges, consent-screen hook
for capability-kind packages.

**Scope OUT:** remote catalog/registry server; update-fetch service; publish
pipeline; storefront; gating first installs on entitlement (by-design skip
asserted not enforced).

**Dependencies:** T5 (config + `kernel.Version`), T4 (consent-screen hook), T6
(plugin enablement path).

**Files:** `internal/api/marketplace.go` (+marketplace_test.go);
`internal/api/api.go` (WithMarketplace, routes); `cmd/glyphuxd/main.go`
(stores + key config); `internal/config/config.go` (+marketplace section);
`sdk-js/src/marketplace.ts` (+client/index); `admin-ui/src/pages/marketplace/`
(+tests, routing).

**Acceptance criteria:**
- configured catalog entry for package P; `GET /api/v0/marketplace/catalog` =>
  P listed with version + `requires.core` compatibility via
  `CoreConstraintSatisfied` (compatible/too-new per entry).
- valid preset-kind package bytes; `POST .../install` => `preset.InstallFromPackage`
  runs (signature + `requires.core` verified; registry-compat + entitlement
  skipped by design) and returns installed id persisted.
- tampered package; install => 422 signature-verification error; nothing
  persisted.
- `requires.core` > `kernel.Version`; install rejected
  (`ErrRequiresCoreNotSatisfied`); nothing persisted.
- valid entitlement token; GET entitlements => `CanFetchUpdate` true +
  `DescribeExpiry` readable; expired/missing => update gated but first install
  still allowed (by-design skip asserted).
- package whose manifest needs capabilities; installed => consent decision
  required before enablement (T4 hook surfaced in UI).
- non-admin install => 403; anonymous 401; mutations require CSRF.
- existing `internal/preset` + `internal/bundle` store tests still green.

**Test plan:** real SQLite preset + bundle stores; real ed25519 keypairs
generated in-test; `SignedPackage` fixtures; httptest all routes + auth
(401/403/422/CSRF); admin-ui tests mocked sdk-js; `-race`.

**Risks:** public-key trust anchor baked-in default vs override; catalog
source embedded JSON vs file; capability-kind packages couple to T4/T6
enablement — preset/bundle kinds consent-free via existing Import path.

**Definition of done:** catalog + install + entitlements API live with §12.5
semantics proven unchanged; admin-only auth + CSRF; consent hook for
capability-kind packages; admin-ui page renders list/install/status; sdk-js
resource; existing store tests green; `-race` + boundary green.

## Ticket T9 — Process/build caveats  [gap 9]

**Goal:** resolve the four handoff process/build caveats: single-source kernel
version, migration-uniqueness guard, admin-SPA rebuild story, stale-branch
hygiene.

**Scope IN:** (a) committed root `VERSION` file (0.2.0) as single source of
truth; `pkg/kernel` derives `Version` from it at build time (`go:embed`) with
`-ldflags -X` override for release builds; unit test asserts semver + matches
`VERSION` file; (b) new `internal/db/migrations_global_test.go` central
registry test walking every package's `Migrations` slice, failing on any
duplicate `Version` naming both colliding packages; (c) justfile/CI target
`npm --prefix admin-ui run build` -> copy to `internal/adminui/dist`; CI step
checks committed dist freshness vs fresh build; (d) `git branch | grep
worktree-agent- | xargs git branch -d` after prior ancestor-verification;
AGENTS.md hygiene note.

**Scope OUT:** version auto-tagging in CI.

**Dependencies:** none.

**Files:** `VERSION` (new root), `pkg/kernel/kernel.go` (+kernel_test.go),
`scripts/` (version target), `justfile`, `internal/db/migrations_global_test.go`
(new), `.github/workflows/ci.yml` (+dist check), `AGENTS.md`/`README`.

**Acceptance criteria:**
- committed `VERSION` file; `go build` => `kernel.Version` equals file; release
  build with ldflags override => override wins and `glyphuxd --version`
  matches; semver test => parses `MAJOR.MINOR.PATCH`.
- contributor adds migration whose `Version` duplicates existing global number;
  registry test fails naming both colliding packages.
- dist/ stale vs admin-ui source; freshness check fails (CI) or documented
  rebuild target invoked (manual).
- stale `worktree-agent-*` branches deleted (after sign-off); `git branch -a`
  => none remain; verification asserts zero unique commits before deletion.

**Test plan:** registry test deterministic repo-wide; version test + scripted
release build; dist freshness via `npm run build` diff; branch cleanup verified
by inspection.

**Risks:** (a) touches release tooling — keep plain-go-build fallback to
committed `VERSION`; (d) destructive — human gate.

**Definition of done:** all four caveats resolved or documented; registry guard
test merged; dist story explicit; branches cleaned pending sign-off.
