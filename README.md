# baseliner

> A single dependency-free binary that scores your repo fleet against **your own**
> hygiene baseline — no server, no app.

[![CI](https://github.com/baselinerhq/baseliner/actions/workflows/ci.yml/badge.svg)](https://github.com/baselinerhq/baseliner/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/baselinerhq/baseliner)](https://github.com/baselinerhq/baseliner/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/baselinerhq/baseliner.svg)](https://pkg.go.dev/github.com/baselinerhq/baseliner)
[![Go Report Card](https://goreportcard.com/badge/github.com/baselinerhq/baseliner)](https://goreportcard.com/report/github.com/baselinerhq/baseliner)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

For anyone responsible for **many repositories** who wants a hygiene **score**
(plus SARIF and auto-filed issues) without standing up a control plane. baseliner
checks each repo against a **configurable** baseline policy, gives it a **0–1
score**, and runs against local checkouts and/or GitHub org/user scopes — ad hoc,
in CI, or continuously from a control repo with nothing more than a token.

```text
repo                                      score   pass   fail   skip
--------------------------------------------------------------------
baselinerhq/baseliner                      1.00     10      0      0
baselinerhq/baseliner-action               1.00     10      0      0
baselinerhq/baselinerhq.github.io          1.00     10      0      0
baselinerhq/baseliner-control              0.78      7      3      0
baselinerhq/.github                        0.65      6      4      0
baselinerhq/homebrew-tap                   0.65      6      4      0

Critical/high failures:
  baselinerhq/homebrew-tap
    [HIGH] ci_present: No CI workflow files found
        see https://baselinerhq.github.io/policies#the-built-in-checks

6 repos scanned — 3 passed, 3 failed
```

## Quick start

```bash
# Install (Linux/macOS) — to ~/.local/bin
curl -fsSL https://raw.githubusercontent.com/baselinerhq/baseliner/main/scripts/install.sh | bash
# …or: go install github.com/baselinerhq/baseliner/cmd/baseliner@latest
```

```yaml
# baseliner.yaml — scan a GitHub org against the built-in policy
scope:
  github:
    type: org
    name: my-org
    token_env: GITHUB_TOKEN
policy:
  base: default
```

```bash
export GITHUB_TOKEN=<your_pat>
baseliner scan --config baseliner.yaml
```

That's it. Prebuilt archives for Linux/macOS/Windows are on the
[releases page](https://github.com/baselinerhq/baseliner/releases); scanning local
paths needs no token. Full walkthrough in
**[Getting Started](docs/getting-started.md)**.

## What it checks

The built-in policy scores 10 hygiene/governance checks — README, LICENSE,
CODEOWNERS, CI, dependency-update config, default branch, staleness, and more —
each severity-weighted into the 0–1 score. Bring your own policy to add, drop, or
reweight checks. See **[Writing a custom policy](docs/policies.md)**.

Results emit as a console table, JSON, or SARIF (for GitHub code scanning), and
`--open-issues` files and closes a findings issue per repo. A privacy guard
redacts private/internal repos when scanning from a public context. Flags and
exit codes: **[CLI reference](docs/cli.md)**.

## Where baseliner fits

Repo-governance tooling is crowded, and baseliner does not invent a category. The
honest comparison:

- **OSSF Scorecard** — scored, but a fixed, security-specific check set.
- **OpenSSF Minder / GitHub Allstar** — configurable and fleet-wide with
  remediation, but run as a server / GitHub App (heavier to adopt).
- **Repolinter** — closest fit (configurable repo-hygiene linting), but unscored,
  and archived in 2026.

baseliner's niche is the *intersection*: **lightweight (single binary, zero infra),
configurable, scored, and hygiene-first**. The edge is low adoption friction and a
crisp score, not breadth. Rule of thumb: want org-wide supply-chain enforcement
with a control-plane? Use Minder. Want a fixed security score? Use Scorecard. Want
a one-binary answer to "does our fleet meet *our own* baseline, as a number"?
That's this.

## Documentation

- [Getting Started](docs/getting-started.md)
- [Configuration](docs/configuration.md) · [Writing a custom policy](docs/policies.md)
- [CLI Reference](docs/cli.md) · [Control Repo](docs/control-repo.md)
- [Roadmap](docs/ROADMAP.md) · [Project history](docs/history/) — the Python → Go migration

## License

MIT. See [LICENSE](LICENSE).
