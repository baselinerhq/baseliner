from __future__ import annotations

from pathlib import Path

import pytest
from typer.testing import CliRunner

import baseliner
from baseliner.cli import app
from baseliner.config import RateLimitError
from baseliner.models.result import RunResult

runner = CliRunner()


def _write_local_config(path: Path, local_repo_path: Path) -> Path:
    config_path = path / "baseliner.yaml"
    config_path.write_text(
        "\n".join(
            [
                "scope:",
                "  local:",
                "    paths:",
                f"      - {local_repo_path.resolve()}",
                "policy:",
                "  base: default",
            ]
        ),
        encoding="utf-8",
    )
    return config_path


def _write_github_config(path: Path) -> Path:
    config_path = path / "baseliner.yaml"
    config_path.write_text(
        "\n".join(
            [
                "scope:",
                "  github:",
                "    type: org",
                "    name: acme",
                "policy:",
                "  base: default",
            ]
        ),
        encoding="utf-8",
    )
    return config_path


def test_missing_config_exits_2(tmp_path: Path) -> None:
    result = runner.invoke(app, ["scan", "--config", str(tmp_path / "missing.yaml")])
    assert result.exit_code == 2
    assert "Error: Config file not found" in result.stderr


def test_version_exits_0() -> None:
    result = runner.invoke(app, ["--version"])
    assert result.exit_code == 0
    assert baseliner.__version__ in result.stdout


def test_scan_exit_0_on_passing_repo(tmp_path: Path, full_repo_path: Path) -> None:
    config_path = _write_local_config(tmp_path, full_repo_path)
    result = runner.invoke(app, ["scan", "--config", str(config_path), "--format", "table"])
    assert result.exit_code == 0


def test_scan_exit_1_on_failing_repo(tmp_path: Path, bare_repo_path: Path) -> None:
    config_path = _write_local_config(tmp_path, bare_repo_path)
    result = runner.invoke(app, ["scan", "--config", str(config_path), "--format", "table"])
    assert result.exit_code == 1


def test_quiet_suppresses_table_and_still_writes_json(
    tmp_path: Path,
    full_repo_path: Path,
) -> None:
    config_path = _write_local_config(tmp_path, full_repo_path)
    output_file = tmp_path / "results.json"
    result = runner.invoke(
        app,
        [
            "scan",
            "--config",
            str(config_path),
            "--format",
            "both",
            "--quiet",
            "--output-file",
            str(output_file),
        ],
    )

    assert result.exit_code == 0
    assert "repos scanned" not in result.stdout
    assert output_file.exists()
    parsed = RunResult.model_validate_json(output_file.read_text(encoding="utf-8"))
    assert parsed.total_repos == 1


def test_rate_limit_error_exits_2(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    config_path = _write_github_config(tmp_path)
    monkeypatch.setenv("GITHUB_TOKEN", "token")
    monkeypatch.setattr(
        "baseliner.discovery.github.GitHubDiscovery.discover",
        lambda self: (_ for _ in ()).throw(
            RateLimitError("Rate limit exceeded. Resets at 2026-04-10T00:00:00Z. Try again later.")
        ),
    )

    result = runner.invoke(app, ["scan", "--config", str(config_path), "--format", "table"])
    assert result.exit_code == 2
    assert "Rate limit exceeded." in result.stderr


def test_unexpected_error_default_hides_traceback(
    tmp_path: Path,
    full_repo_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config_path = _write_local_config(tmp_path, full_repo_path)
    monkeypatch.setattr(
        "baseliner.config.PolicyLoader.load",
        lambda self, base: (_ for _ in ()).throw(RuntimeError("boom")),
    )

    result = runner.invoke(app, ["scan", "--config", str(config_path), "--format", "table"])

    assert result.exit_code == 2
    assert "Unexpected error: RuntimeError: boom" in result.stderr
    assert "Traceback" not in result.stderr


def test_unexpected_error_verbose_shows_traceback(
    tmp_path: Path,
    full_repo_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config_path = _write_local_config(tmp_path, full_repo_path)
    monkeypatch.setattr(
        "baseliner.config.PolicyLoader.load",
        lambda self, base: (_ for _ in ()).throw(RuntimeError("boom")),
    )

    result = runner.invoke(
        app,
        ["scan", "--config", str(config_path), "--format", "table", "--verbose"],
    )

    assert result.exit_code == 2
    assert "Unexpected error: RuntimeError: boom" in result.stderr
    assert "Traceback" in result.stderr


def test_verbose_and_quiet_logs_conflict_message(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "scan",
            "--config",
            str(tmp_path / "missing.yaml"),
            "--verbose",
            "--quiet",
        ],
    )
    assert result.exit_code == 2
    assert "Both --verbose and --quiet given; --verbose wins" in result.stderr
