// Command glyphux-release builds the release artifact contract (Ticket
// T10c, I4): deterministic per-platform archives (binaries + VERSION) plus
// a SHA256SUMS listing plus a release-manifest.json — and, when a signing
// key is configured (the -sign flag, or GLYPHUX_RELEASE_GPG_KEY /
// GLYPHUX_RELEASE_GPG_HOME), a detached ASCII-armored
// release-manifest.json.sig sealing the exact manifest bytes. The
// installer verifies that signature before trusting the manifest's hashes.
// Driven by .github/workflows/release.yml on version tag push. It consumes
// the binaries the workflow's build-matrix job cross-compiles (ci.yml's
// own GOOS/GOARCH set, ci-style flat names glyphux-<goos>-<goarch>[.exe] /
// glyphuxd-<goos>-<goarch>[.exe]) and packages them; it does not build
// anything itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glyphux/glyphux/internal/release"
	"github.com/glyphux/glyphux/internal/sign"
)

// matrix mirrors .github/workflows/ci.yml's build matrix — a release build
// never covers a different platform set than CI already verified.
var matrix = []struct{ goos, goarch string }{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "arm64"},
	{"darwin", "amd64"},
	{"windows", "amd64"},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "glyphux-release:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("glyphux-release", flag.ContinueOnError)
	dist := fs.String("dist", "dist", "directory with the cross-compiled binaries; archives + SHA256SUMS + release-manifest are written here too")
	versionFile := fs.String("version-file", "VERSION", "repo-root VERSION file (single source of the version)")
	channel := fs.String("channel", "stable", "release channel recorded in release-manifest.json")
	signFlag := fs.Bool("sign", false, "sign release-manifest.json with gpg (also enabled by GLYPHUX_RELEASE_GPG_KEY / GLYPHUX_RELEASE_GPG_HOME)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return build(context.Background(), *dist, *versionFile, *channel, resolveSigner(*signFlag))
}

// resolveSigner returns the signing backend when one is configured: the
// -sign flag or either env var (GLYPHUX_RELEASE_GPG_KEY = gpg key id/email,
// GLYPHUX_RELEASE_GPG_HOME = GNUPGHOME). With none set it returns nil —
// dev mode: the manifest is still emitted, but unsigned (no .sig file).
func resolveSigner(signFlag bool) sign.Signer {
	key := os.Getenv("GLYPHUX_RELEASE_GPG_KEY")
	home := os.Getenv("GLYPHUX_RELEASE_GPG_HOME")
	if !signFlag && key == "" && home == "" {
		return nil
	}
	return sign.GPG{Key: key, HomeDir: home}
}

// build packages every matrix platform into a deterministic zip, writes
// the SHA256SUMS listing, then emits the release manifest (signed when a
// signer is configured). Fail-fast: a missing VERSION file, any missing
// binary, or any manifest/SHA256SUMS divergence aborts the whole run
// before anything is published.
func build(ctx context.Context, dist, versionFile, channel string, signer sign.Signer) error {
	if _, err := os.Stat(versionFile); err != nil {
		return fmt.Errorf("read VERSION file %s: %w", versionFile, err)
	}
	for _, p := range matrix {
		ext := ""
		if p.goos == "windows" {
			ext = ".exe"
		}
		bins := []release.Bin{
			{Name: "glyphux", Path: filepath.Join(dist, fmt.Sprintf("glyphux-%s-%s%s", p.goos, p.goarch, ext))},
			{Name: "glyphuxd", Path: filepath.Join(dist, fmt.Sprintf("glyphuxd-%s-%s%s", p.goos, p.goarch, ext))},
		}
		arch, err := release.Package(ctx, release.Options{
			VersionFile: versionFile,
			GOOS:        p.goos,
			GOARCH:      p.goarch,
			Bins:        bins,
			OutDir:      dist,
		})
		if err != nil {
			return fmt.Errorf("%s/%s: %w", p.goos, p.goarch, err)
		}
		fmt.Printf("packaged %s\n", filepath.Base(arch))
	}
	sums, err := release.WriteSHA256SUMS(dist)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d archives)\n", filepath.Base(sums), len(matrix))
	manifest, sig, err := release.WriteReleaseManifest(ctx, release.ManifestOptions{
		OutDir:      dist,
		VersionFile: versionFile,
		Channel:     channel,
		Signer:      signer,
	})
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", filepath.Base(manifest))
	if sig != "" {
		fmt.Printf("wrote %s\n", filepath.Base(sig))
	}
	return nil
}
