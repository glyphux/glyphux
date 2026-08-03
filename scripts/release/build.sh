#!/usr/bin/env bash
# Builds a full cross-platform release of glyphuxd + glyphux (PRD ADR-011:
# "cross-platform install via script... and direct binary", line 1074).
#
# Rebuilds admin-ui from source first (rather than trusting whatever
# internal/adminui/dist/ happens to be checked in) so a release binary's
# embedded admin SPA is reproducibly built from the exact tagged commit,
# not from whatever the last committer's local `npm run build` produced.
#
# Usage:
#   VERSION=0.1.0 scripts/release/build.sh
#   VERSION=v0.1.0 scripts/release/build.sh   (a leading v is normalized away)
#
# VERSION defaults to the repo-root VERSION file (the release contract's
# single source) when unset — a bare MAJOR.MINOR.PATCH with no v prefix, no
# -dirty, no -NN-gHASH suffix. An explicit VERSION may be a bare semver or a
# v-prefixed git tag; a single leading v is stripped before the fail-closed
# guard, so the injected value is ALWAYS bare. If the VERSION file is
# missing or empty (e.g. a tarball checkout with no tags), fall back to
# `git describe` output stripped of the leading v and of the
# -NN-gHASH/-dirty suffixes so the injected value always matches
# ^\d+\.\d+\.\d+$. The single ldflags line below injects VERSION into BOTH
# the glyphuxd display version (main.version) and the kernel's own version
# (pkg/kernel.Version — the value plugin requires.core constraints are
# checked against; Ticket T9 / gap 9 (a)).
#
# Output: dist/<platform-archives> + dist/SHA256SUMS, ready to attach to a
# GitHub Release.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."
repo_root="$(pwd)"

if [ -z "${VERSION:-}" ]; then
	if [ -s VERSION ]; then
		VERSION="$(tr -d '[:space:]' < VERSION)"
	else
		VERSION="$(git describe --tags --always --dirty 2>/dev/null | sed -E 's/^v//; s/-[0-9]+-g[0-9a-f]+$//; s/-dirty$//' || true)"
	fi
fi
# Normalize: a caller may pass the git-tag shape VERSION=v0.2.0; the injected
# value must be BARE MAJOR.MINOR.PATCH (the pkg/kernel.Version invariant), so
# strip a single leading v before the fail-closed guard below.
VERSION="${VERSION#v}"
if ! printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "error: VERSION='${VERSION}' is not a bare MAJOR.MINOR.PATCH (release artifacts must report a semver kernel.Version)" >&2
	exit 1
fi
echo "==> Building glyphux release ${VERSION}"

echo "==> Rebuilding sdk-js (admin-ui depends on it via file:../sdk-js)"
(cd sdk-js && npm ci && npm run build)

echo "==> Rebuilding admin-ui (embedded into glyphuxd via go:embed)"
(cd admin-ui && npm ci && npm run build)

rm -rf dist
mkdir -p dist

# GOOS/GOARCH matrix mirrors .github/workflows/ci.yml's existing build job —
# kept in sync deliberately (see release.yml, which drives this script per
# matrix entry rather than looping here, so CI and this script never drift
# on which platforms are supported).
platforms=(
	"linux amd64"
	"linux arm64"
	"darwin amd64"
	"darwin arm64"
	"windows amd64"
)

ldflags="-s -w -X main.version=${VERSION} -X github.com/glyphux/glyphux/pkg/kernel.Version=${VERSION}"

for platform in "${platforms[@]}"; do
	read -r goos goarch <<<"$platform"
	ext=""
	[ "$goos" = "windows" ] && ext=".exe"

	work="dist/glyphux-${VERSION}-${goos}-${goarch}"
	mkdir -p "$work"

	echo "==> Building ${goos}/${goarch}"
	# modernc.org/sqlite is pure Go (no cgo dependency), so CGO_ENABLED=0
	# cross-compilation is safe and produces a fully static binary on every
	# target, including cross-compiling for darwin/windows from a Linux CI
	# runner.
	GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" \
		-o "$work/glyphuxd${ext}" ./cmd/glyphuxd
	GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" \
		-o "$work/glyphux${ext}" ./cmd/glyphux

	cp README.md "$work/"

	archive_base="dist/glyphux-${VERSION}-${goos}-${goarch}"
	if [ "$goos" = "windows" ]; then
		(cd dist && zip -qr "$(basename "$archive_base").zip" "$(basename "$work")")
	else
		tar -C dist -czf "${archive_base}.tar.gz" "$(basename "$work")"
	fi
	rm -rf "$work"
done

echo "==> Writing checksums"
(cd dist && sha256sum -- *.tar.gz *.zip 2>/dev/null >SHA256SUMS || shasum -a 256 -- *.tar.gz *.zip 2>/dev/null >SHA256SUMS)

echo "==> Done. Release artifacts in $repo_root/dist:"
ls -la dist
