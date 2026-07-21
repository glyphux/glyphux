# Implementation: package the production release/installer

## Goal

Ship a real, distributable production release of `glyphuxd`/`glyphux`, per
PRD ADR-011 and the Phase-0 roadmap line "Cross-platform install via
script, Homebrew, Scoop, and direct binary": cross-platform binaries plus a
one-line install script, so an operator can go from "nothing" to a running
`glyphuxd` without building from source. Homebrew/Scoop taps and a
containerized/Docker distribution were explicitly deferred (scope
confirmed with the user) — this covers the binary-release + script-install
tier only.

## Owning Contexts

- No CONTEXT.md/ADR change — this is release/build tooling around the
  existing daemon, not a new domain concept. ADR-011 (in
  `docs/glyphux-prd.md`) already specifies the target shape this tooling
  fulfills.

## Status

Completed. Built and smoke-tested directly (not delegated to a background
agent) since this is build/release tooling, not a `/tdd` domain-behavior
slice.

## Current Decisions

- **`CGO_ENABLED=0` cross-compilation is safe** because `modernc.org/sqlite`
  (this repo's SQLite driver, `go.mod`) is pure Go — confirmed before
  writing the build matrix, since a cgo-based driver (`mattn/go-sqlite3`)
  would have made cross-compiling for darwin/windows from one Linux runner
  materially harder (a C cross-toolchain per target).
- **`scripts/release/build.sh` rebuilds `sdk-js` before `admin-ui`.**
  `admin-ui/package.json` depends on `@glyphux/sdk` via `file:../sdk-js`,
  which resolves to `sdk-js/dist` — a gitignored build artifact, not
  committed. A first draft of this script only ran `admin-ui`'s own
  `npm ci && npm run build` and failed at `tsc` with "Property 'ai' does
  not exist on type 'GlyphuxClient'" (the P4.8-era `sdk-js/src/ai.ts`
  additions weren't in the locally-installed `@glyphux/sdk`'s stale
  `dist/`). Caught by actually running the script end-to-end rather than
  reading it and assuming it was correct — fixed by rebuilding `sdk-js`
  first in both `build.sh` and `release.yml`.
- **The release build rebuilds `admin-ui` from source rather than trusting
  the committed `internal/adminui/dist/`.** `dist/` is committed (so a
  plain `go build` works without Node present), but a *release* binary
  should be reproducibly built from the exact tagged commit's source, not
  from whatever the last committer's local `npm run build` happened to
  produce.
- **Version is injected via `-ldflags -X main.version=$VERSION`, not
  hardcoded.** Added a `version` var (default `"dev"`) and a `-version`
  flag to `cmd/glyphuxd/main.go` (mirroring `cmd/glyphux`'s existing
  `version` var/command, which was already ldflag-injectable via its full
  import path). `-version` short-circuits before `config.Load`/
  `bootstrap.Boot` — proven by `TestVersionFlagShortCircuitsBeforeBoot`,
  since a release binary should report its version without needing a data
  dir or a real database available.
- **`.github/workflows/release.yml`'s build matrix is a literal copy of
  `ci.yml`'s existing build job's matrix** (linux/amd64+arm64,
  darwin/amd64+arm64, windows/amd64) — deliberately kept in sync so a
  release never covers a different platform set than CI already verified
  builds successfully on every push.
- **`install.sh` only "places the binary"** — per ADR-011's own framing
  ("OS-specific native installers... become thin shells whose only job is
  'place the binary, launch the daemon, open the browser'"), it does not
  attempt to replicate or bypass the real first-run setup wizard; it prints
  "Run 'glyphuxd' to start the daemon and open the first-run setup wizard"
  and stops.
- **Checksums (`SHA256SUMS`) are verified by `install.sh` before
  extraction**, not just published alongside the archives — an install
  script that downloads and runs a binary without verifying it against a
  published checksum would be a real (if modest) supply-chain gap for a
  "production release."

## Open Questions — resolved

- **Homebrew/Scoop taps?** Deferred — confirmed with the user as
  out-of-scope for this pass; PRD line 1074 lists them but this ticket
  covers the binary+script tier only. A follow-up ticket can add
  `homebrew-glyphux`/a Scoop manifest once a release cadence exists to
  maintain them against.
- **Docker image?** Not requested/not built. `glyphuxd` is a single static
  binary with embedded SQLite (Principle 9: no required cloud/managed
  dependency), so a Docker image is a convenience wrapper, not a
  requirement — left for a future ticket if operators ask for it.

## Files/Modules Changed

- `cmd/glyphuxd/main.go` — `version` var + `-version` flag.
- `cmd/glyphuxd/main_test.go` (new) — `TestVersionFlagShortCircuitsBeforeBoot`.
- `scripts/release/build.sh` (new) — rebuilds `sdk-js`/`admin-ui` from
  source, cross-compiles `glyphuxd`+`glyphux` for 5 platforms, packages
  `.tar.gz`/`.zip` archives + `SHA256SUMS`.
- `scripts/release/install.sh` (new) — detects OS/arch, resolves a GitHub
  Release (latest or pinned via `GLYPHUX_VERSION`), downloads + verifies
  checksum, installs to `$INSTALL_DIR` (default `~/.local/bin`).
- `.github/workflows/release.yml` (new) — triggered on `v*` tag push: runs
  the full test suite, builds the same 5-platform matrix `ci.yml` already
  verifies, packages archives, and publishes them to a GitHub Release via
  `gh release create --generate-notes`.
- `README.md` — new "Install a release" section (curl-pipe-sh one-liner),
  existing from-source quick start relabeled "(build from source)" to
  distinguish the two paths.

## Acceptance Criteria

- [x] `scripts/release/build.sh` runs end-to-end locally and produces 5
      platform archives + `SHA256SUMS`.
- [x] Every produced binary is the correct format for its target
      (`file` confirms ELF/Mach-O/PE32+ as appropriate) — verified for all
      5 platforms.
- [x] The packaged Linux `glyphuxd` binary was extracted from its own
      archive and run for real (not just `go build`/`go test`): checksum
      verified against `SHA256SUMS`, `-version` printed the injected
      version, the daemon booted, `/healthz` returned 200, `/` redirected
      to `/setup`, `/setup` served the real wizard HTML (embedded
      admin-ui), and the process shut down cleanly on SIGTERM.
- [x] `go build ./... && go vet ./... && go test -race ./...` green
      repo-wide after the `cmd/glyphuxd` changes.
- [x] `release.yml`'s platform matrix matches `ci.yml`'s existing matrix
      exactly.

## Risks

- **No live GitHub Release has been cut yet** — this ticket built and
  locally verified the tooling; actually tagging and pushing a version
  (e.g. `v0.1.0`) to trigger `release.yml` for the first real published
  release is a follow-up action, not something this tracking doc can
  self-verify (it depends on GitHub Actions running remotely).
- **No Homebrew/Scoop/Docker distribution** — see Open Questions.
- **`install.sh` has no Windows-native (PowerShell) equivalent** — Windows
  users must download the `.zip` from the Releases page manually; only
  linux/darwin get the curl-pipe-sh one-liner. A `install.ps1` is a
  reasonable future addition, not built here (not requested, and untestable
  without a Windows environment on hand).
