// Package release builds the release artifact contract (Ticket T10c, I4):
// the portable per-platform archives the installer consumes. Each archive
// is a deterministic zip containing the platform's binaries (glyphux,
// glyphuxd — .exe on Windows) and a VERSION file whose content is
// single-sourced from the repo-root VERSION file; a SHA256SUMS listing and
// a release-manifest.json (see manifest.go — the signed release metadata
// the installer verifies before trusting any hash) sit next to the
// archives. Determinism mirrors pkg/packagefmt: fixed entry order, no
// timestamps — byte-identical archives across runs.
package release

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// readVersion reads the single-source VERSION file — fail fast when the
// file is missing or empty: a release without a version is not a release.
func readVersion(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("release: read VERSION file %s: %w", path, err)
	}
	v := strings.TrimSpace(string(raw))
	if v == "" {
		return "", fmt.Errorf("release: VERSION file %s is empty", path)
	}
	return v, nil
}

// Package builds one deterministic per-platform archive per Options and
// returns its path. The VERSION file is read and every binary checked
// BEFORE anything is written — a missing binary or missing/empty VERSION
// fails fast and names the offender. The archive's entries are fixed
// order: binaries sorted by name (.exe suffix on Windows), VERSION last.
func Package(ctx context.Context, opts Options) (string, error) {
	version, err := readVersion(opts.VersionFile)
	if err != nil {
		return "", err
	}
	if opts.GOOS == "" || opts.GOARCH == "" {
		return "", fmt.Errorf("release: GOOS (%q) and GOARCH (%q) are required", opts.GOOS, opts.GOARCH)
	}
	if opts.OutDir == "" {
		return "", fmt.Errorf("release: OutDir is required")
	}
	for _, b := range opts.Bins {
		if _, err := os.Stat(b.Path); err != nil {
			return "", fmt.Errorf("release: binary %s (%s): %w", b.Name, b.Path, err)
		}
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return "", fmt.Errorf("release: mkdir %s: %w", opts.OutDir, err)
	}

	sorted := append([]Bin(nil), opts.Bins...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	arch := filepath.Join(opts.OutDir, fmt.Sprintf("glyphux-%s-%s-%s.zip", version, opts.GOOS, opts.GOARCH))
	f, err := os.Create(arch)
	if err != nil {
		return "", fmt.Errorf("release: create %s: %w", arch, err)
	}
	zw := zip.NewWriter(f)
	for _, b := range sorted {
		name := b.Name
		if opts.GOOS == "windows" {
			name += ".exe"
		}
		if err := addFile(zw, name, b.Path); err != nil {
			zw.Close()
			f.Close()
			return "", err
		}
	}
	if err := addBytes(zw, "VERSION", []byte(version)); err != nil {
		zw.Close()
		f.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return "", fmt.Errorf("release: finalize %s: %w", arch, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("release: close %s: %w", arch, err)
	}
	return arch, nil
}

// zipEntry opens one deterministic entry: Store method (no compression
// variance), a fixed mode, and a zero modification time (the zip DOS epoch)
// — no timestamps anywhere in the header, so two packaging runs of the
// same inputs are byte-identical (the same determinism discipline
// pkg/packagefmt applies to its containers).
func zipEntry(zw *zip.Writer, name string, mode os.FileMode) (io.Writer, error) {
	fh := &zip.FileHeader{Name: name, Method: zip.Store}
	fh.SetMode(mode)
	fh.Modified = time.Time{}
	return zw.CreateHeader(fh)
}

func addFile(zw *zip.Writer, name, path string) error {
	w, err := zipEntry(zw, name, 0o755)
	if err != nil {
		return fmt.Errorf("release: zip entry %s: %w", name, err)
	}
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("release: open %s: %w", path, err)
	}
	defer src.Close()
	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("release: copy %s: %w", path, err)
	}
	return nil
}

func addBytes(zw *zip.Writer, name string, data []byte) error {
	w, err := zipEntry(zw, name, 0o644)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

// WriteSHA256SUMS writes a SHA256SUMS file listing every archive in outDir
// with its sha256 hex digest (sha256sum format — "<hex>  <name>", sorted
// by name, fixed order). Returns the SHA256SUMS path.
func WriteSHA256SUMS(outDir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(outDir, "*.zip"))
	if err != nil {
		return "", fmt.Errorf("release: glob %s: %w", outDir, err)
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, n := range names {
		sum, err := sha256File(filepath.Join(outDir, n))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "%s  %s\n", sum, n)
	}
	path := filepath.Join(outDir, "SHA256SUMS")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("release: write %s: %w", path, err)
	}
	return path, nil
}

func sha256File(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("release: hash %s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
