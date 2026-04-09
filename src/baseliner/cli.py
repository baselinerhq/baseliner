from __future__ import annotations

import logging
from datetime import UTC, datetime
from enum import StrEnum
from pathlib import Path
from typing import Annotated

import typer

import baseliner

app = typer.Typer(name="baseliner", help="Repository fleet baseline compliance engine.")


class OutputFormat(StrEnum):
    JSON = "json"
    TABLE = "table"
    BOTH = "both"


def _version_callback(value: bool) -> None:
    if value:
        typer.echo(baseliner.__version__)
        raise typer.Exit()


def _configure_logging(verbose: bool, quiet: bool) -> None:
    level = logging.INFO
    if verbose:
        level = logging.DEBUG
    if quiet:
        level = logging.WARNING
    logging.basicConfig(level=level, format="%(levelname)s: %(message)s", force=True)


@app.callback()
def main(
    version: Annotated[
        bool,
        typer.Option(
            "--version", help="Show version and exit.", callback=_version_callback, is_eager=True
        ),
    ] = False,
) -> None:
    _ = version


@app.command()
def scan(
    config: Annotated[
        Path,
        typer.Option("--config", help="Path to baseliner configuration file."),
    ] = Path("baseliner.yaml"),
    output_file: Annotated[
        Path | None,
        typer.Option("--output-file", help="Write JSON output to this file."),
    ] = None,
    fmt: Annotated[
        OutputFormat,
        typer.Option("--format", help="Output format."),
    ] = OutputFormat.BOTH,
    open_issues: Annotated[
        bool,
        typer.Option("--open-issues/--no-issues", help="Open GitHub issues for findings."),
    ] = False,
    dry_run: Annotated[
        bool,
        typer.Option("--dry-run", help="Skip all API write calls; log intent."),
    ] = False,
    verbose: Annotated[
        bool,
        typer.Option("--verbose", help="Enable debug logging."),
    ] = False,
    quiet: Annotated[
        bool,
        typer.Option("--quiet", help="Suppress all output except errors."),
    ] = False,
) -> None:
    """Scan a collection of repositories against the baseline policy."""
    _configure_logging(verbose, quiet)

    from baseliner.checks.registry import build_default_registry
    from baseliner.collectors.filesystem import FilesystemCollector
    from baseliner.collectors.git import GitCollector
    from baseliner.collectors.github_api import GitHubAPICollector
    from baseliner.config import AuthError, ConfigError, PolicyLoader, load_config
    from baseliner.discovery.github import GitHubDiscovery
    from baseliner.discovery.local import LocalDiscovery
    from baseliner.engine import PolicyEngine
    from baseliner.models.result import CheckResult, CheckStatus, RepoResult
    from baseliner.output.console import print_summary
    from baseliner.output.json import write_json

    try:
        cfg = load_config(config)
    except ConfigError as exc:
        typer.echo(f"Error: {exc}", err=True)
        raise typer.Exit(2) from exc

    policy = PolicyLoader().load(cfg.policy.base)
    registry = build_default_registry()
    engine = PolicyEngine(
        policy=policy,
        registry=registry,
        global_ignore=cfg.policy.ignore,
        repo_ignores=cfg.policy.repo_ignores,
    )

    sources = []
    if cfg.scope.github is not None:
        try:
            sources.extend(
                GitHubDiscovery(
                    cfg.scope.github,
                    include=cfg.scope.include,
                    exclude=cfg.scope.exclude,
                ).discover()
            )
        except AuthError as exc:
            typer.echo(f"Auth error: {exc}", err=True)
            raise typer.Exit(2) from exc

    if cfg.scope.local is not None and cfg.scope.local.paths:
        sources.extend(LocalDiscovery(cfg.scope.local.paths).discover())

    if not sources:
        typer.echo("No repositories discovered. Check your scope config.", err=True)
        raise typer.Exit(2)

    fs_collector = FilesystemCollector()
    git_collector = GitCollector()
    github_collector = GitHubAPICollector()

    repos = []
    repo_error_results: list[RepoResult] = []
    for source in sources:
        try:
            if source.type == "github":
                repo = github_collector.collect(source)
            else:
                repo = fs_collector.collect(source)
                git_result = git_collector.collect(source)
                if git_result is not None:
                    repo = repo.model_copy(update={"git": git_result.git})
            repos.append(repo)
        except Exception as exc:  # noqa: BLE001
            logging.getLogger(__name__).warning(
                "Failed to collect repo '%s'", source.slug, exc_info=True
            )
            repo_error_results.append(
                RepoResult(
                    slug=source.slug,
                    timestamp=datetime.now(tz=UTC),
                    score=0.0,
                    results=[
                        CheckResult(
                            check_id="collection_error",
                            status=CheckStatus.ERROR,
                            severity="critical",
                            message=str(exc),
                        )
                    ],
                )
            )

    run_result = engine.run_batch(repos)
    if repo_error_results:
        all_repos = list(run_result.repos) + repo_error_results
        passed = sum(
            1
            for repo_result in all_repos
            if not any(
                check_result.status in (CheckStatus.FAIL, CheckStatus.ERROR)
                for check_result in repo_result.results
            )
        )
        failed = len(all_repos) - passed
        run_result = run_result.model_copy(
            update={
                "repos": all_repos,
                "total_repos": len(all_repos),
                "passed": passed,
                "failed": failed,
            }
        )

    if fmt in (OutputFormat.JSON, OutputFormat.BOTH):
        write_json(run_result, output_file)
    if fmt in (OutputFormat.TABLE, OutputFormat.BOTH) and not quiet:
        print_summary(run_result)

    if open_issues:
        if not dry_run:
            typer.echo("--open-issues: not yet implemented", err=True)
        else:
            typer.echo("[dry-run] --open-issues: would open issues (not yet implemented)")

    if run_result.failed > 0:
        raise typer.Exit(1)
