from __future__ import annotations

import pytest

from baseliner.checks.base import Check
from baseliner.checks.git import DefaultBranchIsMain, StaleRepo
from baseliner.models.repository import GitContext, NormalizedRepository
from baseliner.models.result import CheckStatus


def make_repo(git: GitContext | None) -> NormalizedRepository:
    return NormalizedRepository(source_type="local", slug="/tmp/repo", name="repo", git=git)


def make_git(
    *, default_branch: str | None = "main", is_stale: bool = False, days: int = 5
) -> GitContext:
    return GitContext(
        default_branch=default_branch,
        last_commit_at=None,
        days_since_commit=days,
        branches=["main"],
        is_stale=is_stale,
    )


@pytest.mark.parametrize(
    ("check", "pass_git", "fail_git", "expected_fail_fragment"),
    [
        (
            DefaultBranchIsMain(),
            make_git(default_branch="main", is_stale=False),
            make_git(default_branch="master", is_stale=False),
            "expected 'main'",
        ),
        (
            StaleRepo(),
            make_git(default_branch="main", is_stale=False, days=2),
            make_git(default_branch="main", is_stale=True, days=120),
            "threshold: 90",
        ),
    ],
)
def test_git_checks_pass_fail_skip(
    check: Check, pass_git: GitContext, fail_git: GitContext, expected_fail_fragment: str
) -> None:
    pass_result = check.evaluate(make_repo(pass_git))
    assert pass_result.status == CheckStatus.PASS

    fail_result = check.evaluate(make_repo(fail_git))
    assert fail_result.status == CheckStatus.FAIL
    assert fail_result.message is not None
    assert expected_fail_fragment in fail_result.message

    skip_result = check.evaluate(make_repo(None))
    assert skip_result.status == CheckStatus.SKIP


def test_git_checks_have_expected_ids_and_required_layer() -> None:
    assert DefaultBranchIsMain().check_id == "default_branch_is_main"
    assert DefaultBranchIsMain().required_layer == "git"
    assert StaleRepo().check_id == "stale_repo"
    assert StaleRepo().required_layer == "git"
