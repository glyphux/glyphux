# Implementation: Phase 5 — plugin loader wiring

## Goal

Generalize the T5 registrar into a full 3-tier loader wired into
`cmd/glyphuxd`/`buildFullHandler`: Tier A in-process first-party plugins
(unchanged `RegisterPlugin`), Tier B wasm plugins loaded from a configured
plugins dir with T2 resource limits, T4 consent, T1 DB-backed KV, and T3
network filtering applied to the built host, and Tier C rpc subprocess
plugins via the broker with T3 env injection, T4's rpc consent seam, and
supervision — so `pkg/runtime`, `internal/consent`, and `internal/pluginstore`
become real in a running daemon for the first time. Full scope, acceptance
criteria, and definition of done: `docs/specs/phase5-gap-closure-spec.md`
Ticket T6.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

In-flight

## Current Decisions

- Loader lives in `internal/plugin`; config `plugins[]{name,tier,source}` +
  `GLYPHUX_PLUGINS_DIR`; boot flow assembles shared
  `KernelDeps{..., KV(pluginstore), Blocks, Audit(nil until T7)}` -> load
  configured plugins -> fatal-fast on invariant violation (duplicate names,
  failed consent, tier misconfig).
- Tier B: per-instance wazero engine with T2 `Limits`; `ConsentChecker` =
  T4 adapter; host built from T3 `FilterManifest(granted)`; `Subscribe` when
  the guest exports `event_buf_ptr`/`on_event`.
- Tier C: subprocess broker from manifest/config; T3 env injection; consent
  via the T4 rpc seam; supervision crash->dead without taking down the
  daemon.
- `internal/boundary` re-verified after wiring — no relaxation; loader must
  not import `database/sql`; no `*sql.DB`/`*os.File` in exported signatures.

## Open Questions

None open — `RegisterAdminPage`/`RegisterJob` are recorded but nothing
renders them (documented); Tier B cannot register content types (documented).

## Files/Modules Expected

- `internal/plugin/loader.go` + `loader_test.go` (new).
- `internal/plugin/consent_adapter.go` (T4).
- `internal/config/config.go` (+plugins section, `GLYPHUX_PLUGINS_DIR`).
- `cmd/glyphuxd/main.go` (wire loader into `buildFullHandler`; migrations
  14/15/19 already in list).
- `internal/boundary` (re-verify only, no relaxation).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T6. Key proofs:
a real `glyphuxd` boots with first-party + >=1 wasm + >=1 rpc plugin loaded
from config under real consent; wasm `Store().Set/Get` survives daemon
restart against the same SQLite (T1 through the real host bridge); the T3
filter is active in the loaded host (`AllowsNetworkHost(b.example)==false`);
an unconsented plugin is refused before `NewHostAPI`; a busy-loop guest is
killed while siblings + daemon survive (T2); the rpc fixture plugin reaches
running, round-trips `Register`, and transitions to dead on kill without
taking down the daemon; subprocess env carries exactly the granted hosts;
boundary-verify + `-race` green repo-wide.

## Risks

- Two consent seams with different signatures — the T4 adapter is the single
  mapping point; keep loader wiring to it.
- Loader must not import `database/sql` (boundary invariant) — KV goes
  through `internal/pluginstore` only.
- Tier B cannot register content types and admin pages are recorded-not-
  rendered — document both so expectations stay honest.
