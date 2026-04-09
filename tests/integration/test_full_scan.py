from __future__ import annotations

from pathlib import Path

from baseliner.checks.registry import build_default_registry
from baseliner.collectors.filesystem import FilesystemCollector
from baseliner.collectors.git import GitCollector
from baseliner.config import PolicyLoader
from baseliner.engine import PolicyEngine
from baseliner.models.result import CheckStatus, RunResult
from baseliner.models.scope import RepoSource
from baseliner.output.json import write_json

FIXTURES_DIR = Path(__file__).parent.parent / "fixtures"
FULL_REPO = (FIXTURES_DIR / "full_repo").resolve()
BARE_REPO = (FIXTURES_DIR / "bare_repo").resolve()


def test_full_local_scan_produces_valid_output(tmp_path: Path) -> None:
    source = RepoSource(type="local", slug=str(FULL_REPO), path=FULL_REPO)
    fs_repo = FilesystemCollector().collect(source)
    git_repo = GitCollector().collect(source)
    repo = fs_repo.model_copy(update={"git": git_repo.git}) if git_repo is not None else fs_repo

    engine = PolicyEngine(
        policy=PolicyLoader().load("default"),
        registry=build_default_registry(),
    )
    run_result = engine.run_batch([repo])

    output_path = tmp_path / "results.json"
    write_json(run_result, output_path)

    assert output_path.exists()
    parsed = RunResult.model_validate_json(output_path.read_text(encoding="utf-8"))
    assert parsed.total_repos == 1
    assert parsed.repos[0].score > 0.0
    assert any(result.status == CheckStatus.PASS for result in parsed.repos[0].results)


def test_bare_repo_scan_all_fail_or_skip() -> None:
    source = RepoSource(type="local", slug=str(BARE_REPO), path=BARE_REPO)
    repo = FilesystemCollector().collect(source)
    engine = PolicyEngine(
        policy=PolicyLoader().load("default"),
        registry=build_default_registry(),
    )
    run_result = engine.run_batch([repo])

    for result in run_result.repos[0].results:
        assert result.status in (CheckStatus.FAIL, CheckStatus.SKIP)
