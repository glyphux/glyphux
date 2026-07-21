# Implementation: Phase 2 slice 2.8 — audit logging

## Goal

PRD §10.5: "every sensitive grant and every cross-boundary call is recorded
by the audit subsystem." §15.3 restates it under Security: "Audit log for
all sensitive operations and cross-boundary calls." This slice builds that
subsystem as a real, DB-backed, append-only log and wires it into the two
places in the codebase that already have a clean allow/deny signal for a
security-relevant action: the install-time consent engine (slice 2.7 —
every `Decide` call, whatever the outcome) and `pkg/sdk`'s `HostAPI`
boundary gates (the registration/event methods with a clear capability
check: `RegisterContentType`, `RegisterBlock`, `RegisterAdminPage`,
`RegisterJob`, `On`, `Emit`).

## Owning Contexts

- (no CONTEXT.md exists yet — see docs/agents/domain.md; this is
  core-kernel work: a new package `internal/audit`, plus additive changes
  to `internal/consent` and `pkg/sdk`)

## Status

Complete. Built via TDD (failing tests written first for each of the three
seams below, confirmed red, then minimal implementation to green). Full
repo build/vet/test -race green across all 24 packages, plus a hand-driven
program proving one shared audit trail correctly spans a real consent
decision AND real HostAPI boundary calls (an allow and a deny) for the same
plugin, in true chronological order.

## Current Decisions

- **Scope confirmed with the user before writing any test** (per the /tdd
  skill's seam-confirmation requirement): this slice covers consent
  decisions plus HostAPI boundary gates — NOT item-level Content/Users/Media
  CRUD auditing (deferred; see Risks).

- **New package `internal/audit`**, not `pkg/sdk`'s own subpackage: audit is
  a kernel-owned concern (PRD's repo-tree sketch lists `audit/` alongside
  `consent/` under the kernel, not under the plugin-facing SDK surface).
  `Record{ID, PluginName, Action, Allowed, Detail, OccurredAt}`, `Logger`
  wrapping `*db.DB`, `Log`/`ListByPlugin`. Real SQLite-backed tests, no
  mocks, matching `internal/consent`'s own test conventions.

- **Migration version 15** — the highest migration version in the repo at
  the time this slice was written is 14 (`internal/consent/store.go`'s
  `consent_decisions` table). Re-verified by grepping `Version:` across
  `internal/*/*.go` immediately before writing `internal/audit/store.go`
  (this has bitten the project three times already this phase: 0009/0010,
  mfa.go's own 8→12 fix, and consent's own 13→14 pick).

- **Audit logging is opt-in, not a hard dependency**, in both integration
  points:
  - `internal/consent.NewEngine(db *db.DB, opts ...Option)` — a new
    variadic `Option` parameter (`consent.WithAudit(logger)`), backward
    compatible with every existing `NewEngine(d)` call site (zero-arg
    variadic call is unaffected). `Decide` logs a `"consent.decide"` record
    (`Allowed = status != StatusDenied`) after a successful insert, only if
    `e.audit != nil`.
  - `pkg/sdk.KernelDeps` gets a new `Audit *audit.Logger` field, exactly
    like `KV`/`Bus`'s existing "nil = default/no-op" convention. A private
    `(h *hostAPI) auditLog(ctx, action string, allowed bool, detail string)`
    helper no-ops when `h.deps.Audit == nil`; every other test in
    `host_test.go` that predates this slice leaves `Audit` nil and is
    unaffected (proven by `TestHostAPIWithoutAudit_StillWorks` /
    `TestDecide_WithoutAudit_StillWorks`).

- **Both allowed AND denied attempts are logged**, not just successes. A
  denial is itself a security-relevant event under the PRD's
  adversarial-by-default model (§10.1) — a plugin probing for a capability
  it doesn't have is exactly the signal an admin/security reviewer would
  want surfaced, not silently dropped.

- **`context.Background()` fallback for methods without a `ctx` parameter**:
  `RegisterBlock`, `RegisterAdminPage`, `RegisterJob`, and `On` predate
  context plumbing in their signatures (slice 2.1's design, out of scope to
  change here) — `auditLog` accepts a nil `context.Context` and substitutes
  `context.Background()`. `RegisterContentType` and `Emit` already receive a
  real `ctx` and forward it.

- **Audit-log failures inside `consent.Decide` surface as an error to the
  caller** (wrapping "decision recorded but audit log failed"), because a
  consent decision's own audit trail gap is exactly the kind of silent
  failure the subsystem exists to prevent — the caller should know, even
  though the already-persisted `Decision` remains valid and usable. By
  contrast, `pkg/sdk`'s `auditLog` helper is fire-and-forget (`_ =
  h.deps.Audit.Log(...)`): HostAPI boundary methods have many call sites
  across many methods and no natural way to add a second failure mode to an
  already-established error contract without a breaking signature change;
  this asymmetry is a deliberate, documented judgment call, not an
  oversight.

## Open Questions — resolved

- **Should audit logging be a hard constructor requirement?** No — see
  above. Making `Audit` required would force every existing test and every
  future minimal HostAPI/Engine construction (e.g. a plugin's own unit
  tests) to wire up a database it doesn't otherwise need, for a concern
  that's additive observability, not correctness.

## Files/Modules Changed

- `internal/audit/audit.go` (new) — `Record`, `Logger`, `NewLogger`, `Log`,
  `ListByPlugin`.
- `internal/audit/store.go` (new) — `Migrations` (version 15,
  `audit_records` table + index on `(plugin_name, id)`), `insert`,
  `listByPlugin`.
- `internal/audit/audit_test.go` (new) — 5 tests: persist+retrieve,
  allowed+denied both recorded (in insertion order), unknown plugin returns
  empty (not an error), no cross-plugin leakage, persistence across
  `Logger` instances.
- `internal/consent/consent.go` — added `Option`/`WithAudit`, `Engine.audit`
  field, `NewEngine(db, opts...)` (backward compatible), audit-logging call
  inside `Decide`.
- `internal/consent/consent_test.go` — added
  `TestDecide_WithAudit_LogsApprovalAndDenial`,
  `TestDecide_WithoutAudit_StillWorks`.
- `pkg/sdk/host.go` — added `KernelDeps.Audit`, `hostAPI.auditLog` helper,
  wired into `RegisterContentType`, `RegisterBlock`, `On`, `Emit`,
  `RegisterAdminPage`, `RegisterJob`.
- `pkg/sdk/host_test.go` — added `newTestAuditLogger`,
  `TestHostAPIAuditsRegisterAdminPageAllowAndDeny`,
  `TestHostAPIWithoutAudit_StillWorks`.

## Acceptance Criteria

- [x] `internal/audit` package: real SQLite-backed `Logger`, own migration,
      no collision with existing migration versions (verified: 15 is free).
- [x] Consent decisions (approve/partial/deny) are recorded when
      `consent.WithAudit` is configured; unaffected when it isn't.
- [x] HostAPI boundary-gate allow AND deny attempts are recorded when
      `KernelDeps.Audit` is set; unaffected when it's nil.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide.
- [x] TDD discipline followed: seam confirmed with the user before any test
      was written; red confirmed before each implementation; one behavior
      per cycle.

## Risks

- **Item-level Content/Users/Media CRUD auditing is deferred** (explicitly
  scoped out per the user's confirmed answer to the seam-confirmation
  question). A future slice would need to decide whether every
  Get/List/Create/Update/Delete/Publish/Unpublish call warrants its own
  audit record (likely yes for write/publish-grade operations, probably not
  for read — a judgment call for that slice, not this one).
- **No audit trail for `pkg/runtime/wasm`/`pkg/runtime/rpc` boundary
  crossings themselves** — those runtimes call into `pkg/sdk.HostAPI`
  methods that ARE now audited (e.g. a WASM guest's `emit_event` call
  reaches `hostAPI.Emit`, which logs), so boundary crossings that go
  through HostAPI's own gated methods are covered transitively. What is
  NOT covered: WASM instantiation itself (a guest's import set being
  satisfied/rejected at `wazero`'s link layer, per slice 2.4) and RPC
  subprocess launch/crash events (slice 2.5) don't themselves produce audit
  records — only the HostAPI calls a running plugin subsequently makes do.
  A future slice could add audit hooks at `wasm.Runtime.Load`/
  `rpc.Broker.Launch` for that gap.
- **No log rotation/retention policy** — `audit_records` grows unbounded.
  Reasonable for now (same posture as `consent_decisions`); a future
  ops-focused slice would need a retention/archival strategy before this
  ships to a high-volume production deployment.
- **`ListByPlugin` has no pagination** — fine for this slice's proof (a
  handful of test records) but would need a `LIMIT`/cursor before any
  admin-UI audit-log viewer is built on top of it.
