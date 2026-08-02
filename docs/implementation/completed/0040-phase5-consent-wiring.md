# Implementation: Phase 5 — consent wiring + consent-screen UI

## Goal

Take `internal/consent` live: a single adapter implements both runtime
consent seams (`wasm.ConsentChecker` and `rpc.ConsentChecker`) derived from
`consent.Engine.IsConsented`; the loader gates on `IsConsented` before
building any `NewHostAPI`; an admin API serves consent requests and a
consent-screen UI renders them; decisions are audited when a logger is
present. Full scope, acceptance criteria, and definition of done:
`docs/specs/phase5-gap-closure-spec.md` Ticket T4.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

In-flight

## Current Decisions

- `internal/plugin/consent_adapter.go` is the single mapping point for the
  two different seam signatures (`Consented(plugin,capability)` vs
  `Allowed(plugin,capability,scope)`), both from `Engine.IsConsented`
  granted subsets; no `AlwaysConsent` anywhere in the daemon load path
  (optional `plugins.auto_consent_first_party` knob, default false, dev
  convenience only).
- API: `GET /api/v0/plugins`, `GET /api/v0/plugins/consent-requests`, `POST
  /api/v0/plugins/consent-requests/{plugin}/decide` — granted-subset body,
  admin-only + CSRF, `DecidedBy` = acting identity.User.ID, 422 on
  `ErrGrantExceedsRequest`.
- Daemon constructs `consent.NewEngine(db, consent.WithAudit(audit.NewLogger(db)))`;
  T4 owns migrations 14 (consent) + 15 (audit) in the main.go list —
  must not collide with T7.
- Loader consumption (IsConsented -> filter granted -> build host) lands in
  T6; the adapter ships here.

## Open Questions

None open — marketplace review flow (T8 hook) and audit listing UI (T7) are
explicitly out of scope.

## Files/Modules Expected

- `internal/plugin/consent_adapter.go` (+test).
- `internal/api/consent.go` (+consent_test.go); `internal/api/api.go`
  (WithConsent, routes).
- `cmd/glyphuxd/main.go` (engine + migrations 14/15).
- `sdk-js/src/plugins.ts` (+client/index).
- `admin-ui/src/pages/plugins/ConsentScreen.tsx` (+test, routing).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T4. Key proofs:
undecided plugin refused at load (never reaches `NewHostAPI`); stale consent
(scope gained at same version) forces re-consent via the fingerprint path;
partial grants enforced transport->engine->adapter with 422 on over-grant;
`audit_records` row `consent.decide` when logger present (audit optional
otherwise); wasm seam denies even declared capabilities when unconsented;
401/403/CSRF; decision persists across a real-SQLite daemon restart and
re-consent surfaces as pending in the UI.

## Risks

- `DecidedBy` must be resolved from the session `Principal` — do not trust
  client-supplied identity.
- Two seams have different signatures; the adapter is the only mapping point
  and must not drift from `Engine.IsConsented`.
- Migration-list ownership (14/15 here) must not collide with T7 or T1's 19.
