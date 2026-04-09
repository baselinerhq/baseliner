from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

import git as gitpython
import pytest


@pytest.fixture
def fixtures_dir() -> Path:
    return Path(__file__).parent / "fixtures"


@pytest.fixture
def full_repo_path(fixtures_dir: Path) -> Path:
    return fixtures_dir / "full_repo"


@pytest.fixture
def bare_repo_path(fixtures_dir: Path) -> Path:
    return fixtures_dir / "bare_repo"


@pytest.fixture
def no_git_repo_path(fixtures_dir: Path) -> Path:
    return fixtures_dir / "no_git_repo"


@pytest.fixture
def git_repo(tmp_path: Path) -> Path:
    repo = gitpython.Repo.init(tmp_path)
    (tmp_path / "README.md").write_text("# Test\n", encoding="utf-8")
    repo.index.add(["README.md"])
    actor = gitpython.Actor("Baseliner Test", "baseliner@example.com")
    repo.index.commit("initial commit", author=actor, committer=actor)
    return tmp_path


@pytest.fixture
def stale_repo(tmp_path: Path) -> Path:
    repo = gitpython.Repo.init(tmp_path)
    (tmp_path / "README.md").write_text("# Stale\n", encoding="utf-8")
    repo.index.add(["README.md"])
    stale_date = datetime.now(tz=UTC) - timedelta(days=120)
    git_timestamp = stale_date.strftime("%Y-%m-%d %H:%M:%S %z")
    actor = gitpython.Actor("Baseliner Test", "baseliner@example.com")
    repo.index.commit(
        "old commit",
        author=actor,
        committer=actor,
        author_date=git_timestamp,
        commit_date=git_timestamp,
    )
    return tmp_path
