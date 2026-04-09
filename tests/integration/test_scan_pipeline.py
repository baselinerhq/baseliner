from __future__ import annotations

from pathlib import Path

from baseliner.checks.registry import build_default_registry
from baseliner.collectors.filesystem import FilesystemCollector
from baseliner.collectors.git import GitCollector
from baseliner.config import PolicyLoader
from baseliner.engine import PolicyEngine
from baseliner.models.result import RunResult
from baseliner.models.scope import RepoSource
from baseliner.output.json import write_json


def test_local_scan_pipeline_writes_valid_json(full_repo_path: Path, tmp_path: Path) -> None:
    resolved_path = full_repo_path.resolve()
    source = RepoSource(type="local", slug=str(resolved_path), path=resolved_path)

    fs_repo = FilesystemCollector().collect(source)
    git_repo = GitCollector().collect(source)
    if git_repo is not None:
        normalized_repo = fs_repo.model_copy(update={"git": git_repo.git})
    else:
        normalized_repo = fs_repo

    engine = PolicyEngine(
        policy=PolicyLoader().load("default"),
        registry=build_default_registry(),
    )
    run_result = engine.run_batch([normalized_repo])

    out_file = tmp_path / "results.json"
    write_json(run_result, out_file)

    assert out_file.exists()
    loaded = RunResult.model_validate_json(out_file.read_text(encoding="utf-8"))
    assert loaded.total_repos == 1
    assert loaded.repos[0].score > 0
