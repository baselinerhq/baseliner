from __future__ import annotations

import logging
import os
from pathlib import Path

from baseliner.collectors.base import Collector
from baseliner.models.repository import FilesystemContext, NormalizedRepository
from baseliner.models.scope import RepoSource

LOGGER = logging.getLogger(__name__)

README_FILENAMES = {"readme.md", "readme.rst", "readme.txt", "readme"}
LICENSE_FILENAMES = {"license", "license.md", "license.txt", "copying"}
GITIGNORE_FILENAMES = {".gitignore"}
DEPENDENCY_UPDATE_PATHS = {
    ".github/dependabot.yml",
    ".github/dependabot.yaml",
    "renovate.json",
    ".renovaterc",
    ".renovaterc.json",
}


def detect_key_files(files: list[str]) -> dict[str, bool]:
    key_files = {
        "README": False,
        "LICENSE": False,
        "GITIGNORE": False,
        "CODEOWNERS": False,
    }
    for rel_path in files:
        path_obj = Path(rel_path)
        filename_lower = path_obj.name.lower()
        parent_lower = path_obj.parent.as_posix().lower()
        if filename_lower in README_FILENAMES:
            key_files["README"] = True
        if filename_lower in LICENSE_FILENAMES:
            key_files["LICENSE"] = True
        if filename_lower in GITIGNORE_FILENAMES:
            key_files["GITIGNORE"] = True
        if filename_lower == "codeowners" and parent_lower in {".", "", ".github"}:
            key_files["CODEOWNERS"] = True
    return key_files


def detect_ci_files(files: list[str]) -> list[str]:
    matches: list[str] = []
    for rel_path in files:
        rel_lower = rel_path.lower()
        name_lower = Path(rel_path).name.lower()
        if rel_lower.startswith(".github/workflows/") and (
            rel_lower.endswith(".yml") or rel_lower.endswith(".yaml")
        ):
            matches.append(rel_path)
            continue
        if rel_lower == ".circleci/config.yml":
            matches.append(rel_path)
            continue
        if name_lower == "jenkinsfile":
            matches.append(rel_path)
            continue
        if rel_lower == ".gitlab-ci.yml":
            matches.append(rel_path)
    return sorted(set(matches))


def detect_dependency_update_files(files: list[str]) -> list[str]:
    matches: list[str] = []
    for rel_path in files:
        if rel_path.lower() in DEPENDENCY_UPDATE_PATHS:
            matches.append(rel_path)
    return sorted(set(matches))


def find_readme_path(files: list[str]) -> str | None:
    for rel_path in files:
        if Path(rel_path).name.lower() in README_FILENAMES:
            return rel_path
    return None


class FilesystemCollector(Collector):
    def collect(self, source: RepoSource) -> NormalizedRepository:
        path = source.path
        if path is None:
            LOGGER.warning(
                "FilesystemCollector called without source.path for slug=%s", source.slug
            )
            return self._empty_result(source)

        root_path = path.resolve()
        if not root_path.exists() or not root_path.is_dir():
            LOGGER.warning("Path does not exist or is not a directory: %s", root_path)
            return self._empty_result(source)

        files = self._collect_files(root_path)
        key_files = detect_key_files(files)
        ci_files = detect_ci_files(files)
        dep_update_files = detect_dependency_update_files(files)
        readme_content = self._read_readme(root_path, files)

        return NormalizedRepository(
            source_type=source.type,
            slug=source.slug,
            name=root_path.name,
            fs=FilesystemContext(
                files=files,
                key_files=key_files,
                readme_content=readme_content,
                ci_files=ci_files,
                dep_update_files=dep_update_files,
            ),
        )

    def _collect_files(self, root_path: Path) -> list[str]:
        files: list[str] = []

        def on_walk_error(err: OSError) -> None:
            LOGGER.warning("Error walking %s: %s", root_path, err)

        for current_dir, dirnames, filenames in os.walk(
            root_path, topdown=True, onerror=on_walk_error
        ):
            dirnames[:] = [d for d in dirnames if d != ".git"]

            try:
                rel_dir = Path(current_dir).relative_to(root_path)
            except ValueError:
                continue

            depth = 0 if rel_dir == Path(".") else len(rel_dir.parts)
            if depth >= 4:
                dirnames[:] = []

            for filename in filenames:
                file_path = Path(current_dir) / filename
                try:
                    rel_path = file_path.relative_to(root_path)
                except ValueError:
                    continue
                if len(rel_path.parts) > 4:
                    continue
                if rel_path.parts and rel_path.parts[0] == ".git":
                    continue
                files.append(rel_path.as_posix())

        return sorted(set(files))

    def _read_readme(self, root_path: Path, files: list[str]) -> str | None:
        readme_rel_path = find_readme_path(files)
        if readme_rel_path is None:
            return None

        readme_path = root_path / readme_rel_path
        try:
            readme_bytes = readme_path.read_bytes()[:4096]
        except OSError as exc:
            LOGGER.warning("Could not read README at %s: %s", readme_path, exc)
            return None
        return readme_bytes.decode("utf-8", errors="replace")

    def _empty_result(self, source: RepoSource) -> NormalizedRepository:
        return NormalizedRepository(
            source_type=source.type,
            slug=source.slug,
            name=self._resolve_name(source),
            fs=FilesystemContext(
                files=[],
                key_files={
                    "README": False,
                    "LICENSE": False,
                    "GITIGNORE": False,
                    "CODEOWNERS": False,
                },
                readme_content=None,
                ci_files=[],
                dep_update_files=[],
            ),
        )

    @staticmethod
    def _resolve_name(source: RepoSource) -> str:
        if source.path is not None:
            return source.path.name
        return source.slug.split("/")[-1]
