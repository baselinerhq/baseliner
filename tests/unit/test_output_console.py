from __future__ import annotations

from datetime import UTC, datetime

from baseliner.models.result import CheckResult, CheckStatus, RepoResult, RunResult
from baseliner.output.console import print_summary


def test_print_summary_smoke_and_content(capsys) -> None:
    now = datetime.now(tz=UTC)
    result = RunResult(
        run_id="run-console",
        timestamp=now,
        total_repos=2,
        passed=1,
        failed=1,
        repos=[
            RepoResult(
                slug="acme/passing-repo",
                timestamp=now,
                score=1.0,
                results=[
                    CheckResult(
                        check_id="readme_exists",
                        status=CheckStatus.PASS,
                        severity="critical",
                    )
                ],
            ),
            RepoResult(
                slug="acme/failing-repo",
                timestamp=now,
                score=0.2,
                results=[
                    CheckResult(
                        check_id="ci_present",
                        status=CheckStatus.FAIL,
                        severity="high",
                        message="No CI workflow files found",
                    )
                ],
            ),
        ],
    )

    print_summary(result)

    output = capsys.readouterr().out
    assert "acme/passing-repo" in output
    assert "acme/failing-repo" in output
    assert "Critical/high failures:" in output
    assert "2 repos scanned" in output
