from __future__ import annotations

import logging
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from github import GithubException, UnknownObjectException

from baseliner.collectors.base import Collector
from baseliner.collectors.filesystem import (
    detect_ci_files,
    detect_dependency_update_files,
    detect_key_files,
)
from baseliner.models.repository import FilesystemContext, GitContext, NormalizedRepository
from baseliner.models.scope import RepoSource

LOGGER = logging.getLogger(__name__)


class GitHubAPICollector(Collector):
    def __init__(self, stale_threshold: int = 90) -> None:
        self.stale_threshold = stale_threshold

    def collect(self, source: RepoSource) -> NormalizedRepository:
        repo = source.pygithub_repo
        if repo is None:
            LOGGER.warning(
                "GitHubAPICollector called without pygithub_repo for slug=%s", source.slug
            )
            return NormalizedRepository(
                source_type=source.type,
                slug=source.slug,
                name=source.slug.split("/")[-1],
                fs=FilesystemContext(
                    files=[],
                    key_files={
                        "README": False,
                        "LICENSE": False,
                        "GITIGNORE": False,
                        "CODEOWNERS": False,
                    },
                    readme_content=None,
                    ci_files=[],
                    dep_update_files=[],
                ),
                git=GitContext(
                    default_branch=None,
                    last_commit_at=None,
                    days_since_commit=None,
                    branches=[],
                    is_stale=False,
                ),
            )

        root_contents = self._get_contents(repo, "")
        github_contents = self._get_contents(repo, ".github")
        workflows_contents = self._get_contents(repo, ".github/workflows")
        circleci_contents = self._get_contents(repo, ".circleci")

        file_paths = self._extract_file_paths(root_contents)
        file_paths.extend(self._extract_file_paths(github_contents))
        file_paths.extend(self._extract_file_paths(workflows_contents))
        file_paths.extend(self._extract_file_paths(circleci_contents))
        files = sorted(set(file_paths))

        key_files = detect_key_files(files)
        ci_files = detect_ci_files(files)
        dep_update_files = detect_dependency_update_files(files)
        readme_content = self._get_readme_content(repo)

        default_branch = getattr(repo, "default_branch", None)
        last_commit_at = self._normalize_timestamp(getattr(repo, "pushed_at", None))
        days_since_commit = None
        is_stale = False
        if last_commit_at is not None:
            days_since_commit = (datetime.now(tz=UTC) - last_commit_at).days
            is_stale = days_since_commit > self.stale_threshold

        branches = self._get_branch_names(repo)

        return NormalizedRepository(
            source_type=source.type,
            slug=source.slug,
            name=self._resolve_name(source, repo),
            fs=FilesystemContext(
                files=files,
                key_files=key_files,
                readme_content=readme_content,
                ci_files=ci_files,
                dep_update_files=dep_update_files,
            ),
            git=GitContext(
                default_branch=default_branch,
                last_commit_at=last_commit_at,
                days_since_commit=days_since_commit,
                branches=branches,
                is_stale=is_stale,
            ),
        )

    def _get_contents(self, repo: Any, path: str) -> list[Any]:
        try:
            contents = repo.get_contents(path)
        except GithubException as exc:
            if exc.status == 404:
                return []
            LOGGER.warning("GitHub contents lookup failed for '%s': %s", path, exc)
            return []
        if isinstance(contents, list):
            return contents
        return [contents]

    @staticmethod
    def _extract_file_paths(contents: list[Any]) -> list[str]:
        paths: list[str] = []
        for item in contents:
            if getattr(item, "type", None) != "file":
                continue
            item_path = getattr(item, "path", None)
            if isinstance(item_path, str):
                paths.append(item_path)
        return paths

    @staticmethod
    def _normalize_timestamp(timestamp: datetime | None) -> datetime | None:
        if timestamp is None:
            return None
        if timestamp.tzinfo is None:
            return timestamp.replace(tzinfo=UTC)
        return timestamp.astimezone(UTC)

    def _get_readme_content(self, repo: Any) -> str | None:
        try:
            readme = repo.get_readme()
        except UnknownObjectException:
            return None
        except GithubException as exc:
            LOGGER.warning("Failed to fetch README: %s", exc)
            return None

        decoded_content = getattr(readme, "decoded_content", b"")
        if not isinstance(decoded_content, (bytes, bytearray)):
            return None
        return bytes(decoded_content[:4096]).decode("utf-8", errors="replace")

    @staticmethod
    def _get_branch_names(repo: Any) -> list[str]:
        try:
            branch_iterable = repo.get_branches()
        except GithubException as exc:
            LOGGER.warning("Failed to list branches: %s", exc)
            return []

        branch_names: list[str] = []
        for index, branch in enumerate(branch_iterable):
            if index >= 100:
                break
            branch_name = getattr(branch, "name", None)
            if isinstance(branch_name, str):
                branch_names.append(branch_name)
        return branch_names

    @staticmethod
    def _resolve_name(source: RepoSource, repo: Any) -> str:
        repo_name = getattr(repo, "name", None)
        if isinstance(repo_name, str) and repo_name:
            return repo_name
        if source.path is not None:
            return Path(source.path).name
        return source.slug.split("/")[-1]
