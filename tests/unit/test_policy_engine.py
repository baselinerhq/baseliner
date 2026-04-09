from __future__ import annotations

from pathlib import Path

from baseliner.checks.base import Check
from baseliner.checks.registry import CheckRegistry, build_default_registry
from baseliner.collectors.filesystem import FilesystemCollector
from baseliner.collectors.git import GitCollector
from baseliner.config import PolicyLoader
from baseliner.engine import PolicyEngine
from baseliner.models.policy import CheckDefinition, Policy, Severity
from baseliner.models.repository import NormalizedRepository
from baseliner.models.result import CheckResult, CheckStatus
from baseliner.models.scope import RepoSource


def _collect_local_repo(path: Path) -> NormalizedRepository:
    resolved = path.resolve()
    source = RepoSource(type="local", slug=str(resolved), path=resolved)
    fs_repo = FilesystemCollector().collect(source)
    git_repo = GitCollector().collect(source)
    if git_repo is None:
        return fs_repo
    return fs_repo.model_copy(update={"git": git_repo.git})


def test_default_registry_contains_all_phase2_checks() -> None:
    registry = build_default_registry()
    assert set(registry.all().keys()) == {
        "readme_exists",
        "readme_nonempty",
        "readme_has_heading",
        "license_exists",
        "gitignore_exists",
        "ci_present",
        "codeowners_exists",
        "dependency_update_config",
        "default_branch_is_main",
        "stale_repo",
    }


def test_policy_engine_full_repo_scores_high(full_repo_path: Path) -> None:
    policy = PolicyLoader().load("default")
    engine = PolicyEngine(policy=policy, registry=build_default_registry())
    repo = _collect_local_repo(full_repo_path)

    result = engine.run(repo)
    by_id = {check_result.check_id: check_result for check_result in result.results}

    assert result.score == 1.0
    assert by_id["readme_exists"].status == CheckStatus.PASS
    assert by_id["license_exists"].status == CheckStatus.PASS
    assert by_id["ci_present"].status == CheckStatus.PASS
    assert by_id["default_branch_is_main"].status == CheckStatus.SKIP
    assert by_id["stale_repo"].status == CheckStatus.SKIP


def test_policy_engine_bare_repo_scores_low(bare_repo_path: Path) -> None:
    policy = PolicyLoader().load("default")
    engine = PolicyEngine(policy=policy, registry=build_default_registry())
    repo = _collect_local_repo(bare_repo_path)

    result = engine.run(repo)
    by_id = {check_result.check_id: check_result for check_result in result.results}
    fail_count = sum(
        1 for check_result in result.results if check_result.status == CheckStatus.FAIL
    )

    assert result.score == 0.0
    assert fail_count >= 8
    assert by_id["default_branch_is_main"].status == CheckStatus.SKIP
    assert by_id["stale_repo"].status == CheckStatus.SKIP


def test_policy_engine_global_ignore_excludes_check(full_repo_path: Path) -> None:
    policy = PolicyLoader().load("default")
    engine = PolicyEngine(
        policy=policy,
        registry=build_default_registry(),
        global_ignore=["readme_exists"],
    )
    repo = _collect_local_repo(full_repo_path)

    result = engine.run(repo)
    result_ids = {check_result.check_id for check_result in result.results}
    assert "readme_exists" not in result_ids


def test_policy_engine_repo_ignore_only_affects_target_repo(full_repo_path: Path) -> None:
    policy = PolicyLoader().load("default")
    base_repo = _collect_local_repo(full_repo_path)
    ignored_repo = base_repo.model_copy(update={"slug": "org/ignored"})
    normal_repo = base_repo.model_copy(update={"slug": "org/normal"})

    engine = PolicyEngine(
        policy=policy,
        registry=build_default_registry(),
        repo_ignores={"org/ignored": ["readme_exists"]},
    )

    ignored_result = engine.run(ignored_repo)
    normal_result = engine.run(normal_repo)

    ignored_ids = {check_result.check_id for check_result in ignored_result.results}
    normal_ids = {check_result.check_id for check_result in normal_result.results}
    assert "readme_exists" not in ignored_ids
    assert "readme_exists" in normal_ids


def test_policy_engine_unknown_check_logs_warning_and_continues(
    full_repo_path: Path, caplog
) -> None:
    policy = Policy(
        id="custom",
        checks=[
            CheckDefinition(id="unknown_check", severity=Severity.LOW, enabled=True),
            CheckDefinition(id="readme_exists", severity=Severity.CRITICAL, enabled=True),
        ],
    )
    engine = PolicyEngine(policy=policy, registry=build_default_registry())
    repo = _collect_local_repo(full_repo_path)

    with caplog.at_level("WARNING"):
        result = engine.run(repo)

    assert any(
        "Unknown check id 'unknown_check' in policy" in message for message in caplog.messages
    )
    assert any(check_result.check_id == "readme_exists" for check_result in result.results)


def test_policy_engine_compute_score_formula() -> None:
    engine = PolicyEngine(policy=Policy(id="empty", checks=[]), registry=CheckRegistry())
    results = [
        CheckResult(
            check_id="critical-pass",
            status=CheckStatus.PASS,
            severity=Severity.CRITICAL.value,
        ),
        CheckResult(
            check_id="high-fail",
            status=CheckStatus.FAIL,
            severity=Severity.HIGH.value,
        ),
        CheckResult(
            check_id="low-skip",
            status=CheckStatus.SKIP,
            severity=Severity.LOW.value,
        ),
        CheckResult(
            check_id="medium-error",
            status=CheckStatus.ERROR,
            severity=Severity.MEDIUM.value,
        ),
    ]

    assert engine._compute_score(results) == 0.4444


def test_policy_engine_run_batch_counts_error_as_failed() -> None:
    class ErrorOrPassCheck(Check):
        check_id = "error_or_pass"
        required_layer = None

        def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
            if repo.slug.endswith("/bad"):
                return CheckResult(
                    check_id=self.check_id,
                    status=CheckStatus.ERROR,
                    severity="unknown",
                    message="simulated",
                )
            return self._pass()

    registry = CheckRegistry()
    registry.register(ErrorOrPassCheck())
    policy = Policy(
        id="batch",
        checks=[CheckDefinition(id="error_or_pass", severity=Severity.MEDIUM, enabled=True)],
    )
    engine = PolicyEngine(policy=policy, registry=registry)

    good_repo = NormalizedRepository(source_type="local", slug="org/good", name="good")
    bad_repo = NormalizedRepository(source_type="local", slug="org/bad", name="bad")

    batch_result = engine.run_batch([good_repo, bad_repo])
    assert batch_result.total_repos == 2
    assert batch_result.passed == 1
    assert batch_result.failed == 1


def test_policy_engine_run_batch_exception_creates_error_result() -> None:
    """An unhandled exception in run() must produce an error RepoResult, not a dropped repo."""

    class ExplodingCheck(Check):
        check_id = "exploding_check"
        required_layer = None

        def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
            raise RuntimeError("boom")

    registry = CheckRegistry()
    registry.register(ExplodingCheck())
    policy = Policy(
        id="bang",
        checks=[CheckDefinition(id="exploding_check", severity=Severity.MEDIUM, enabled=True)],
    )
    engine = PolicyEngine(policy=policy, registry=registry)

    repo = NormalizedRepository(source_type="local", slug="org/exploding", name="exploding")

    batch_result = engine.run_batch([repo])
    assert batch_result.total_repos == 1
    assert batch_result.failed == 1
    assert batch_result.passed == 0
    assert batch_result.repos[0].results[0].check_id == "engine_error"
    assert batch_result.repos[0].results[0].status == CheckStatus.ERROR
