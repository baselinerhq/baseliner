from __future__ import annotations

from baseliner.checks.base import Check


class CheckRegistry:
    def __init__(self) -> None:
        self._registry: dict[str, Check] = {}

    def register(self, check: Check) -> None:
        self._registry[check.check_id] = check

    def get(self, check_id: str) -> Check:
        if check_id not in self._registry:
            raise KeyError(f"Unknown check id: '{check_id}'")
        return self._registry[check_id]

    def all(self) -> dict[str, Check]:
        return dict(self._registry)


def build_default_registry() -> CheckRegistry:
    from baseliner.checks.git import DefaultBranchIsMain, StaleRepo
    from baseliner.checks.hygiene import (
        CiPresent,
        CodeownersExists,
        DependencyUpdateConfig,
        GitignoreExists,
        LicenseExists,
        ReadmeExists,
        ReadmeHasHeading,
        ReadmeNonEmpty,
    )

    registry = CheckRegistry()
    for check in [
        ReadmeExists(),
        ReadmeNonEmpty(),
        ReadmeHasHeading(),
        LicenseExists(),
        GitignoreExists(),
        CiPresent(),
        CodeownersExists(),
        DependencyUpdateConfig(),
        DefaultBranchIsMain(),
        StaleRepo(),
    ]:
        registry.register(check)
    return registry
