# Implementation: Phase 5 — WASM resource limits (fuel, memory cap, execution timeout)

## Goal

Close the no-fuel/no-memory-cap/no-execution-timeout gap in
`pkg/runtime/wasm` using wazero's native support, on top of the existing
per-instance private-engine isolation — so a hostile or buggy guest cannot
spin forever, grow memory without bound, or exhaust host CPU. Full scope,
acceptance criteria, and definition of done:
`docs/specs/phase5-gap-closure-spec.md` Ticket T2.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

Completed (merged to dev @ 5531619)

## Current Decisions

- `wasm.New(ctx, checker, opts...)` gains `Limits{MaxMemoryPages uint32,
  Fuel uint64, ExecutionTimeout time.Duration}`; per-instance engine config
  via `WithMemoryLimitPages`/`WithFuel` plus a per-Call context deadline.
- Defaults: memory cap 1024 pages (~64 MiB), execution timeout 30s, fuel 0 =
  unlimited unless configured (documented judgment call, non-breaking).
- New committed fixtures `busy_loop` + `memory_hog` (.rs source + .wasm)
  with the rebuild command documented — repo already commits .wasm fixtures.
- Runtime built without the new options behaves identically to today.

## Open Questions

None open — the default-limits judgment call is recorded in the spec.

## Files/Modules Expected

- `pkg/runtime/wasm/runtime.go`, `runtime_test.go`, `limits_test.go` (new).
- `pkg/runtime/wasm/testdata/busy_loop.{rs,wasm}` +
  `memory_hog.{rs,wasm}` (new committed).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T2. Key proofs:
infinite-loop guest errors under `ExecutionTimeout` while the sibling
instance and host stay unaffected (re-run the trap-isolation pattern);
memory growth beyond `MaxMemoryPages` errors rather than crashing the host;
finite fuel exhausts deterministically; full package suite green with the
new defaults.

## Risks

- Defaults are a judgment call — documented in the spec, kept non-breaking.
- Committing .wasm requires a toolchain — commit binaries + source + rebuild
  command so fixtures stay reproducible.
- Execution-timeout enforcement must not break legitimate long-running
  guests at the 30s default (trap-isolation tests cover the boundary).
