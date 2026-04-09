from __future__ import annotations

import fnmatch
import logging
import os

import github

from baseliner.config import AuthError, GitHubScopeConfig
from baseliner.discovery.base import Discovery
from baseliner.models.scope import RepoSource

LOGGER = logging.getLogger(__name__)


class GitHubDiscovery(Discovery):
    def __init__(
        self,
        config: GitHubScopeConfig,
        include: list[str] | None = None,
        exclude: list[str] | None = None,
    ) -> None:
        self.config = config
        self.include = include or []
        self.exclude = exclude or []

    def discover(self) -> list[RepoSource]:
        token = os.environ.get(self.config.token_env, "").strip()
        if not token:
            raise AuthError(
                f"GitHub token not found in environment variable '{self.config.token_env}'. "
                f"Set it with: export {self.config.token_env}=<your_token>"
            )

        client = github.Github(token)
        self._check_rate_limit(client)

        if self.config.type == "org":
            repos = client.get_organization(self.config.name).get_repos()
        else:
            repos = client.get_user(self.config.name).get_repos(type="all")

        sources: list[RepoSource] = []
        for repo in repos:
            repo_name = repo.name
            if self._is_excluded(repo_name):
                LOGGER.debug("Excluding repo '%s' (matched exclude pattern)", repo_name)
                continue
            if not self._is_included(repo_name):
                LOGGER.debug("Skipping repo '%s' (not in include list)", repo_name)
                continue
            sources.append(
                RepoSource(
                    type="github",
                    slug=f"{self.config.name}/{repo_name}",
                    pygithub_repo=repo,
                )
            )
        return sources

    def _is_excluded(self, name: str) -> bool:
        return any(fnmatch.fnmatch(name, pattern) for pattern in self.exclude)

    def _is_included(self, name: str) -> bool:
        if not self.include:
            return True
        return any(fnmatch.fnmatch(name, pattern) for pattern in self.include)

    def _check_rate_limit(self, client: github.Github) -> None:
        try:
            rate = client.get_rate_limit().core
            if rate.remaining < 100:
                LOGGER.warning(
                    "GitHub API rate limit low: %d requests remaining (resets at %s)",
                    rate.remaining,
                    rate.reset.isoformat(),
                )
        except Exception:  # noqa: BLE001
            LOGGER.debug("Could not check rate limit", exc_info=True)
