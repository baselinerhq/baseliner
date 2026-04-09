from __future__ import annotations

import typer

from baseliner.models.result import CheckStatus, RunResult


def print_summary(result: RunResult) -> None:
    """Print a per-repo table and a summary of critical/high failures."""
    _print_table(result)
    _print_failures(result)
    _print_footer(result)


def _score_color(score: float) -> str:
    if score >= 0.8:
        return "green"
    if score >= 0.5:
        return "yellow"
    return "red"


def _print_table(result: RunResult) -> None:
    col_slug = 40
    typer.echo(f"{'repo':<{col_slug}}  {'score':>5}  {'pass':>5}  {'fail':>5}  {'skip':>5}")
    typer.echo("-" * (col_slug + 28))
    for repo in result.repos:
        passes = sum(1 for check in repo.results if check.status == CheckStatus.PASS)
        fails = sum(
            1 for check in repo.results if check.status in (CheckStatus.FAIL, CheckStatus.ERROR)
        )
        skips = sum(1 for check in repo.results if check.status == CheckStatus.SKIP)
        score_num = f"{repo.score:5.2f}"
        score_str = typer.style(score_num, fg=_score_color(repo.score))
        slug_display = repo.slug[:col_slug]
        typer.echo(f"{slug_display:<{col_slug}}  {score_str}  {passes:>5}  {fails:>5}  {skips:>5}")


def _print_failures(result: RunResult) -> None:
    high_severity = {"critical", "high"}
    any_printed = False
    for repo in result.repos:
        critical_fails = [
            check
            for check in repo.results
            if check.status == CheckStatus.FAIL and check.severity in high_severity
        ]
        if critical_fails:
            if not any_printed:
                typer.echo("")
                typer.echo("Critical/high failures:")
                any_printed = True
            typer.echo(f"  {repo.slug}")
            for check in critical_fails:
                sev = typer.style(
                    check.severity.upper(), fg="red" if check.severity == "critical" else "yellow"
                )
                message = check.message or "(no message)"
                typer.echo(f"    [{sev}] {check.check_id}: {message}")


def _print_footer(result: RunResult) -> None:
    typer.echo("")
    status = (
        typer.style("failed", fg="green") if result.failed == 0 else typer.style("failed", fg="red")
    )
    typer.echo(
        f"{result.total_repos} repos scanned — {result.passed} passed, {result.failed} {status}"
    )
