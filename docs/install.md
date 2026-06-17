# Installing baseliner

baseliner ships as a single statically-linked binary — no runtime required.

## Install script (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/scripts/install.sh | bash
```

Installs to `~/.local/bin/baseliner` (override with `BINDIR=`). Pin a version with
`VERSION=v0.1.0`.

In CI or on shared networks, the anonymous "latest release" lookup can be
rate-limited by the GitHub API. Either pin `VERSION=`, or set `GITHUB_TOKEN` so
the lookup is authenticated:

```bash
curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/scripts/install.sh | GITHUB_TOKEN=$TOKEN bash
```

## go install

```bash
go install github.com/baselinerhq/baseliner/cmd/baseliner@latest
```

## Prebuilt archives

Download from the [releases page](https://github.com/baselinerhq/baseliner/releases):
`baseliner_<os>_<arch>.tar.gz` (Linux/macOS) or `.zip` (Windows). Verify against
`checksums.txt`, extract, and place `baseliner` on your `PATH`.

## Homebrew (macOS/Linux)

```bash
brew install baselinerhq/tap/baseliner
```

The formula is published to [`baselinerhq/homebrew-tap`](https://github.com/baselinerhq/homebrew-tap)
by GoReleaser on each tagged release.

## Usage

See [Getting Started](getting-started.md), [Configuration](configuration.md), and
[CLI Reference](cli.md).

```bash
baseliner scan --config baseliner.yaml --format both
```

## Control repo (scheduled scans)

Use [`examples/control-repo-workflow.yml`](../examples/control-repo-workflow.yml) — it
installs the binary via the script above and runs the scan from GitHub Actions.

## Releasing

Tagging `vX.Y.Z` triggers `.github/workflows/release.yml`, which runs GoReleaser
(`.goreleaser.yaml`) to cross-compile Linux/macOS/Windows (amd64/arm64) binaries, archives,
and `checksums.txt`, and publishes a draft GitHub release.
