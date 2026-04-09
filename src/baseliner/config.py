from __future__ import annotations

import importlib.resources
from pathlib import Path

import yaml
from pydantic import BaseModel, Field

from baseliner.models.policy import Policy


class ConfigError(Exception):
    pass


class PolicyConfig(BaseModel):
    base: str = "default"
    ignore: list[str] = Field(default_factory=list)
    repo_ignores: dict[str, list[str]] = Field(default_factory=dict)


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
