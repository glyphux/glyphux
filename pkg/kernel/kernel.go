// Package kernel holds the single source of truth for the running kernel's
// own semantic version — the value a plugin manifest's requires.core
// constraint (PRD §7.3) is checked against at HostAPI-construction time
// (PRD §10.2, "runtime enforcement (call time)").
//
// This lives in its own tiny package, separate from pkg/sdk and pkg/contract,
// for two reasons:
//
//  1. It is not the same thing as pkg/contract.Version (a composition
//     CONTRACT layer version, e.g. "content-composition/v0" — independently
//     versioned per layer per §3.2) or a plugin's own Manifest.Version (a
//     PLUGIN's own version). Overloading either package's existing "Version"
//     identifier to also mean "the kernel's own version" would have been
//     genuinely ambiguous to a reader of either package.
//  2. cmd/glyphux/main.go already has its own `version` var, but that is the
//     developer CLI's own display string — an unexported, ldflags-injectable
//     value on a binary the PRD itself calls "a secondary surface... never
//     the install path" (see cmd/glyphux/main.go's package doc). pkg/sdk
//     cannot import cmd/glyphux (a command depends on library packages,
//     never the reverse — cmd/glyphux already imports pkg/contract, not vice
//     versa), and even if it could, that var answers "what does the CLI
//     tool call itself" rather than "what version is the running kernel
//     that just granted this plugin a HostAPI" — a distinct question this
//     package exists to answer as one, greppable, non-scattered constant.
package kernel

// Version is the running kernel's own semantic version. It intentionally
// tracks the kernel's actual current development stage (mid Phase 2 of the
// PRD's multi-phase build), not the PRD document's own "Version: 1.0.0"
// header — the PRD describes where the project is going, not what glyphuxd
// actually is today.
//
// This is a var (not a const) so a release build can ldflags-override it:
// `-ldflags "-X github.com/glyphux/glyphux/pkg/kernel.Version=..."` — the
// same mechanism cmd/glyphuxd uses for its own display `version` var, and
// the one the justfile `release` target and scripts/release/build.sh drive
// from the repo-root VERSION file. The source default is kept in sync with
// that VERSION file; the sync mechanism is the unit test in version_test.go
// (Ticket T9 / gap 9), which asserts the two agree under a plain build.
// Sync deliberately goes through that test rather than a `go:embed` or
// init-time assignment: either of those would overwrite the value at
// program start and clobber the `-ldflags -X` release override (the
// ldflags value lands in the variable before init runs, so an embed-based
// assignment would make every release report the source default).
var Version = "0.2.0"
