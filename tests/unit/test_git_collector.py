from __future__ import annotations

from pathlib import Path

from baseliner.collectors.git import GitCollector
from baseliner.models.scope import RepoSource


def _source_for(path: Path) -> RepoSource:
    resolved = path.resolve()
    return RepoSource(type="local", slug=str(resolved), path=resolved)


def test_git_collector_recent_repo(git_repo: Path) -> None:
    collector = GitCollector()
    result = collector.collect(_source_for(git_repo))

    assert result is not None
    assert result.git is not None
    assert result.fs is None
    assert result.platform is None
    assert result.git.default_branch is None
    assert result.git.last_commit_at is not None
    assert result.git.days_since_commit is not None
    assert result.git.is_stale is False


def test_git_collector_stale_repo(stale_repo: Path) -> None:
    collector = GitCollector()
    result = collector.collect(_source_for(stale_repo))

    assert result is not None
    assert result.git is not None
    assert result.git.default_branch is None
    assert result.git.days_since_commit is not None
    assert result.git.days_since_commit >= 120
    assert result.git.is_stale is True


def test_git_collector_without_git(bare_repo_path: Path) -> None:
    collector = GitCollector()
    result = collector.collect(_source_for(bare_repo_path))

    assert result is None
