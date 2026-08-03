// Release manifest (I4) — the machine-readable release description the
// installer consumes, emitted next to SHA256SUMS:
//
//	release-manifest.json   {"version":"0.2.0","channel":"stable",
//	                         "platforms":[{"goos":"windows","goarch":"amd64",
//	                         "zip":"glyphux-0.2.0-windows-amd64.zip",
//	                         "sha256":"<64hex>","size":<bytes>}, ...]}
//	release-manifest.json.sig  detached ASCII-armored signature of the
//	                           manifest bytes — ONLY when a signer is
//	                           configured (dev mode emits no .sig)
//
// Determinism is the same discipline as the archives: platforms sorted by
// goos+goarch, marshaled from a fixed-shape struct (no map iteration), no
// timestamps/randomness — byte-identical across runs. The sha256 values
// are mirrored VERBATIM from SHA256SUMS after re-hashing each zip, so the
// manifest can never diverge from the checksum listing (one source of
// truth); any divergence (missing zip, tampered zip, unlisted zip) fails
// the build rather than emit an inconsistent manifest.
//
// Trust model: the installer verifies release-manifest.json.sig (against a
// pinned public key) before trusting the manifest's sha256 entries, then
// verifies each downloaded zip against those entries — the signature on
// the manifest, not the (unauthenticated) SHA256SUMS fetch, is the root of
// trust. An unsigned manifest in dev mode is explicit: no .sig exists, so
// a verification-capable installer must refuse it.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glyphux/glyphux/internal/sign"
)

const (
	manifestName    = "release-manifest.json"
	manifestSigName = "release-manifest.json.sig"
)

// Manifest is the deterministic release description: version, channel, and
// every platform artifact. Field order is fixed by the struct (marshaled
// with json.MarshalIndent) — never a map, so output is stable.
type Manifest struct {
	Version   string             `json:"version"`
	Channel   string             `json:"channel"`
	Platforms []ManifestPlatform `json:"platforms"`
}

// ManifestPlatform is one per-platform artifact entry.
type ManifestPlatform struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	Zip    string `json:"zip"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// ManifestOptions configures WriteReleaseManifest.
type ManifestOptions struct {
	// OutDir is the dist directory holding the zips and SHA256SUMS.
	OutDir string
	// VersionFile is the repo-root VERSION file — single source of the
	// version string recorded in the manifest.
	VersionFile string
	// Channel is the release channel; empty defaults to "stable".
	Channel string
	// Signer optionally seals the manifest bytes; nil => dev mode (the
	// manifest is emitted, no .sig file).
	Signer sign.Signer
}

// WriteReleaseManifest writes release-manifest.json (and, when a Signer is
// configured, release-manifest.json.sig sealing the exact manifest bytes)
// into opts.OutDir. It returns the manifest path and the signature path
// (empty when no signer). Fail-closed: SHA256SUMS must exist and agree
// with a fresh re-hash of every zip; a zip missing from disk, a zip
// missing from SHA256SUMS, or a sha256 mismatch aborts with an error
// naming the offender — an inconsistent manifest is never emitted.
func WriteReleaseManifest(ctx context.Context, opts ManifestOptions) (manifestPath, sigPath string, err error) {
	version, err := readVersion(opts.VersionFile)
	if err != nil {
		return "", "", err
	}
	if opts.OutDir == "" {
		return "", "", fmt.Errorf("release: OutDir is required")
	}
	channel := opts.Channel
	if channel == "" {
		channel = "stable"
	}

	// Trusted hash source: the SHA256SUMS listing written by
	// WriteSHA256SUMS. The manifest mirrors it verbatim — one source of
	// truth, never a fresh independent computation that could drift.
	sumsRaw, err := os.ReadFile(filepath.Join(opts.OutDir, "SHA256SUMS"))
	if err != nil {
		return "", "", fmt.Errorf("release: read SHA256SUMS in %s (manifest hashes must come from the verified listing): %w", opts.OutDir, err)
	}
	sums := map[string]string{} // zip name -> 64-hex sha256
	for _, ln := range strings.Split(strings.TrimSpace(string(sumsRaw)), "\n") {
		f := strings.Fields(ln)
		if len(f) != 2 {
			return "", "", fmt.Errorf("release: malformed SHA256SUMS line %q", ln)
		}
		sums[f[1]] = f[0]
	}

	// Every zip on disk: derive its platform from the contract name,
	// require a SHA256SUMS entry, and re-hash to prove no divergence.
	matches, err := filepath.Glob(filepath.Join(opts.OutDir, "*.zip"))
	if err != nil {
		return "", "", fmt.Errorf("release: glob %s: %w", opts.OutDir, err)
	}
	platforms := make([]ManifestPlatform, 0, len(matches))
	for _, m := range matches {
		name := filepath.Base(m)
		goos, goarch, err := platformOf(version, name)
		if err != nil {
			return "", "", err
		}
		listed, ok := sums[name]
		if !ok {
			return "", "", fmt.Errorf("release: %s has no SHA256SUMS entry — every zip must be verified before the manifest trusts it", name)
		}
		rehash, err := sha256File(m)
		if err != nil {
			return "", "", err
		}
		if rehash != listed {
			return "", "", fmt.Errorf("release: %s sha256 (%s) differs from SHA256SUMS (%s) — refusing to emit an inconsistent manifest", name, rehash, listed)
		}
		stat, err := os.Stat(m)
		if err != nil {
			return "", "", fmt.Errorf("release: stat %s: %w", name, err)
		}
		platforms = append(platforms, ManifestPlatform{
			GOOS:   goos,
			GOARCH: goarch,
			Zip:    name,
			SHA256: listed,
			Size:   stat.Size(),
		})
	}

	// Every SHA256SUMS entry must exist on disk (a zip removed after the
	// listing was written is a broken release — name it).
	for name := range sums {
		if _, err := os.Stat(filepath.Join(opts.OutDir, name)); err != nil {
			return "", "", fmt.Errorf("release: %s listed in SHA256SUMS but missing from %s: %w", name, opts.OutDir, err)
		}
	}

	// Deterministic order: goos then goarch.
	sort.Slice(platforms, func(i, j int) bool {
		if platforms[i].GOOS != platforms[j].GOOS {
			return platforms[i].GOOS < platforms[j].GOOS
		}
		return platforms[i].GOARCH < platforms[j].GOARCH
	})

	m := Manifest{Version: version, Channel: channel, Platforms: platforms}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("release: marshal manifest: %w", err)
	}
	data = append(data, '\n')

	manifestPath = filepath.Join(opts.OutDir, manifestName)
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return "", "", fmt.Errorf("release: write %s: %w", manifestPath, err)
	}
	if opts.Signer != nil {
		sig, err := opts.Signer.Sign(data)
		if err != nil {
			return "", "", err
		}
		sigPath = filepath.Join(opts.OutDir, manifestSigName)
		if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
			return "", "", fmt.Errorf("release: write %s: %w", sigPath, err)
		}
	}
	return manifestPath, sigPath, nil
}

// platformOf derives (goos, goarch) from a contract artifact name
// glyphux-<version>-<goos>-<goarch>.zip. Version is bare
// MAJOR.MINOR.PATCH and goos/goarch contain no dashes, so a strict 4-field
// split is unambiguous; anything else is not a contract artifact and is
// rejected (fail-closed).
func platformOf(version, name string) (goos, goarch string, err error) {
	base := strings.TrimSuffix(name, ".zip")
	prefix := "glyphux-" + version + "-"
	if !strings.HasPrefix(base, prefix) {
		return "", "", fmt.Errorf("release: %s is not a contract artifact name (want glyphux-<version>-<goos>-<goarch>.zip)", name)
	}
	parts := strings.Split(strings.TrimPrefix(base, prefix), "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("release: cannot derive goos/goarch from %s", name)
	}
	return parts[0], parts[1], nil
}
