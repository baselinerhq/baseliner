from __future__ import annotations

from enum import StrEnum

from pydantic import BaseModel


class Severity(StrEnum):
    CRITICAL = "critical"
    HIGH = "high"
    MEDIUM = "medium"
    LOW = "low"


SEVERITY_WEIGHT: dict[Severity, int] = {
    Severity.CRITICAL: 4,
    Severity.HIGH: 3,
    Severity.MEDIUM: 2,
    Severity.LOW: 1,
}


class CheckDefinition(BaseModel):
    id: str
    severity: Severity
    enabled: bool = True


class Policy(BaseModel):
    id: str
    checks: list[CheckDefinition]
