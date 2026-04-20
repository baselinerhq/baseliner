from __future__ import annotations

import logging
import time
from datetime import UTC, datetime

from github import Auth, Github, GithubException, UnknownObjectException
from github.Repository import Repository as GithubRepo

from baseliner.models.result import CheckStatus, RepoResult

LOGGER = logging.getLogger(__name__)

ISSUE_TITLE = "[baseliner] baseline compliance findings"
ISSUE_LABEL = "baseliner"
LABEL_COLOR = "0075ca"


class GitHubIssueAction:
    def __init__(self, token: str, dry_run: bool = False) -> None:
        self.g = Github(auth=Auth.Token(token))
        self.dry_run = dry_run

    def run(self, repo_result: RepoResult, pygithub_repo: GithubRepo) -> None:
        """Open or update a baseliner findings issue in the target repo."""
        label = self._ensure_label(pygithub_repo)
        body = self._build_body(repo_result)
        existing = self._find_existing_issue(pygithub_repo)

        if existing:
            if self.dry_run:
                LOGGER.info(
                    "[dry-run] Would update issue #%d in '%s'",
                    existing.number,
                    repo_result.slug,
                )
                return
            existing.edit(body=body)
            LOGGER.info("Updated issue #%d in '%s'", existing.number, repo_result.slug)
        else:
            if self.dry_run:
                LOGGER.info("[dry-run] Would create issue in '%s'", repo_result.slug)
                return
            issue = pygithub_repo.create_issue(
                title=ISSUE_TITLE,
                body=body,
                labels=[label] if label is not None else [],
            )
            LOGGER.info("Created issue #%d in '%s'", issue.number, repo_result.slug)

        # GitHub recommends a minimum 1s spacing between mutative API calls.
        time.sleep(1.1)

    def _ensure_label(self, repo: GithubRepo):
        try:
            return repo.get_label(ISSUE_LABEL)
        except UnknownObjectException:
            pass

        if self.dry_run:
            LOGGER.debug("[dry-run] Would create label '%s' in '%s'", ISSUE_LABEL, repo.full_name)
            return None

        try:
            return repo.create_label(
                name=ISSUE_LABEL,
                color=LABEL_COLOR,
                description="baseliner findings",
            )
        except GithubException as exc:
            LOGGER.warning("Could not create label in '%s': %s", repo.full_name, exc)
            return None

    def _find_existing_issue(self, repo: GithubRepo):
        try:
            for issue in repo.get_issues(state="open", labels=[ISSUE_LABEL]):
                if issue.title == ISSUE_TITLE:
                    return issue
        except GithubException as exc:
            LOGGER.warning("Could not search issues in '%s': %s", repo.full_name, exc)
        return None

    def _build_body(self, result: RepoResult) -> str:
        timestamp = datetime.now(tz=UTC).strftime("%Y-%m-%d %H:%M UTC")
        score_pct = f"{result.score * 100:.0f}%"

        rows = []
        for check_result in result.results:
            status_icon = {
                CheckStatus.PASS: "✅",
                CheckStatus.FAIL: "❌",
                CheckStatus.SKIP: "⏭️",
                CheckStatus.ERROR: "⚠️",
            }.get(check_result.status, check_result.status.value)
            message = check_result.message or ""
            rows.append(
                f"| `{check_result.check_id}` | "
                f"{status_icon} {check_result.status.value} | "
                f"{check_result.severity} | "
                f"{message} |"
            )

        table = "\n".join(
            [
                "| check | status | severity | message |",
                "|---|---|---|---|",
                *rows,
            ]
        )

        return (
            "## baseliner findings\n\n"
            f"**Score**: {score_pct}  \n"
            f"**Scanned**: {timestamp}\n\n"
            f"{table}\n\n"
            "---\n"
            "*managed by [baseliner](https://github.com/baselinerhq/baseliner)*"
        )
