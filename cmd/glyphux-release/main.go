// Command glyphux-release builds the release artifact contract (Ticket
// T10c): deterministic per-platform archives (binaries + VERSION) plus a
// SHA256SUMS listing, driven by .github/workflows/release.yml on version
// tag push. It consumes the binaries the workflow's build-matrix job
// cross-compiles (ci.yml's own GOOS/GOARCH set, ci-style flat names
// glyphux-<goos>-<goarch>[.exe] / glyphuxd-<goos>-<goarch>[.exe]) and
// packages them; it does not build anything itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glyphux/glyphux/internal/release"
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
	dist := fs.String("dist", "dist", "directory with the cross-compiled binaries; archives + SHA256SUMS are written here too")
	versionFile := fs.String("version-file", "VERSION", "repo-root VERSION file (single source of the version)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return build(context.Background(), *dist, *versionFile)
}

// build packages every matrix platform into a deterministic zip and writes
// the SHA256SUMS listing. Fail-fast: a missing VERSION file or any missing
// binary aborts the whole run before any archive is published.
func build(ctx context.Context, dist, versionFile string) error {
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
	return nil
}
