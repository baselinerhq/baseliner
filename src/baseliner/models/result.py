from __future__ import annotations

from datetime import datetime
from enum import StrEnum

from pydantic import BaseModel


class CheckStatus(StrEnum):
    PASS = "pass"
    FAIL = "fail"
    SKIP = "skip"
    ERROR = "error"


class CheckResult(BaseModel):
    check_id: str
    status: CheckStatus
    severity: str
    message: str | None = None


class RepoResult(BaseModel):
    slug: str
    timestamp: datetime
    score: float
    results: list[CheckResult]


class RunResult(BaseModel):
    run_id: str
    timestamp: datetime
    total_repos: int
    passed: int
    failed: int
    repos: list[RepoResult]
