from __future__ import annotations

from typing import Annotated

import typer

from baseliner import __version__

app = typer.Typer(name="baseliner", help="Repository fleet baseline compliance engine.")


def _version_callback(value: bool) -> None:
    if value:
        typer.echo(__version__)
        raise typer.Exit()


@app.callback()
def main(
    version: Annotated[
        bool,
        typer.Option(
            "--version", help="Show version and exit.", callback=_version_callback, is_eager=True
        ),
    ] = False,
) -> None:
    # Intentionally empty; command registration happens through decorators.
    _ = version


@app.command()
def scan(
    config: Annotated[
        str,
        typer.Option("--config", help="Path to baseliner configuration file."),
    ] = "baseliner.yaml",
) -> None:
    _ = config
    typer.echo("not yet implemented")
