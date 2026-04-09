from __future__ import annotations

import logging
from pathlib import Path

from baseliner.discovery.base import Discovery
from baseliner.models.scope import RepoSource

LOGGER = logging.getLogger(__name__)


class LocalDiscovery(Discovery):
    def __init__(self, paths: list[str]) -> None:
        self.paths = [Path(path).expanduser().resolve() for path in paths]

    def discover(self) -> list[RepoSource]:
        sources: list[RepoSource] = []
        for path in self.paths:
            if not path.exists():
                LOGGER.warning("Local path does not exist, skipping: %s", path)
                continue
            if not path.is_dir():
                LOGGER.warning("Local path is not a directory, skipping: %s", path)
                continue
            sources.append(RepoSource(type="local", slug=str(path), path=path))
        return sources
