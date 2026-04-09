from __future__ import annotations

import logging
import uuid
from datetime import UTC, datetime

from baseliner.checks.registry import CheckRegistry
from baseliner.models.policy import SEVERITY_WEIGHT, Policy, Severity
from baseliner.models.repository import NormalizedRepository
from baseliner.models.result import CheckResult, CheckStatus, RepoResult, RunResult

LOGGER = logging.getLogger(__name__)


class PolicyEngine:
    def __init__(
        self,
        policy: Policy,
        registry: CheckRegistry,
        global_ignore: list[str] | None = None,
        repo_ignores: dict[str, list[str]] | None = None,
    ) -> None:
        self.policy = policy
        self.registry = registry
        self.global_ignore: set[str] = set(global_ignore or [])
        self.repo_ignores: dict[str, list[str]] = repo_ignores or {}

    def run(self, repo: NormalizedRepository) -> RepoResult:
        repo_ignore_set = set(self.repo_ignores.get(repo.slug, []))
        results: list[CheckResult] = []

        for check_def in self.policy.checks:
            if not check_def.enabled:
                continue
            if check_def.id in self.global_ignore or check_def.id in repo_ignore_set:
                LOGGER.debug("Skipping check '%s' for '%s' (ignored)", check_def.id, repo.slug)
                continue

            try:
                check = self.registry.get(check_def.id)
            except KeyError:
                LOGGER.warning("Unknown check id '%s' in policy — skipping", check_def.id)
                continue

            result = check.evaluate(repo)
            result = result.model_copy(update={"severity": check_def.severity.value})
            results.append(result)

        score = self._compute_score(results)
        return RepoResult(
            slug=repo.slug,
            timestamp=datetime.now(tz=UTC),
            score=score,
            results=results,
        )

    def _compute_score(self, results: list[CheckResult]) -> float:
        total_weight = 0
        passed_weight = 0
        for result in results:
            if result.status == CheckStatus.SKIP:
                continue
            try:
                weight = SEVERITY_WEIGHT[Severity(result.severity)]
            except (ValueError, KeyError):
                weight = 1
            total_weight += weight
            if result.status == CheckStatus.PASS:
                passed_weight += weight
        if total_weight == 0:
            return 1.0
        return round(passed_weight / total_weight, 4)

    def run_batch(self, repos: list[NormalizedRepository]) -> RunResult:
        repo_results: list[RepoResult] = []
        for repo in repos:
            try:
                repo_results.append(self.run(repo))
            except Exception:  # noqa: BLE001
                LOGGER.exception("Unhandled error evaluating repo '%s'", repo.slug)

        passed = sum(
            1
            for repo_result in repo_results
            if all(
                check_result.status not in (CheckStatus.FAIL, CheckStatus.ERROR)
                for check_result in repo_result.results
            )
        )
        failed = len(repo_results) - passed

        return RunResult(
            run_id=str(uuid.uuid4()),
            timestamp=datetime.now(tz=UTC),
            total_repos=len(repo_results),
            passed=passed,
            failed=failed,
            repos=repo_results,
        )
