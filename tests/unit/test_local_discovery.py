from __future__ import annotations

from pathlib import Path

from baseliner.discovery.local import LocalDiscovery


def test_local_discovery_returns_resolved_absolute_slugs(tmp_path: Path) -> None:
    repo_a = tmp_path / "repo-a"
    repo_b = tmp_path / "repo-b"
    repo_a.mkdir()
    repo_b.mkdir()

    discovery = LocalDiscovery([str(repo_a), str(repo_b)])
    sources = discovery.discover()

    assert len(sources) == 2
    assert {source.slug for source in sources} == {str(repo_a.resolve()), str(repo_b.resolve())}
    assert all(source.type == "local" for source in sources)


def test_local_discovery_skips_missing_path_with_warning(tmp_path: Path, caplog) -> None:
    missing = tmp_path / "missing"
    discovery = LocalDiscovery([str(missing)])

    with caplog.at_level("WARNING"):
        sources = discovery.discover()

    assert sources == []
    assert any("Local path does not exist, skipping" in message for message in caplog.messages)


def test_local_discovery_skips_file_path_with_warning(tmp_path: Path, caplog) -> None:
    file_path = tmp_path / "not-a-dir.txt"
    file_path.write_text("x", encoding="utf-8")
    discovery = LocalDiscovery([str(file_path)])

    with caplog.at_level("WARNING"):
        sources = discovery.discover()

    assert sources == []
    assert any("Local path is not a directory, skipping" in message for message in caplog.messages)


def test_local_discovery_empty_paths_returns_empty_list() -> None:
    assert LocalDiscovery([]).discover() == []
