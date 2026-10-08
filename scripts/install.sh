#!/usr/bin/env bash
# Install the baseliner binary from GitHub releases.
#   curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/scripts/install.sh | bash
# Env:
#   VERSION       version to install (default: latest)
#   BINDIR        install dir (default: $HOME/.local/bin)
#   GITHUB_TOKEN  optional; authenticates the "latest release" lookup to avoid
#                 anonymous API rate limits (recommended in CI / shared IPs)
set -euo pipefail

REPO="baselinerhq/baseliner"
BINDIR="${BINDIR:-$HOME/.local/bin}"

# resolve_latest prints the latest release tag, retrying transient failures.
# Authenticates with GITHUB_TOKEN when set (anonymous calls are rate-limited on
# shared IPs and in CI). Returns non-zero if it cannot resolve a tag.
resolve_latest() {
  local attempt out auth=()
  [ -n "${GITHUB_TOKEN:-}" ] && auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  for attempt in 1 2 3; do
    out=$(curl -fsSL -H "Accept: application/vnd.github+json" "${auth[@]}" \
      "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
      | grep -m1 '"tag_name"' | cut -d'"' -f4) || true
    if [ -n "$out" ]; then
      printf '%s' "$out"
      return 0
    fi
    sleep "$((attempt * 2))"
  done
  return 1
}

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
  if ! ver=$(resolve_latest); then
    echo "error: could not resolve the latest release tag." >&2
    echo "  GitHub's API may be rate-limiting this network (common in CI / shared IPs)." >&2
    echo "  Fixes: set GITHUB_TOKEN to authenticate, or pin a version with VERSION=vX.Y.Z." >&2
    exit 1
  fi
fi

# sha256 prints the SHA-256 of a file, with whichever tool this system has.
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    echo "error: neither sha256sum nor shasum is available to verify the download" >&2
    return 1
  fi
}

archive="baseliner_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${ver}"
echo "downloading ${base}/${archive}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$archive" "${base}/${archive}"
curl -fsSL -o "$tmp/checksums.txt" "${base}/checksums.txt"

# Verify before extracting: an archive that does not match the release's
# checksums.txt is not installed.
want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
got=$(sha256 "$tmp/$archive")
if [ -z "$want" ] || [ "$got" != "$want" ]; then
  echo "error: ${archive} does not match the checksum in the ${ver} release's checksums.txt; not installing." >&2
  exit 1
fi
echo "verified SHA-256 ${got}"
tar -xzf "$tmp/$archive" -C "$tmp" baseliner

mkdir -p "$BINDIR"
install -m 0755 "$tmp/baseliner" "$BINDIR/baseliner"
echo "installed baseliner ${ver} to ${BINDIR}/baseliner"
"$BINDIR/baseliner" --version
