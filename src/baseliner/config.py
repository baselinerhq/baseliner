from __future__ import annotations

import importlib.resources
from pathlib import Path
from typing import Literal

import yaml
from pydantic import BaseModel, Field, ValidationError

from baseliner.models.policy import Policy


class ConfigError(Exception):
    pass


class AuthError(Exception):
    pass


class PolicyConfig(BaseModel):
    base: str = "default"
    ignore: list[str] = Field(default_factory=list)
    repo_ignores: dict[str, list[str]] = Field(default_factory=dict)


class GitHubScopeConfig(BaseModel):
    type: Literal["org", "user"]
    name: str
    token_env: str = "GITHUB_TOKEN"


class LocalScopeConfig(BaseModel):
    paths: list[str] = Field(default_factory=list)


class ScopeConfig(BaseModel):
    github: GitHubScopeConfig | None = None
    local: LocalScopeConfig | None = None
    include: list[str] = Field(default_factory=list)
    exclude: list[str] = Field(default_factory=list)


class BaselinerConfig(BaseModel):
    scope: ScopeConfig
    policy: PolicyConfig = Field(default_factory=PolicyConfig)


def load_config(path: Path) -> BaselinerConfig:
    if not path.exists():
        raise ConfigError(f"Config file not found: {path}")
    try:
        data = yaml.safe_load(path.read_text(encoding="utf-8"))
    except yaml.YAMLError as exc:
        raise ConfigError(f"Invalid YAML in config file: {exc}") from exc

    if data is None:
        data = {}

    try:
        return BaselinerConfig.model_validate(data)
    except ValidationError as exc:
        raise ConfigError(f"Config validation failed: {exc}") from exc


class PolicyLoader:
    def load(self, base: str) -> Policy:
        if base == "default":
            return self._load_builtin()

        path = Path(base)
        if not path.exists():
            raise ConfigError(f"Policy file not found: {path}")
        return self._load_yaml(path.read_text(encoding="utf-8"))

    def _load_builtin(self) -> Policy:
        default_policy = importlib.resources.files("baseliner.policies").joinpath("default.yaml")
        with default_policy.open("r", encoding="utf-8") as policy_file:
            return self._load_yaml(policy_file.read())

    def _load_yaml(self, text: str) -> Policy:
        data = yaml.safe_load(text)
        return Policy.model_validate(data)
