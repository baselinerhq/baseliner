from __future__ import annotations

from abc import ABC, abstractmethod

from baseliner.models.repository import NormalizedRepository
from baseliner.models.scope import RepoSource


class Collector(ABC):
    @abstractmethod
    def collect(self, source: RepoSource) -> NormalizedRepository:
        """Collect context from a repository source and return a NormalizedRepository."""
