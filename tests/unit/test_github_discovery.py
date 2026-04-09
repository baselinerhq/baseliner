from __future__ import annotations

from datetime import UTC, datetime, timedelta
from types import SimpleNamespace
from unittest.mock import MagicMock

import pytest

from baseliner.config import AuthError, GitHubScopeConfig
from baseliner.discovery.github import GitHubDiscovery


def _make_repo(name: str) -> SimpleNamespace:
    return SimpleNamespace(name=name)


def _mock_client_with_org_repos(repo_names: list[str], remaining: int = 500) -> MagicMock:
    client = MagicMock()
    repos = [_make_repo(name) for name in repo_names]
    org = MagicMock()
    org.get_repos.return_value = repos
    client.get_organization.return_value = org
    rate = SimpleNamespace(remaining=remaining, reset=datetime.now(tz=UTC) + timedelta(minutes=30))
    client.get_rate_limit.return_value = SimpleNamespace(core=rate)
    return client


def _mock_client_with_user_repos(repo_names: list[str], remaining: int = 500) -> MagicMock:
    client = MagicMock()
    repos = [_make_repo(name) for name in repo_names]
    user = MagicMock()
    user.get_repos.return_value = repos
    client.get_user.return_value = user
    rate = SimpleNamespace(remaining=remaining, reset=datetime.now(tz=UTC) + timedelta(minutes=30))
    client.get_rate_limit.return_value = SimpleNamespace(core=rate)
    return client


def test_github_discovery_no_filters_returns_all_repos(monkeypatch: pytest.MonkeyPatch) -> None:
    client = _mock_client_with_org_repos(["service-a", "lib-b", "tooling-c"])
    monkeypatch.setenv("GITHUB_TOKEN", "token")
    monkeypatch.setattr("baseliner.discovery.github.github.Github", lambda token: client)

    config = GitHubScopeConfig(type="org", name="acme", token_env="GITHUB_TOKEN")
    sources = GitHubDiscovery(config).discover()

    assert [source.slug for source in sources] == ["acme/service-a", "acme/lib-b", "acme/tooling-c"]
    client.get_organization.assert_called_once_with("acme")


def test_github_discovery_exclude_filter(monkeypatch: pytest.MonkeyPatch) -> None:
    client = _mock_client_with_org_repos(["service-a", "archived-old", "lib-b"])
    monkeypatch.setenv("GITHUB_TOKEN", "token")
    monkeypatch.setattr("baseliner.discovery.github.github.Github", lambda token: client)

    config = GitHubScopeConfig(type="org", name="acme", token_env="GITHUB_TOKEN")
    sources = GitHubDiscovery(config, exclude=["archived-*"]).discover()

    assert [source.slug for source in sources] == ["acme/service-a", "acme/lib-b"]


def test_github_discovery_include_filter(monkeypatch: pytest.MonkeyPatch) -> None:
    client = _mock_client_with_org_repos(["service-a", "lib-b", "service-b"])
    monkeypatch.setenv("GITHUB_TOKEN", "token")
    monkeypatch.setattr("baseliner.discovery.github.github.Github", lambda token: client)

    config = GitHubScopeConfig(type="org", name="acme", token_env="GITHUB_TOKEN")
    sources = GitHubDiscovery(config, include=["service-*"]).discover()

    assert [source.slug for source in sources] == ["acme/service-a", "acme/service-b"]


def test_github_discovery_include_and_exclude(monkeypatch: pytest.MonkeyPatch) -> None:
    client = _mock_client_with_org_repos(["svc-a", "svc-legacy", "lib-x"])
    monkeypatch.setenv("GITHUB_TOKEN", "token")
    monkeypatch.setattr("baseliner.discovery.github.github.Github", lambda token: client)

    config = GitHubScopeConfig(type="org", name="acme", token_env="GITHUB_TOKEN")
    sources = GitHubDiscovery(config, include=["svc-*"], exclude=["svc-legacy"]).discover()

    assert [source.slug for source in sources] == ["acme/svc-a"]


def test_github_discovery_missing_token_raises_auth_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    config = GitHubScopeConfig(type="org", name="acme", token_env="GITHUB_TOKEN")

    with pytest.raises(AuthError, match="GITHUB_TOKEN"):
        GitHubDiscovery(config).discover()


def test_github_discovery_empty_token_raises_auth_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GITHUB_TOKEN", "   ")
    config = GitHubScopeConfig(type="org", name="acme", token_env="GITHUB_TOKEN")

    with pytest.raises(AuthError, match="GITHUB_TOKEN"):
        GitHubDiscovery(config).discover()


def test_github_discovery_logs_rate_limit_warning(monkeypatch: pytest.MonkeyPatch, caplog) -> None:
    client = _mock_client_with_org_repos(["service-a"], remaining=50)
    monkeypatch.setenv("GITHUB_TOKEN", "token")
    monkeypatch.setattr("baseliner.discovery.github.github.Github", lambda token: client)
    config = GitHubScopeConfig(type="org", name="acme", token_env="GITHUB_TOKEN")

    with caplog.at_level("WARNING"):
        sources = GitHubDiscovery(config).discover()

    assert [source.slug for source in sources] == ["acme/service-a"]
    assert any("GitHub API rate limit low" in message for message in caplog.messages)


def test_github_discovery_user_type_uses_get_user(monkeypatch: pytest.MonkeyPatch) -> None:
    client = _mock_client_with_user_repos(["service-a"])
    monkeypatch.setenv("GITHUB_TOKEN", "token")
    monkeypatch.setattr("baseliner.discovery.github.github.Github", lambda token: client)
    config = GitHubScopeConfig(type="user", name="alice", token_env="GITHUB_TOKEN")

    sources = GitHubDiscovery(config).discover()

    assert [source.slug for source in sources] == ["alice/service-a"]
    client.get_user.assert_called_once_with("alice")
    client.get_organization.assert_not_called()
