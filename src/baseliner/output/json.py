from __future__ import annotations

import sys
from pathlib import Path

from baseliner.models.result import RunResult


def write_json(result: RunResult, path: Path | None = None) -> None:
    """
    Write RunResult as formatted JSON.
    If path is None, write to stdout.
    If path is provided, write atomically (tmp file + replace).
    """
    content = result.model_dump_json(indent=2)
    if path is None:
        sys.stdout.write(content)
        sys.stdout.write("\n")
        return

    tmp_path = path.with_suffix(path.suffix + ".tmp")
    try:
        tmp_path.write_text(content, encoding="utf-8")
        tmp_path.replace(path)
    except Exception:
        tmp_path.unlink(missing_ok=True)
        raise
