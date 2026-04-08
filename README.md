# baseliner

## problem statement

Existing tools that scan repositories for metadata and policy compliance are either narrow in domain (dependency updates, security findings, code quality), tightly coupled to a single hosting platform, or too heavyweight to serve as a foundation for further tooling. There is no simple, platform-flexible engine for scanning a fleet of repositories — local or remote — against a consistent baseline of structural, hygiene, and git-level policies. `baseliner` fills that gap.

## goal

Build a lightweight, extensible engine that can scan any collection of repositories (local directories and/or GitHub-hosted repos) against a configurable baseline policy and produce structured, machine-readable output — with optional side effects such as opening GitHub issues per repo. It is designed to be operated interactively by an individual developer as a CLI tool or scheduled in a control repo for continuous governance, and serves as the foundation for more capable tooling in later versions.

## anti-goals

- **Not a static analysis tool.** It does not inspect source code for bugs, vulnerabilities, or style violations.
- **Not a legal/regulatory compliance tool.** Compliance here means internal operational and process standards within a software development context.
- **Not a code formatter or linter.** It may enforce structural conventions (e.g., required files), but not code-level style rules.
- **Not a per-repo CI/CD check.** It is not run as part of a repository's own pipeline; it lives in its own control repo and runs _across_ repositories.
- **Not a replacement for platform-native enforcement** (e.g., GitHub rulesets, safe-settings). It is a portable inspection and reporting layer, not a platform settings manager.

## scope

### near-term (mvp)

- CLI execution and scheduled GitHub Actions execution from a control repo
- Repo discovery: GitHub API (user or org) with include/exclude overrides, and explicit local path lists
- Filesystem layer checks: file/directory existence and shallow content inspection (e.g., README present and non-empty)
- Git layer checks: default branch name, last commit date, basic activity signals (requires `.git`; gracefully skipped if absent)
- Built-in default policy with per-repo and per-run ignore/override config
- Structured JSON output per repository (check id, status, severity, message, score)
- Optional GitHub issue creation in the target repo summarizing findings (on by default in scheduled/CI mode, off by default in CLI mode)
- PAT-based GitHub authentication

### mid-term (v1.0 - personal / internal use)

- GitHub API metadata layer: branch protection, CI/CD presence, topics, visibility settings, etc.
  - Requires careful evaluation of `safe-settings` and similar tools to determine fit/differentiation (see literature review)
- GitHub App authentication (replaces PAT for org-scale use)
- Expanded policy model: layered/inherited rules, severity-based thresholds, per-policy check configuration
- Deterministic autofix PRs for a defined set of safe remediations (e.g., add README template, add LICENSE)

### long-term (v2.0+ - public release)

- Multi-platform adapters: GitLab, Codeberg, Bitbucket, Azure DevOps
- Suppression/ignore feedback loop (structured in-repo config, e.g., `.baseliner.yaml`)
- Aggregated fleet-level reporting and trend tracking
- AI-assisted remediation suggestions over structured findings output

### out of scope (never to be done in this project)

- Source code static analysis (use Semgrep, CodeQL, SonarQube, etc.)
- Dependency update automation (use Renovate, Dependabot)
- Developer portal / software catalog functionality (use Backstage, Port)
- Platform settings enforcement (use safe-settings, GitHub rulesets)

## state of the art / literature review:

- **Dependabot** is GitHub’s native dependency automation tool. It can raise pull requests for vulnerable dependencies and version updates, and it can be enabled at the repository or organization level. However, its scope is fundamentally **dependency-centric**; it is not a general repository baseline engine for arbitrary metadata, governance, or cross-platform policy evaluation. ([GitHub Docs][1])

- **Renovate** is a broader dependency update bot that can be self-hosted and applied across many repositories. It supports centralized/self-hosted configuration and repository onboarding, so it is closer than Dependabot to a fleet-wide tool. Still, its core model is also **dependency management and update PR generation**, not general-purpose repository inspection and baseline compliance across heterogeneous repository properties. ([docs.renovatebot.com][2])

- **OpenSSF Scorecard** evaluates repositories against a set of software supply-chain and security best practices, and **Scorecard Monitor** extends this to organization-level tracking with Markdown/JSON reports and optional GitHub issue alerts. This is highly relevant because it shows that multi-repository automated policy checking is feasible. However, Scorecard is intentionally focused on **security and supply-chain health**, not a flexible, extensible baseline engine for arbitrary repository metadata and future custom rules. ([undefined][3])

- **Semgrep Managed Scans** supports onboarding repositories in bulk across GitHub, GitLab, Bitbucket, and Azure DevOps, including optional auto-scan of current and future repositories. This makes it strong for large-scale code and security scanning. However, it is centered on **static analysis findings in source code**, rather than building a general internal representation of a repository that can combine local file metadata, git metadata, and host-platform metadata under one baseline policy layer. ([Semgrep][4])

- **SonarQube** provides multi-project aggregation through **Applications** and **Portfolios**, giving higher-level views of code quality and releasability across many repositories. This is useful for organization-level visibility, but it remains primarily a **code quality platform**, not a lightweight, platform-agnostic baseline compliance engine intended as a simple foundation for future tooling. ([Sonar Documentation][5])

- **Codacy** also supports organization-level repository monitoring and dashboards for code quality, security posture, and configuration metrics. Like SonarQube, it provides fleet-level visibility, but it is still oriented toward a hosted code analysis product rather than a simple extensible engine for arbitrary repository baseline policies. ([Codacy Docs][6])

- **GitHub-native governance features** now include organization-wide **rulesets**, which can apply rules to multiple repositories, and **GitHub Code Quality**, which provides repository and organization dashboards plus ruleset-based enforcement. These features show that repository-fleet governance is increasingly being pulled into the hosting platform itself. The limitation is that they are **platform-specific**, whereas the proposed tool aims to keep the core engine independent of GitHub so that adapters for GitLab, Codeberg, or local repositories can be added later. ([GitHub Docs][7])

- **Backstage** and **Port** are relevant at a higher layer. Backstage provides a centralized software catalog built around metadata files stored with code, while Port provides scorecards and rule-based standards tracking across catalog entities. These systems are useful as organizational control planes, but they assume a broader developer portal or catalog model. They are not, by themselves, the simple baseline repository scanner proposed here. ([Backstage][8])

- A closely related existing GitHub-specific project is **safe-settings**, which manages repository settings, branch protections, teams, and related organization policy as code from a central admin repository. This is highly relevant because it demonstrates centralized repository governance as code, but it is focused on **GitHub settings enforcement** rather than general baseline scanning over local structure, git history, and remote host metadata. ([GitHub][9])
  - This should be carefully examined to determine if and how we would integrate with or differentiate from it. It may be that safe-settings could be used as a GitHub adapter for enforcing certain rules related to repository settings, while the core engine remains platform-agnostic and extensible.

[1]: https://docs.github.com/en/code-security/concepts/supply-chain-security/about-dependabot-security-updates "About Dependabot security updates"
[2]: https://docs.renovatebot.com/self-hosted-configuration/ "Self-Hosted configuration options - Renovate documentation"
[3]: https://scorecard.dev/ "OpenSSF Scorecard"
[4]: https://semgrep.dev/docs/deployment/managed-scanning/overview "Semgrep Managed Scans"
[5]: https://docs.sonarsource.com/sonarqube-server/user-guide/applications "Using applications | SonarQube Server"
[6]: https://docs.codacy.com/organizations/managing-repositories/ "Managing repositories"
[7]: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets "About rulesets - GitHub Docs"
[8]: https://backstage.io/docs/features/software-catalog/ "Backstage Software Catalog and Developer Platform"
[9]: https://github.com/github/safe-settings "github/safe-settings"
[10]: https://docs.github.com/en/code-security/concepts/supply-chain-security/about-dependabot-version-updates "About Dependabot version updates"

## conceptual flow (vision)

### input

- **Scope definition** — one of:
  - GitHub API discovery for a user or org, with optional include/exclude list overrides
  - Explicit list of local filesystem paths (git presence optional)
- **Policy definition** — built-in default checks, with optional per-repo or per-run ignore/override config
- **Auth** — PAT for GitHub-sourced repos (GitHub App in v1)

### process

For each repository in scope, collect context in layers — each layer applied only if applicable:

1. **Filesystem layer** (always): directory/file structure, presence and shallow content of key files (e.g., README non-empty, has headings; LICENSE present; `.gitignore` present; CI workflow directory present)
2. **Git layer** (if `.git` present): default branch name, last commit date, stale repo detection, branch list
3. **Remote/platform layer** (v1+, deferred): branch protection rules, CI/CD configuration, repository settings — scope and approach to be determined after evaluating `safe-settings` and related tools

From the collected context, build a normalized internal representation of the repository, then apply the policy engine: each check evaluates the representation and returns a structured result (pass/fail, severity, message).

### output

- **Structured JSON result per repository:**
  ```json
  {
    "repo": "owner/name",
    "timestamp": "...",
    "score": 0.75,
    "results": [
      { "check_id": "readme_exists", "status": "pass" },
      {
        "check_id": "ci_present",
        "status": "fail",
        "severity": "high",
        "message": "No CI workflow found"
      }
    ]
  }
  ```
- **Optional GitHub issue** in the target repo summarizing findings (enabled by default in scheduled/CI mode; disabled by default in CLI mode)
- Output is intentionally machine-readable first — human-readable reporting, autofix PRs, and AI-assisted remediation are downstream consumers built on top of this layer
