from __future__ import annotations

from abc import ABC, abstractmethod

from baseliner.models.scope import RepoSource


class Discovery(ABC):
    @abstractmethod
    def discover(self) -> list[RepoSource]:
        """Return a list of RepoSources to scan."""
