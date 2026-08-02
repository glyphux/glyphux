# Implementation: Phase 5 — process/build caveats

## Goal

Resolve the four process/build caveats from the handoff: a single source of
truth for the kernel version, a migration-uniqueness guard that prevents the
recurring global-version collisions, an explicit admin-SPA rebuild story with
CI freshness enforcement, and cleanup of the 12 stale `worktree-agent-*`
branches (after ancestor verification). Full scope, acceptance criteria, and
definition of done: `docs/specs/phase5-gap-closure-spec.md` Ticket T9.

## Owning Contexts

- No new CONTEXT.md/ADR — additive wiring across existing owning contexts
  (internal/, pkg/sdk, pkg/runtime, capabilities/); see
  docs/agents/domain.md.

## Status

Completed (merged to dev @ 5531619)

## Current Decisions

- (a) Committed root `VERSION` file (0.2.0) is the single source of truth;
  `pkg/kernel` derives `Version` at build time (`go:embed`) with a
  `-ldflags -X` override for release builds; keep the plain-go-build
  fallback to the committed file.
- (b) New `internal/db/migrations_global_test.go`: a central registry test
  walking every package's `Migrations` slice, failing on any duplicate
  `Version` and naming both colliding packages (repo has been bitten twice).
- (c) justfile/CI target: `npm --prefix admin-ui run build` -> copy to
  `internal/adminui/dist`; CI checks committed dist freshness vs a fresh
  build.
- (d) `worktree-agent-*` branches deleted only after sign-off and after
  verification asserts all 12 tips are ancestors of `dev` with zero unique
  commits; AGENTS.md hygiene note added.

## Open Questions

None open — version auto-tagging in CI is an explicit non-goal.

## Files/Modules Expected

- `VERSION` (new root).
- `pkg/kernel/kernel.go` (+kernel_test.go).
- `scripts/` (version target); `justfile`.
- `internal/db/migrations_global_test.go` (new).
- `.github/workflows/ci.yml` (+dist check).
- `AGENTS.md`/`README` (hygiene note; doc-only).

## Acceptance Criteria

Full list in `docs/specs/phase5-gap-closure-spec.md` Ticket T9. Key proofs:
`go build` => `kernel.Version` equals the VERSION file, ldflags override
wins and `glyphuxd --version` matches, semver parses `MAJOR.MINOR.PATCH`; a
duplicate migration version fails the registry test naming both colliding
packages; stale dist fails the freshness check (CI) or the documented
rebuild target is invoked (manual); after sign-off `git branch -a` shows no
`worktree-agent-*` branches remain, with zero unique commits asserted before
any deletion.

## Risks

- (a) Touches release tooling — keep the plain-go-build fallback to the
  committed VERSION so ordinary builds never depend on ldflags.
- (d) Destructive — human gate before any branch deletion; verification
  asserts zero unique commits first.
- The registry guard test must be deterministic and repo-wide (walk every
  package's `Migrations` slice) or it will give false confidence.
