# Implementation: Phase 5 — audit activation + item-level CRUD auditing

## Goal

Take audit logging live in a real daemon: `KernelDeps.Audit` is wired so
every loaded plugin's `HostAPI` boundary gates (register/on/emit) and
sensitive events (`user.created`, `payment.*`) log allow/deny, and the
deferred item-level Content/Users/Media CRUD auditing is delivered through
domain-facing recorders persisted with a stable JSON Detail payload, exposed
via an admin-only `GET /api/v0/audit` endpoint. Full scope, acceptance
criteria, and definition of done: `docs/specs/phase5-gap-closure-spec.md`
Ticket T7.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

Completed (merged to dev @ 5531619)

## Current Decisions

- Daemon constructs `audit.NewLogger(db)`; passes it as `KernelDeps.Audit`
  into the T6 loader's shared KernelDeps (boundary-gate + sensitive-event
  logging) and as nil-safe `WithAudit(*audit.Logger)` options on the
  content (6 write sites), media (3), and identity (4) constructors.
- `internal/audit/recorders.go` defines the 13 action constants once and the
  recorders `RecordContent`/`RecordMedia`/`RecordUser`, persisting via
  `Logger.Log` with `Detail=JSON {actor_id, role, item_id, type}`.
- Reads (Get/List/Open) are deliberately un-audited; append-only growth is
  documented (no retention/compaction this round).
- `GET /api/v0/audit?plugin=` admin-only via `ListByPlugin`.

## Open Questions

None open — read auditing, retention/compaction, request-middleware
auditing, plugin-authored records, and the audit admin UI are explicit
non-goals.

## Files/Modules Expected

- `internal/audit/recorders.go` (new) + tests.
- `internal/content/content.go` (+WithAudit opt, 6 sites) + content_test.go.
- `internal/media/media.go` (3 sites) + tests.
- `internal/identity/identity.go` + `users_manage.go` (4 sites) + tests.
- `internal/api/audit.go` (new) + audit_test.go.
- `cmd/glyphuxd/main.go` (logger -> KernelDeps + WithAudit on the three
  constructors).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T7. Key proofs:
denied `RegisterContentType` and `HostAPI.Emit` of sensitive events produce
`audit_records` rows (allowed=false where denied); every item-level write
across content/media/identity produces the expected action row with Detail
containing actor id+role+item id+type; reads write nothing; `WithAudit(nil)`
is a byte-identical no-op with no panic; rows survive a real-SQLite daemon
restart (`ListByPlugin` returns them); 401/403 on the endpoint; boundary-
verify green.

## Risks

- Additive `Option` constructors must keep existing tests compiling and
  behavior byte-identical when nil.
- The Detail payload must stay stable/parseable — it is the audit contract.
- Append-only table growth is unbounded this round; document it (no
  retention/compaction in scope).
