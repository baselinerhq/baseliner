from __future__ import annotations

from pathlib import Path

from baseliner.collectors.filesystem import FilesystemCollector
from baseliner.models.scope import RepoSource


def _source_for(path: Path) -> RepoSource:
    resolved = path.resolve()
    return RepoSource(type="local", slug=str(resolved), path=resolved)


def test_filesystem_collector_full_repo(full_repo_path: Path) -> None:
    collector = FilesystemCollector()
    result = collector.collect(_source_for(full_repo_path))

    assert result.fs is not None
    assert result.git is None
    assert result.platform is None
    assert result.fs.key_files["README"] is True
    assert result.fs.key_files["LICENSE"] is True
    assert result.fs.key_files["GITIGNORE"] is True
    assert result.fs.key_files["CODEOWNERS"] is True
    assert ".github/workflows/ci.yml" in result.fs.ci_files
    assert ".github/dependabot.yml" in result.fs.dep_update_files
    assert result.fs.readme_content is not None
    assert result.fs.readme_content.startswith("# Full Repo")


def test_filesystem_collector_bare_repo(bare_repo_path: Path) -> None:
    collector = FilesystemCollector()
    result = collector.collect(_source_for(bare_repo_path))

    assert result.fs is not None
    assert all(not value for value in result.fs.key_files.values())
    assert result.fs.ci_files == []
    assert result.fs.dep_update_files == []
    assert result.fs.readme_content is None


def test_filesystem_collector_no_git_repo(no_git_repo_path: Path) -> None:
    collector = FilesystemCollector()
    result = collector.collect(_source_for(no_git_repo_path))

    assert result.fs is not None
    assert result.fs.key_files["README"] is True
    assert result.fs.key_files["LICENSE"] is True
    assert result.fs.ci_files == []
    assert result.fs.dep_update_files == []
