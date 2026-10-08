# baseliner

> Portable assessment-as-code for repository fleets: one binary checks every repo
> against a policy **you** write, and reports what it could not see as well as
> what failed — no server, no app.

[![CI](https://github.com/baselinerhq/baseliner/actions/workflows/ci.yml/badge.svg)](https://github.com/baselinerhq/baseliner/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/baselinerhq/baseliner)](https://github.com/baselinerhq/baseliner/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/baselinerhq/baseliner.svg)](https://pkg.go.dev/github.com/baselinerhq/baseliner)
[![Go Report Card](https://goreportcard.com/badge/github.com/baselinerhq/baseliner)](https://goreportcard.com/report/github.com/baselinerhq/baseliner)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

For anyone who has to say what is true across **many repositories**, such as a
written standard, an internal baseline or a compliance control, without standing
up a control plane. baseliner checks each repo against a **policy you write**. It
gives each repo a **0–1 score** over what it could observe and a separate
**coverage** figure for what it could not. It runs against local checkouts or
GitHub orgs and users, ad hoc, in CI, or on a schedule from a control repo, with
nothing more than a token.

```text
repo                                      score  cover   pass   fail    unk
----------------------------------------------------------------------------
baselinerhq/baseliner                      1.00   100%     10      0      0
baselinerhq/.github                        0.61   100%      5      5      0
baselinerhq/homebrew-tap                   0.65   100%      6      4      0
baselinerhq/baseliner-control              0.78   100%      7      3      0
baselinerhq/baselinerhq.github.io          1.00   100%     10      0      0
baselinerhq/baseliner-action               1.00   100%     10      0      0

Critical/high failures:
  baselinerhq/.github
    [HIGH] ci_present: No CI workflow files found
        see https://baselinerhq.github.io/policies#the-built-in-checks
  baselinerhq/homebrew-tap
    [HIGH] ci_present: No CI workflow files found
        see https://baselinerhq.github.io/policies#the-built-in-checks

6 repos scanned — 3 passed, 3 failed
1 private repo(s) hidden from public output.
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

**Today:** the built-in policy has 10 repository checks. They cover README,
LICENSE, CODEOWNERS, CI, dependency-update config, default branch, staleness and
more, each severity-weighted into the score. A check whose evidence cannot be
read reports `unknown`. That lowers coverage and never raises the score. Bring
your own policy to add, drop, or reweight checks. See
**[Writing a custom policy](docs/policies.md)**.

**Since v0.2.6, opt-in:** checks that read what is actually
enforced, not only which files exist:

- `default_branch_requires_review` reads classic branch protection and rulesets
  together;
- `no_exempt_bypass` fails on a ruleset bypass actor in `exempt` mode;
- settings a plan tier makes unreadable report `unknown`, never a pass.

They are off in the default policy; enable them with
[`examples/policies/forge-controls.yaml`](examples/policies/forge-controls.yaml).
Why they matter, with reproductions:
[Your branch protection is not where you think it is](https://cameronbrooks11.github.io/devops/2026/09/12/branch-protection-is-not-where-you-think/).

Results emit as a console table, JSON, or SARIF (for GitHub code scanning), and
`--open-issues` files and closes a findings issue per repo. A privacy guard
keeps private/internal repos out of the output when scanning from a public
context, and is on by default under GitHub Actions. Flags and
exit codes: **[CLI reference](docs/cli.md)**.

## Where baseliner fits

What is enforced on a repository depends on more than the configuration you can
export (see the write-up above). A tool that reads one source, or treats what it
cannot read as passing, reports compliance it has not observed. baseliner's
design point is the opposite: **unobserved is a finding**. That part ships
today, and so, since v0.2.6, do the opt-in forge-control checks.

The neighbours, honestly:

- **OpenSSF Scorecard** — a fixed, security-focused check set with a score. Its
  Branch-Protection check already reads rulesets and bypass actors. Use it for a
  security score.
- **OpenSSF Minder / GitHub Allstar** — configurable and fleet-wide, with
  remediation, run as a server or GitHub App. Use them for enforcement.
- **Repolinter** — configurable repository linting, unscored, archived in 2026.

Rule of thumb: for enforcement with remediation, use Minder; for a security
score, use Scorecard. For a portable answer to "does every repo meet the
standard *we* wrote, and where can't we tell?", use baseliner.

## Documentation

- [Getting Started](docs/getting-started.md)
- [Configuration](docs/configuration.md) · [Writing a custom policy](docs/policies.md)
- [CLI Reference](docs/cli.md) · [Control Repo](docs/control-repo.md) · [Token permissions](docs/token-permissions.md)
- [Roadmap](docs/ROADMAP.md) · [Project history](docs/history/) — the Python → Go migration

## License

MIT. See [LICENSE](LICENSE).
