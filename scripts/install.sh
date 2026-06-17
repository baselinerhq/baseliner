#!/usr/bin/env bash
# Install the baseliner binary from GitHub releases.
#   curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/scripts/install.sh | bash
# Env: VERSION (default: latest), BINDIR (default: $HOME/.local/bin).
set -euo pipefail

REPO="baselinerhq/baseliner"
BINDIR="${BINDIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux | darwin) ;;
  *) echo "unsupported OS: $os (use the .zip release on Windows)" >&2; exit 1 ;;
esac

ver="${VERSION:-latest}"
if [ "$ver" = "latest" ]; then
  ver=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | cut -d'"' -f4)
  [ -n "$ver" ] || { echo "could not resolve latest release tag" >&2; exit 1; }
fi

url="https://github.com/${REPO}/releases/download/${ver}/baseliner_${os}_${arch}.tar.gz"
echo "downloading ${url}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" | tar -xz -C "$tmp"

mkdir -p "$BINDIR"
install -m 0755 "$tmp/baseliner" "$BINDIR/baseliner"
echo "installed baseliner ${ver} to ${BINDIR}/baseliner"
"$BINDIR/baseliner" --version
