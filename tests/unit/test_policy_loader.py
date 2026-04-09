from __future__ import annotations

from pathlib import Path

import pytest

from baseliner.config import ConfigError, PolicyLoader


def test_policy_loader_loads_default_policy() -> None:
    policy = PolicyLoader().load("default")
    assert policy.id == "default-v1"
    assert len(policy.checks) == 10


def test_policy_loader_raises_for_missing_file(tmp_path: Path) -> None:
    missing_path = tmp_path / "missing-policy.yaml"
    with pytest.raises(ConfigError, match="Policy file not found"):
        PolicyLoader().load(str(missing_path))


def test_policy_loader_loads_custom_yaml(tmp_path: Path) -> None:
    custom_policy = tmp_path / "policy.yaml"
    custom_policy.write_text(
        "\n".join(
            [
                "id: custom-v1",
                "checks:",
                "  - { id: readme_exists, severity: critical, enabled: true }",
                "  - { id: stale_repo, severity: low, enabled: false }",
            ]
        ),
        encoding="utf-8",
    )

    policy = PolicyLoader().load(str(custom_policy))
    assert policy.id == "custom-v1"
    assert len(policy.checks) == 2
    assert policy.checks[0].id == "readme_exists"
