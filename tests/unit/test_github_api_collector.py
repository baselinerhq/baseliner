from __future__ import annotations

from datetime import UTC, datetime, timedelta
from types import SimpleNamespace
from unittest.mock import MagicMock

from github import GithubException, UnknownObjectException

from baseliner.collectors.github_api import GitHubAPICollector
from baseliner.models.scope import RepoSource


def _file(path: str) -> SimpleNamespace:
    return SimpleNamespace(path=path, type="file")


def _dir(path: str) -> SimpleNamespace:
    return SimpleNamespace(path=path, type="dir")


def _build_repo(
    contents_map: dict[str, list[SimpleNamespace]], pushed_days_ago: int = 10
) -> MagicMock:
    repo = MagicMock()
    repo.name = "demo"
    repo.default_branch = "main"
    repo.pushed_at = datetime.now(tz=UTC) - timedelta(days=pushed_days_ago)
    repo.get_branches.return_value = [SimpleNamespace(name="main"), SimpleNamespace(name="feature")]
    repo.get_readme.return_value = SimpleNamespace(decoded_content=b"# Demo\n\nBody")

    def get_contents(path: str) -> list[SimpleNamespace]:
        if path in contents_map:
            return contents_map[path]
        raise GithubException(404, {"message": "Not Found"}, None)

    repo.get_contents.side_effect = get_contents
    return repo


def test_github_api_collector_full_repo() -> None:
    contents = {
        "": [
            _file("README.md"),
            _file("LICENSE"),
            _file(".gitignore"),
            _file("CODEOWNERS"),
            _dir(".github"),
        ],
        ".github": [_dir(".github/workflows"), _file(".github/dependabot.yml")],
        ".github/workflows": [_file(".github/workflows/ci.yml")],
        ".circleci": [],
    }
    repo = _build_repo(contents)
    collector = GitHubAPICollector()
    source = RepoSource(type="github", slug="acme/demo", pygithub_repo=repo)

    result = collector.collect(source)

    assert result.fs is not None
    assert result.git is not None
    assert result.fs.key_files["README"] is True
    assert result.fs.key_files["LICENSE"] is True
    assert result.fs.key_files["GITIGNORE"] is True
    assert result.fs.key_files["CODEOWNERS"] is True
    assert ".github/workflows/ci.yml" in result.fs.ci_files
    assert ".github/dependabot.yml" in result.fs.dep_update_files
    assert result.fs.readme_content is not None
    assert result.fs.readme_content.startswith("# Demo")
    assert result.git.default_branch == "main"
    assert result.git.is_stale is False


def test_github_api_collector_handles_missing_dot_github() -> None:
    contents = {
        "": [_file("README.md"), _file("LICENSE"), _file(".gitignore"), _file("CODEOWNERS")],
    }
    repo = _build_repo(contents)
    collector = GitHubAPICollector()
    source = RepoSource(type="github", slug="acme/demo", pygithub_repo=repo)

    result = collector.collect(source)

    assert result.fs is not None
    assert result.fs.ci_files == []
    assert result.fs.dep_update_files == []


def test_github_api_collector_handles_missing_readme() -> None:
    contents = {
        "": [_file("LICENSE"), _file(".gitignore"), _file("CODEOWNERS")],
        ".github": [],
        ".github/workflows": [],
        ".circleci": [],
    }
    repo = _build_repo(contents)
    repo.get_readme.side_effect = UnknownObjectException(404, {"message": "Not Found"}, None)

    collector = GitHubAPICollector()
    source = RepoSource(type="github", slug="acme/demo", pygithub_repo=repo)
    result = collector.collect(source)

    assert result.fs is not None
    assert result.fs.readme_content is None


def test_github_api_collector_stale_repo() -> None:
    contents = {
        "": [_file("README.md")],
        ".github": [],
        ".github/workflows": [],
        ".circleci": [],
    }
    repo = _build_repo(contents, pushed_days_ago=120)
    collector = GitHubAPICollector(stale_threshold=90)
    source = RepoSource(type="github", slug="acme/demo", pygithub_repo=repo)

    result = collector.collect(source)

    assert result.git is not None
    assert result.git.days_since_commit is not None
    assert result.git.days_since_commit >= 120
    assert result.git.is_stale is True
