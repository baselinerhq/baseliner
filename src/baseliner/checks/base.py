from __future__ import annotations

from abc import ABC, abstractmethod
from typing import Literal

from baseliner.models.repository import NormalizedRepository
from baseliner.models.result import CheckResult, CheckStatus


class Check(ABC):
    check_id: str
    required_layer: Literal["fs", "git", "platform"] | None = None

    def evaluate(self, repo: NormalizedRepository) -> CheckResult:
        if self.required_layer == "fs" and repo.fs is None:
            return CheckResult(
                check_id=self.check_id,
                status=CheckStatus.SKIP,
                severity="unknown",
                message="Filesystem context not available",
            )
        if self.required_layer == "git" and repo.git is None:
            return CheckResult(
                check_id=self.check_id,
                status=CheckStatus.SKIP,
                severity="unknown",
                message="Git context not available",
            )
        if self.required_layer == "platform" and repo.platform is None:
            return CheckResult(
                check_id=self.check_id,
                status=CheckStatus.SKIP,
                severity="unknown",
                message="Platform context not available",
            )
        return self._evaluate(repo)

    @abstractmethod
    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        """Implement the actual check logic. Layer presence is guaranteed by evaluate()."""

    def _pass(self) -> CheckResult:
        return CheckResult(check_id=self.check_id, status=CheckStatus.PASS, severity="unknown")

    def _fail(self, message: str) -> CheckResult:
        return CheckResult(
            check_id=self.check_id,
            status=CheckStatus.FAIL,
            severity="unknown",
            message=message,
        )
