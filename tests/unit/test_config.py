from __future__ import annotations

from pathlib import Path

import pytest

from baseliner.config import ConfigError, load_config


def test_load_config_valid_file(tmp_path: Path) -> None:
    config_file = tmp_path / "baseliner.yaml"
    config_file.write_text(
        "\n".join(
            [
                "scope:",
                "  github:",
                "    type: org",
                "    name: my-org",
                "    token_env: MY_TOKEN",
                "  include:",
                "    - service-*",
                "  exclude:",
                "    - archived-*",
                "policy:",
                "  base: default",
                "  ignore:",
                "    - stale_repo",
            ]
        ),
        encoding="utf-8",
    )

    config = load_config(config_file)
    assert config.scope.github is not None
    assert config.scope.github.name == "my-org"
    assert config.scope.include == ["service-*"]
    assert config.policy.ignore == ["stale_repo"]


def test_load_config_missing_file_raises_config_error(tmp_path: Path) -> None:
    missing_file = tmp_path / "does-not-exist.yaml"
    with pytest.raises(ConfigError, match=str(missing_file)):
        load_config(missing_file)


def test_load_config_invalid_yaml_raises_config_error(tmp_path: Path) -> None:
    config_file = tmp_path / "baseliner.yaml"
    config_file.write_text("scope: [invalid", encoding="utf-8")

    with pytest.raises(ConfigError, match="Invalid YAML"):
        load_config(config_file)


def test_load_config_wrong_field_type_raises_config_error(tmp_path: Path) -> None:
    config_file = tmp_path / "baseliner.yaml"
    config_file.write_text(
        "\n".join(
            [
                "scope:",
                "  github:",
                "    type: 123",
                "    name: my-org",
            ]
        ),
        encoding="utf-8",
    )

    with pytest.raises(ConfigError, match="Config validation failed"):
        load_config(config_file)


def test_load_config_minimal_valid_config_applies_defaults(tmp_path: Path) -> None:
    config_file = tmp_path / "baseliner.yaml"
    config_file.write_text("scope: {}", encoding="utf-8")

    config = load_config(config_file)
    assert config.scope.github is None
    assert config.scope.local is None
    assert config.scope.include == []
    assert config.scope.exclude == []
    assert config.policy.base == "default"
    assert config.policy.ignore == []
    assert config.policy.repo_ignores == {}


def test_load_config_parses_github_and_local_scopes(tmp_path: Path) -> None:
    config_file = tmp_path / "baseliner.yaml"
    config_file.write_text(
        "\n".join(
            [
                "scope:",
                "  github:",
                "    type: user",
                "    name: alice",
                "  local:",
                "    paths:",
                "      - ./repo-a",
                "      - ./repo-b",
            ]
        ),
        encoding="utf-8",
    )

    config = load_config(config_file)
    assert config.scope.github is not None
    assert config.scope.github.type == "user"
    assert config.scope.local is not None
    assert config.scope.local.paths == ["./repo-a", "./repo-b"]
