from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

from baseliner.models.result import CheckResult, CheckStatus, RepoResult, RunResult
from baseliner.output.json import write_json


def _build_run_result() -> RunResult:
    now = datetime.now(tz=UTC)
    return RunResult(
        run_id="run-123",
        timestamp=now,
        total_repos=1,
        passed=1,
        failed=0,
        repos=[
            RepoResult(
                slug="acme/repo",
                timestamp=now,
                score=1.0,
                results=[
                    CheckResult(
                        check_id="readme_exists",
                        status=CheckStatus.PASS,
                        severity="critical",
                        message=None,
                    )
                ],
            )
        ],
    )


def test_write_json_roundtrip_to_file(tmp_path: Path) -> None:
    result = _build_run_result()
    out_file = tmp_path / "out.json"

    write_json(result, out_file)

    loaded = RunResult.model_validate_json(out_file.read_text(encoding="utf-8"))
    assert loaded.run_id == "run-123"
    assert loaded.total_repos == 1
    assert loaded.repos[0].slug == "acme/repo"
    assert loaded.repos[0].score == 1.0


def test_write_json_to_stdout(capsys) -> None:
    result = _build_run_result()

    write_json(result, None)

    captured = capsys.readouterr()
    loaded = RunResult.model_validate_json(captured.out)
    assert loaded.run_id == "run-123"


def test_write_json_replaces_existing_file_atomically(tmp_path: Path) -> None:
    first = _build_run_result()
    second = first.model_copy(update={"run_id": "run-456"})
    out_file = tmp_path / "out.json"

    write_json(first, out_file)
    write_json(second, out_file)

    loaded = RunResult.model_validate_json(out_file.read_text(encoding="utf-8"))
    assert loaded.run_id == "run-456"
