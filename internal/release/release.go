// Package release builds the release artifact contract (Ticket T10c): the
// portable per-platform archives the installer consumes. Each archive is a
// deterministic zip containing the platform's binaries (glyphux, glyphuxd —
// .exe on Windows) and a VERSION file whose content is single-sourced from
// the repo-root VERSION file; a SHA256SUMS listing sits next to the
// archives. Determinism mirrors pkg/packagefmt: fixed entry order, no
// timestamps — byte-identical archives across runs.
//
// RED checkpoint (T10c): the contract types exist; the behaviors are
// stubbed so the RED tests fail for the right reasons.
package release

import (
	"context"
	"errors"
)

// Bin is one binary to package into an archive. Name is the logical entry
// name without extension ("glyphuxd"); Path is the on-disk location of the
// built binary.
type Bin struct {
	Name string
	Path string
}

// Options configures one per-platform archive.
type Options struct {
	// VersionFile is the repo-root VERSION file — the single source of the
	// version string embedded into every archive's VERSION entry.
	VersionFile string
	// GOOS/GOARCH are the target platform; they drive the archive file
	// name and the Windows .exe entry suffix.
	GOOS   string
	GOARCH string
	// Bins are the binaries to package (each exists on disk before
	// packaging — a missing binary is a fail-fast error).
	Bins []Bin
	// OutDir receives the archive (and, later, SHA256SUMS).
	OutDir string
}

// ErrNotImplemented is the RED checkpoint stub error.
var ErrNotImplemented = errors.New("release: not yet implemented (T10c RED)")

// Package builds one deterministic per-platform archive per Options. It
// returns the archive path. Errors: missing/empty VERSION file, missing
// binary, or an unusable output dir — all fail fast.
func Package(ctx context.Context, opts Options) (string, error) {
	return "", ErrNotImplemented
}

// WriteSHA256SUMS writes a SHA256SUMS file listing every archive in outDir
// with its sha256 hex digest (sha256sum format, sorted by name, fixed
// order). Returns the SHA256SUMS path.
func WriteSHA256SUMS(outDir string) (string, error) {
	return "", ErrNotImplemented
}
