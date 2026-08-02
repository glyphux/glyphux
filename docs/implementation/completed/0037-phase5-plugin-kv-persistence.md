# Implementation: Phase 5 — persistent plugin KV (SQL-backed ScopedKV)

## Goal

Replace `pkg/sdk`'s process-lifetime `MemoryKVBackend` with a SQL-backed
backend so plugin state survives daemon restarts, exposed through the
unchanged `HostAPI.Store()` surface across all three tiers (in-process wasm
host funcs `kv_get`/`kv_set`/`kv_delete` and RPC `StoreGet`/`Set`/`Delete`
stay byte-identical). Full scope, acceptance criteria, and definition of
done: `docs/specs/phase5-gap-closure-spec.md` Ticket T1.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

In-flight

## Current Decisions

- `KVBackend` interface exported from `pkg/sdk`; implementation lives in new
  `internal/pluginstore` over `internal/db.Queryer` — never `database/sql`
  directly (keeps `pkg/sdk` public free of `internal/` imports).
- Table `plugin_kv(plugin_name TEXT, key TEXT, value BLOB, updated_at TEXT,
  PRIMARY KEY(plugin_name,key))`; upsert on Set; namespacing by
  `manifest.Name`; plain BLOB values, no TTL/size caps (scope decision).
- `MemoryKVBackend` retained as default when `KernelDeps.KV` is nil — all
  existing tests stay byte-identical.
- Migration 19 appended to `cmd/glyphuxd` main.go list; must be re-grepped
  free before landing (repo bitten twice by collisions).

## Open Questions

None open — plain-BLOB/no-cap semantics confirmed in the scope decisions.

## Files/Modules Expected

- `pkg/sdk/host.go` (+host_test.go) — export `KVBackend`; keep
  `MemoryKVBackend`.
- `internal/pluginstore/store.go` (+store_test.go, new).
- `cmd/glyphuxd/main.go` (one line: migration 19).

## Acceptance Criteria

Full given/when/then list in `docs/specs/phase5-gap-closure-spec.md` Ticket
T1. Key proofs: persistence across real-SQLite open->write->close->reopen;
two plugins writing the same key each read only their own value; Delete ->
`(nil,false,nil)`; second Set wins (upsert); real `testdata/kv_guest.wasm`
round-trips its own value through a closed-and-reloaded instance; migration
19 applies with the composite PK and the global-numbering guard test reports
no duplicates.

## Risks

- `pkg/sdk` is public and must not import `internal/db` — interface in sdk,
  impl in `internal/pluginstore` (boundary invariant).
- `MemoryKVBackend` semantics must stay byte-identical for existing tests.
- Migration-number collision on 19 — re-grep free before landing.
