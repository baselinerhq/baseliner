from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel


class FilesystemContext(BaseModel):
    files: list[str]
    key_files: dict[str, bool]
    readme_content: str | None
    ci_files: list[str]
    dep_update_files: list[str]


class GitContext(BaseModel):
    default_branch: str | None
    last_commit_at: datetime | None
    days_since_commit: int | None
    branches: list[str]
    is_stale: bool


class PlatformContext(BaseModel):
    """Stub — reserved for v1 GitHub API metadata layer."""


class NormalizedRepository(BaseModel):
    source_type: Literal["local", "github"]
    slug: str
    name: str
    fs: FilesystemContext | None = None
    git: GitContext | None = None
    platform: PlatformContext | None = None
