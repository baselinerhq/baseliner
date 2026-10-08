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
| `--open-issues` | list, create, edit and close issues; read and create the label | Issues: Read and write | The findings issue is not delivered, and the run exits `2`. |

Rate-limit lookups need no permission.

GitHub also documents that it keeps a new issue's labels only when the
token's user has push access to the repository, and drops them silently
otherwise. baseliner finds its findings issue again by the `baseliner`
label, so an unlabelled issue would be lost and another opened on every run.
To prevent that, a new issue that comes back without the label is closed at
once, with a note saying why. Later runs find that closed issue among the
token's user's own closed issues and refuse to open another until it is
dealt with: reopen and label it, or give the user push access and delete
it. An App token cannot look up its user, so with one this is not
remembered, and a dropped label means an issue opened and closed on each
run.

Each time, that repo's delivery fails, the run exits `2`, and the log says
why. Whether a fine-grained token with Issues: Read and write alone keeps
the label is not yet verified (#133).

When GitHub refuses a fine-grained or App token for a missing permission
("Resource not accessible by …"), baseliner adds the permission GitHub names
to the error message, for example
`HTTP 403: Resource not accessible by personal access token (the token needs administration: read)`.
Wherever that error is reported, in a check's result or a log line, it
names the permission. The workflow listing is the exception: it logs one
warning, which says Actions: Read is the usual cause.

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

Even on its own repository it has limits. A workflow cannot grant it
Administration, so classic branch protection is unreadable and
`default_branch_requires_review` is `unknown` unless a ruleset requires
review. For `ci_present` to see workflow state, grant `actions: read` in
the workflow's `permissions`.

## GitLab

A personal, group or project access token with the **`read_api`** scope
covers everything baseliner reads on GitLab: the group's projects, their
repository trees and files, and branches. It writes nothing there.

The token reads only what its user or bot can see. Give it **Reporter** (or
higher) on the group: GitLab documents that on a self-managed instance a
**Guest** cannot read a private project's code (on gitlab.com a Guest can).
A project the token cannot see at all is not discovered.

## SAML single sign-on

If your organization enforces SAML SSO, a classic token must be authorized
for the organization after it is created, or calls for that organization's
repositories return `403`. A fine-grained token is authorized when it is
created, by choosing the organization as its resource owner.
