# Implementation: Phase 0 production-readiness fixes

## Goal

Close the blocking correctness findings from the Phase 0 confirmation review so slices 0.1, 0.3, and 0.5 move from "partial" to complete, per PRD §16 Phase 0 and the §17 production-readiness gate. Fixed via TDD (behavior-first tests), one vertical slice at a time.

## Owning Contexts

- `docs/glyphux-prd.md` §5.1 (layered model / Communication Law), §6 (installation and first-run experience), §16 Phase 0, §17 (production readiness)

## Status

Completed

## Current Decisions

- **DB wizard fix (finding 1):** no-restart design — defer opening the real database until after the wizard collects driver/DSN, rather than requiring an operator restart. Bigger change (restructures `cmd/glyphuxd/main.go` boot flow and the `setup` package), but it's what the finding actually asks for.
- **Bootstrap persistence:** never write secrets to `<data_dir>/glyphux.json`. Persist `{driver, dsn_env_var}` (a pointer to where the DSN lives, e.g. `GLYPHUX_DB_DSN`), never the raw DSN. The operator must set that env var before a restart for Postgres to keep working.
- **Atomicity (finding 2):** admin creation and composition save happen inside one `db.WithTx` — both commit or neither does. Recovery from a crash between DB commit and bootstrap-file write is self-healing: on next boot, if the bootstrap file is missing but `composition.Store.Exists()` is true against the best-guess DB, treat setup as already complete and rewrite the bootstrap file, rather than blindly re-running the wizard (which would hit a duplicate-email error).
- **DB boundary (finding 7):** narrow `db.Queryer`/`db.Row`/`db.Rows`/`db.Result` interfaces plus `db.ErrNoRows`, satisfied structurally by the existing `*sql.Row`/`*sql.Rows`/`sql.Result` — no wrapper types needed. `db.WithTx` hands callers the same `Queryer` surface scoped to a transaction. A `internal/boundary` test forbids `database/sql` imports outside `internal/db` (scoped rule only; full `boundary-verify` tool is separate future work).
- **HTTPS enforcement (finding 3):** `X-Forwarded-Proto` is only trusted when an explicit `TrustProxyHeaders` config flag is set — no implicit trust of proxy headers.
- Scope for this pass: the 7 blocking correctness findings only. The remaining §17 gate items (cold-start/memory benchmarks, signed reproducible releases, deployment/backup/upgrade docs, dedicated lint check) are explicitly deferred — process/infra work, not something a failing test drives.

## Open Questions

- None outstanding. Postgres self-heal (missing bootstrap file after a Postgres setup) is intentionally *not* automatic: without a persisted DSN there's nothing to recover from except the operator re-exporting `GLYPHUX_DB_DSN`, which then flows through the ordinary persisted-config path. Documented as an accepted limitation, not a gap.

## Files/Modules Expected

- `internal/db/db.go` — Queryer/Row/Rows/Result/ErrNoRows/WithTx (done)
- `internal/content/store.go`, `internal/identity/{identity,sessions}.go`, `internal/media/store.go`, `internal/composition/store.go` — off `database/sql`; `identity`/`composition` also gained `*With(ctx, db.Queryer, ...)` variants for transactional use (done)
- `internal/boundary/imports_test.go` — boundary-verify test (done)
- `internal/server/server.go` — HTTP timeouts; `New` now takes a plain `http.Handler` so bootstrap's Gateway can be served the same way as the ordinary route table (done)
- `internal/api/api.go` — `/readyz` readiness endpoint (done)
- `internal/setup/setup.go` — HTTPS enforcement for remote access; `Input`/`Committer` seam; default commit path now atomic via `db.WithTx` (done)
- `internal/config/config.go` — `TrustProxyHeaders` flag (done)
- `cmd/glyphuxd/browser.go` — OpenBrowser wiring via injectable opener seam (done)
- `internal/bootstrap/` (new package) — `Config`/`Load`/`Save` (sanitized persisted choice), `Gateway` (swappable handler, no restart), `committer` (opens Postgres fresh when the wizard picks it, atomic commit, persists config, switches the gateway), `Boot` (explicit-config / persisted-config / virgin-install decision, self-heal) (done)
- `cmd/glyphuxd/main.go` — delegates boot to `internal/bootstrap`; `databaseExplicit` extracted as a tested function (done)

## Acceptance Criteria

- All 7 blocking correctness findings resolved with a failing-then-passing test at a confirmed seam for each. Met.
- `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` all pass (including real-Postgres-gated tests via `GLYPHUX_TEST_POSTGRES_DSN`). Met.
- Wizard can complete setup against Postgres without a daemon restart. Met — verified both by `internal/bootstrap.TestBootPostgresSubmitSwitchesWithoutRestart` and by hand against the compiled binary and a real Postgres container.
- No secret (DSN, password) is ever written to `<data_dir>/glyphux.json`. Met.

## Risks (as encountered)

- The bootstrap module was the largest single change (restructured daemon boot control flow). `internal/server/server_test.go:TestFirstRunLifecycle` and the Postgres-gated equivalents kept passing throughout — no regression.
- A real bug was caught only by end-to-end smoke-testing the compiled binary, not by `go test`: `databaseExplicit`'s first version treated "a DSN happens to be set" as "operator explicitly chose a driver," which made a Postgres restart following the documented recovery step (`export GLYPHUX_DB_DSN=...`, no `GLYPHUX_DB_DRIVER`) silently reopen SQLite instead of Postgres. Fixed to key off the driver alone; regression-covered by `cmd/glyphuxd/databaseexplicit_test.go`. Takeaway: this class of "which code path do I take" boot-sequencing bug is exactly what unit tests miss and a real-binary smoke test catches — worth doing for any future change to `databaseExplicit`/`bootstrap.Boot`'s branch selection.
