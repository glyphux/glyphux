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

Completed

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

- [x] Raw DB/FS handle leakage check implemented and proven (via a
      synthetic fixture, not just "no violations found in today's code") to
      catch a real violation.
- [x] Plugin-import and manifest-declaration checks are written, tested
      against synthetic fixtures (since no real plugin/manifest code exists
      yet), and documented as dormant-but-ready.
- [x] `go build/vet/test ./...` green, including the new boundary tests.
- [x] Existing `TestNoRawSQLOutsideDBPackage` still passes unchanged.
- [x] Tracking doc moved to completed with an honest, specific account of
      what's real today vs. deferred, not a blanket "done".

## Outcome (honest account of what's real vs. dormant)

Four checks now live in `internal/boundary`, one per PRD §17.2 invariant.
**Only invariant 2 is enforced against real code today.** Invariants 1, 3,
and 4 are written and proven against synthetic fixtures, but are dormant —
they have nothing real in this repo to check yet, and that is not the same
claim as "enforced."

- **Invariant 2 — no raw DB/FS handle leakage (ENFORCED TODAY).**
  `internal/boundary/handles.go`'s `CheckRawHandleLeakage` loads
  `internal/content`, `internal/composition`, `internal/media`, and
  `internal/identity` via `go/packages`/`go/types` and inspects every
  exported function/method signature for a raw `*sql.DB`, `*sql.Tx`,
  `*sql.Rows`, `*sql.Row`, `*sql.Stmt`, `*sql.Conn`, or `*os.File` (directly
  or through a pointer/slice/array/map/chan wrapper).
  `TestNoRawHandleLeakageInDomainAPIs` runs it against the real domain-API
  packages and passes today with zero violations.
  `TestRawHandleLeakageChecker_CatchesViolation` proves the checker itself
  works: it builds a standalone synthetic fixture module in a temp dir (not
  checked into the repo — a real, separate `go.mod` so it can import
  `database/sql` freely without tripping the unrelated
  `TestNoRawSQLOutsideDBPackage` walk) with deliberately leaky exported
  functions and a method, and asserts the checker flags exactly those and
  not the clean ones.
- **Invariant 1 — no plugin/theme code importing kernel internals
  (WRITTEN, TESTED, DORMANT).** `internal/boundary/plugin_imports.go`'s
  `CheckPluginKernelImports` walks `plugins/` and `themes/` roots (if they
  exist) and flags any `.go` file importing a
  `github.com/glyphux/glyphux/...` path other than `pkg/sdk`/`pkg/contract`
  (or their subpackages). Neither root exists in this repo (Phase 1 is
  headless-only, no plugin/theme system yet), so
  `TestPluginKernelImportsCheck_VacuousOnRealRepo` explicitly asserts both
  that no such directory exists *and* that the checker reports zero
  violations as a result — documenting dormancy, not claiming enforcement.
  `TestPluginKernelImportsCheck_CatchesViolation` and
  `_ThemesRootAlsoChecked` prove the logic itself against synthetic fixture
  trees built in a temp dir.
- **Invariant 3 — no theme code path reaching a mutation API — NOT
  IMPLEMENTED, genuinely deferred.** Unlike invariants 1/2/4, this one has
  no cheap static proxy without a real plugin/theme call-graph and a real
  notion of "mutation API" surface to reach; inventing one now would be
  exactly the kind of premature, unfalsifiable stub this audit was meant to
  catch. Deferred to Phase 2, alongside the plugin/theme system itself, and
  intentionally left off this slice's Acceptance Criteria checklist scope
  (not silently dropped — recorded here as the one invariant with no code
  in this slice at all).
- **Invariant 4 — every used capability is declared in a manifest
  (WRITTEN, TESTED, DORMANT).** No manifest system or schema exists yet, so
  `internal/boundary/manifest_capabilities.go` defines an explicitly
  provisional `ManifestStub` (`{"capabilities": [...]}`) and a provisional
  capability-request call convention (`RequireCapability("name")`), both
  clearly commented as stand-ins to be replaced when Phase 2 designs the
  real manifest format and `pkg/sdk` capability API.
  `CheckCapabilityDeclarations` walks a code directory's `.go` files via
  `go/ast`, finds `RequireCapability("...")` call sites, and flags any
  capability name not in the manifest's declared set.
  `TestCapabilityDeclarationCheck_VacuousOnRealRepo` documents there's no
  real manifest or plugin/theme code to point this at yet.
  `TestCapabilityDeclarationCheck_CatchesViolation` and
  `_NoUndeclaredUses` prove the logic against synthetic fixtures.

Verification: `go build ./...`, `go vet ./...`, and `go test ./...` are all
green, including the pre-existing `TestNoRawSQLOutsideDBPackage` (kept
unmodified) and all 9 tests in `internal/boundary` (3 old-style + 6 new).
`golang.org/x/tools` (already an indirect dependency via `gqlgen`) was
promoted to a direct `require` for `go/packages`.

**Correction on invariant 3:** the Acceptance Criteria checklist above,
carried over from the original plan, only ever scoped invariants 1, 2, and
4 for "written and tested against synthetic fixtures" treatment — invariant
3 was never assigned that treatment in Current Decisions, and is not
implemented in any form here. Anyone reviewing "done" against PRD §17.2's
four invariants should read this slice as 2 of 4 fully accounted for
(1 enforced, 1 dormant-but-real, 1 dormant-but-real) and invariant 3 as
still entirely open, tracked as follow-up work for whenever Phase 2's
plugin/theme system and mutation-API surface exist to reason about.

## Risks

- Isolated from every other slice in this batch — touches only
  `internal/boundary` — so it's safe to run fully in parallel with
  everything else, including 0006.
- Risk of over-claiming completeness for invariants 1/3/4 when there's
  nothing real to check yet — be explicit in the completed doc that these
  are dormant, not "done", to avoid a future audit re-flagging this as a
  false claim of completeness (exactly the failure mode this whole audit
  round was triggered by).
