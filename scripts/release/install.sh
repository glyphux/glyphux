#!/usr/bin/env sh
# Installs the latest (or a pinned) glyphux release: detects OS/arch,
# downloads the matching release archive + SHA256SUMS from GitHub Releases,
# verifies the checksum, and installs glyphuxd/glyphux into $INSTALL_DIR.
#
# This script is deliberately NOT the setup experience itself (PRD ADR-011:
# "the primary install/first-run experience is a browser-based setup
# wizard served by the glyphuxd daemon itself... the graphical wizard is
# primary; the CLI is for power users") — its only job is "place the
# binary," per ADR-011's own framing of what a native/script installer
# should be responsible for. Running the installed `glyphuxd` opens the
# real first-run wizard.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/glyphux/glyphux/main/scripts/release/install.sh | sh
#   GLYPHUX_VERSION=v0.1.0 sh install.sh     # pin a specific release
#   INSTALL_DIR=/usr/local/bin sh install.sh # override install location
set -eu

repo="glyphux/glyphux"
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
version="${GLYPHUX_VERSION:-latest}"

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
Linux) goos="linux" ;;
Darwin) goos="darwin" ;;
*)
	echo "glyphux install: unsupported OS '$os' — download a release manually from https://github.com/${repo}/releases" >&2
	exit 1
	;;
esac

case "$arch" in
x86_64 | amd64) goarch="amd64" ;;
arm64 | aarch64) goarch="arm64" ;;
*)
	echo "glyphux install: unsupported architecture '$arch' — download a release manually from https://github.com/${repo}/releases" >&2
	exit 1
	;;
esac

if [ "$version" = "latest" ]; then
	api_url="https://api.github.com/repos/${repo}/releases/latest"
else
	api_url="https://api.github.com/repos/${repo}/releases/tags/${version}"
fi

echo "==> Resolving release (${version})"
tag="$(curl -fsSL "$api_url" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
if [ -z "$tag" ]; then
	echo "glyphux install: could not resolve a release tag from ${api_url}" >&2
	exit 1
fi

archive="glyphux-${tag}-${goos}-${goarch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${tag}"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

echo "==> Downloading ${archive} (${tag})"
curl -fsSL -o "${work_dir}/${archive}" "${base_url}/${archive}"
curl -fsSL -o "${work_dir}/SHA256SUMS" "${base_url}/SHA256SUMS"

echo "==> Verifying checksum"
(
	cd "$work_dir"
	expected="$(grep " ${archive}\$" SHA256SUMS | cut -d' ' -f1)"
	if [ -z "$expected" ]; then
		echo "glyphux install: no checksum entry for ${archive} in SHA256SUMS" >&2
		exit 1
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "$archive" | cut -d' ' -f1)"
	else
		actual="$(shasum -a 256 "$archive" | cut -d' ' -f1)"
	fi
	if [ "$expected" != "$actual" ]; then
		echo "glyphux install: checksum mismatch for ${archive} (expected ${expected}, got ${actual})" >&2
		exit 1
	fi
)

echo "==> Extracting"
tar -C "$work_dir" -xzf "${work_dir}/${archive}"

mkdir -p "$install_dir"
extracted_dir="${work_dir}/glyphux-${tag}-${goos}-${goarch}"
mv "${extracted_dir}/glyphuxd" "${install_dir}/glyphuxd"
mv "${extracted_dir}/glyphux" "${install_dir}/glyphux"
chmod +x "${install_dir}/glyphuxd" "${install_dir}/glyphux"

echo "==> Installed glyphuxd and glyphux ${tag} to ${install_dir}"
case ":$PATH:" in
*":${install_dir}:"*) ;;
*) echo "    Add ${install_dir} to your PATH, e.g.: export PATH=\"${install_dir}:\$PATH\"" ;;
esac
echo "==> Run 'glyphuxd' to start the daemon and open the first-run setup wizard."
