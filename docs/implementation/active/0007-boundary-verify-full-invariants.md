# Implementation: Expand boundary-verify to its full four invariants

## Goal

PRD §17.2 defines the `boundary-verify` CI gate as enforcing four static
architectural invariants:

1. No plugin/theme code imports kernel internals (only `pkg/sdk` and
   `pkg/contract`).
2. No domain-API implementation exposes raw DB/FS handles across the
   boundary.
3. No theme code path can reach a mutation API.
4. Every capability used is declared in a manifest.

A full audit (2026-07-18) found `internal/boundary` currently only checks a
narrower rule than any of these four: that `database/sql` isn't imported
outside `internal/db` (a proxy for invariant 2, but not the same claim —
it wouldn't catch a domain API returning a raw `*sql.Rows` or a
driver-specific type through an otherwise-typed export). Invariants 1, 3,
and 4 are entirely unenforced. This slice closes that gap as far as today's
codebase allows.

## Owning Contexts

- (no CONTEXT.md yet — core-kernel/CI tooling: `internal/boundary`)

## Status

In-flight

## Current Decisions

- Invariant 1 (no plugin/theme importing kernel internals via anything but
  `pkg/sdk`/`pkg/contract`) and invariant 3 (no theme code reaching a
  mutation API) are **not yet enforceable for real** — no plugin/theme
  system exists until Phase 2/4. Don't invent a fake plugin/theme directory
  to satisfy the checker; instead, write the static-analysis rule now
  (walk import graphs, flag anything under a future `plugins/`/`themes/`
  root importing `internal/*` outside the sanctioned surface) so it's ready
  and tested the day Phase 2 lands, and record in this note that they are
  written-but-currently-vacuously-passing (no such directories exist yet).
  Add a regression test using a synthetic fixture module (a temp directory
  tree faking a plugin importing a kernel package) so the check itself is
  proven to catch a real violation, not just pass because there's nothing
  to check.
- Invariant 4 (every capability used is declared in a manifest) is also a
  Phase-2 concept (no manifest system exists yet) — same treatment: write
  the checker against a manifest schema stub if one is cheap to define now,
  or explicitly defer with a clear comment if manifests don't exist in any
  form yet. Don't block this slice on designing Phase 2's manifest format;
  if there's nothing real to check, say so honestly in the completed
  tracking doc rather than faking a pass.
- Invariant 2 (no raw DB/FS handle leakage) IS enforceable today and is the
  main real deliverable of this slice: extend the existing import-boundary
  walker (or add a sibling check) to also verify no exported function/method
  in `internal/content`, `internal/composition`, `internal/media`,
  `internal/identity` returns or accepts a raw `*sql.DB`, `*sql.Rows`,
  `*sql.Tx`, `os.File`, or similar driver/FS-native type in its signature —
  this can likely be done via `go/packages` + `go/types` inspecting exported
  function signatures, rather than a text/regex walk like the current
  import check.

## Open Questions

- Whether to keep the current `imports_test.go` database/sql-import check as
  a separate rule or fold it into a single richer boundary-verify command —
  prefer keeping it (it already works and is tested) and adding new checks
  alongside it, not rewriting what already works.

## Files/Modules Expected

- `internal/boundary/imports_test.go` (existing, keep)
- `internal/boundary/*.go` (new checks — raw-handle-leakage, plugin-import
  rule, manifest rule) + matching `_test.go` with synthetic fixtures proving
  each check actually catches a violation
- Tracking doc moved to `docs/implementation/completed/` when done, with an
  honest note on what's enforced today vs. written-but-dormant pending
  Phase 2 plugin/manifest infrastructure

## Acceptance Criteria

- [ ] Raw DB/FS handle leakage check implemented and proven (via a
      synthetic fixture, not just "no violations found in today's code") to
      catch a real violation.
- [ ] Plugin-import and manifest-declaration checks are written, tested
      against synthetic fixtures (since no real plugin/manifest code exists
      yet), and documented as dormant-but-ready.
- [ ] `go build/vet/test ./...` green, including the new boundary tests.
- [ ] Existing `TestNoRawSQLOutsideDBPackage` still passes unchanged.
- [ ] Tracking doc moved to completed with an honest, specific account of
      what's real today vs. deferred, not a blanket "done".

## Risks

- Isolated from every other slice in this batch — touches only
  `internal/boundary` — so it's safe to run fully in parallel with
  everything else, including 0006.
- Risk of over-claiming completeness for invariants 1/3/4 when there's
  nothing real to check yet — be explicit in the completed doc that these
  are dormant, not "done", to avoid a future audit re-flagging this as a
  false claim of completeness (exactly the failure mode this whole audit
  round was triggered by).
