# Token permissions

baseliner reads repository metadata through the GitHub API, and writes only
with `--open-issues`. This page lists what each feature calls and the
least a token needs for it, so you can grant no more than you use.

The permissions below come from the endpoints baseliner calls, matched
against GitHub's
[permissions required for fine-grained personal access tokens](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens).
GitHub App installation tokens use the same permission names.

## Fine-grained token or GitHub App

Grant these repository permissions on every repository you scan:

| Feature | API calls | Permission | Without it |
| --- | --- | --- | --- |
| Discovery | list the org's or user's repos | Metadata: Read | Granted to every fine-grained token. |
| File, README and branch checks, repo waivers | contents, README, git blobs, branches | Contents: Read | The directories and README it could not read leave the checks that look there `unknown`. Repo waivers are not read, so the checks run. |
| `ci_present` workflow state | list workflows | Actions: Read | `ci_present` falls back to file presence, so it passes workflows GitHub has disabled. One warning in the run log says so. |
| Platform checks: classic branch protection | branch protection | Administration: Read | Classic protection is unreadable. `default_branch_requires_review` is `unknown` unless a ruleset already requires review. |
| Platform checks: rulesets | branch rules, ruleset | Metadata: Read | — |
| Platform checks: ruleset bypass actors | ruleset (its `bypass_actors`) | Write access to the ruleset | GitHub returns bypass actors only to a caller that can edit the ruleset, though baseliner only reads them. Without that access GitHub leaves them out, and `no_exempt_bypass` is `unknown`. |
| `--open-issues` | search, create, edit and close issues; create the label | Issues: Read and write | The findings issue is not delivered, and the run exits `2`. |

Rate-limit lookups need no permission.

When GitHub refuses a call for a missing permission, baseliner adds the
permission GitHub names to the error message, for example
`HTTP 403: Resource not accessible by personal access token (the token needs administration: read)`.
The message is shown in the run log and in the result of the check it
affected.

## Classic personal access token

A classic token has no per-feature permissions. `repo` covers everything
above for private and public repositories, within what the token's user can
access. Ruleset bypass actors still need that user to have write access to
the ruleset. With `--open-issues` on public repositories only, `public_repo`
is enough.

## The workflow's own `GITHUB_TOKEN`

The token GitHub Actions gives a workflow can read the repository the
workflow runs in and public repositories, nothing private beyond its own.
To scan other private repositories, or open findings issues in other
repositories, give the workflow a fine-grained token or a GitHub App token.

## SAML single sign-on

If your organization enforces SAML SSO, authorize the token for the
organization after creating it, or every call returns `403`.
