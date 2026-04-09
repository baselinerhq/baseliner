from __future__ import annotations

from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel


class RepoSource(BaseModel):
    type: Literal["local", "github"]
    slug: str
    path: Path | None = None
    pygithub_repo: Any | None = None

    model_config = {"arbitrary_types_allowed": True}
