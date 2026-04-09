from __future__ import annotations

from baseliner.checks.base import Check
from baseliner.models.repository import NormalizedRepository
from baseliner.models.result import CheckResult


class DefaultBranchIsMain(Check):
    check_id = "default_branch_is_main"
    required_layer = "git"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.git is not None
        if repo.git.default_branch == "main":
            return self._pass()
        return self._fail(f"Default branch is '{repo.git.default_branch}', expected 'main'")


class StaleRepo(Check):
    check_id = "stale_repo"
    required_layer = "git"
    threshold_days = 90

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.git is not None
        if repo.git.is_stale is False:
            return self._pass()
        days = repo.git.days_since_commit
        days_text = "unknown" if days is None else str(days)
        return self._fail(
            f"Repository has had no commits in {days_text} days (threshold: {self.threshold_days})"
        )
