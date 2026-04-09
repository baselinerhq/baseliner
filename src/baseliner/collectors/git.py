from __future__ import annotations

import logging
from datetime import UTC, datetime
from pathlib import Path

import git as gitpython

from baseliner.models.repository import GitContext, NormalizedRepository
from baseliner.models.scope import RepoSource

LOGGER = logging.getLogger(__name__)


class GitCollector:
    def __init__(self, stale_threshold: int = 90) -> None:
        self.stale_threshold = stale_threshold

    def collect(self, source: RepoSource) -> NormalizedRepository | None:
        repo_path = source.path
        if repo_path is None:
            LOGGER.warning("GitCollector called without source.path for slug=%s", source.slug)
            return None

        resolved_path = repo_path.resolve()
        if not (resolved_path / ".git").exists():
            return None

        try:
            repo = gitpython.Repo(str(resolved_path))
            last_commit_at = repo.head.commit.committed_datetime
            if last_commit_at.tzinfo is None:
                last_commit_at = last_commit_at.replace(tzinfo=UTC)
            else:
                last_commit_at = last_commit_at.astimezone(UTC)

            days_since_commit = (datetime.now(tz=UTC) - last_commit_at).days
            default_branch = self._get_default_branch(repo)
            branches = [branch.name for branch in repo.branches]
        except (gitpython.InvalidGitRepositoryError, gitpython.GitCommandError, ValueError) as exc:
            LOGGER.warning("Failed to collect git context for %s: %s", resolved_path, exc)
            return None

        return NormalizedRepository(
            source_type=source.type,
            slug=source.slug,
            name=self._resolve_name(source, resolved_path),
            git=GitContext(
                default_branch=default_branch,
                last_commit_at=last_commit_at,
                days_since_commit=days_since_commit,
                branches=branches,
                is_stale=days_since_commit > self.stale_threshold,
            ),
        )

    @staticmethod
    def _get_default_branch(repo: gitpython.Repo) -> str | None:
        ref_candidates = ("refs/remotes/origin/HEAD", "origin/HEAD")
        for ref_name in ref_candidates:
            try:
                origin_head = repo.references[ref_name]
                return origin_head.reference.remote_head
            except (AttributeError, IndexError, KeyError, TypeError):
                continue
        return None

    @staticmethod
    def _resolve_name(source: RepoSource, repo_path: Path) -> str:
        if source.path is not None:
            return repo_path.name
        return source.slug.split("/")[-1]
