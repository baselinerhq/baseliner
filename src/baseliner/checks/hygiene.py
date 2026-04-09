from __future__ import annotations

from baseliner.checks.base import Check
from baseliner.models.repository import NormalizedRepository
from baseliner.models.result import CheckResult


class ReadmeExists(Check):
    check_id = "readme_exists"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        if repo.fs.key_files.get("README", False):
            return self._pass()
        return self._fail("No README file found")


class ReadmeNonEmpty(Check):
    check_id = "readme_nonempty"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        if repo.fs.readme_content is None:
            return self._fail("README not found")
        if len(repo.fs.readme_content.strip()) > 0:
            return self._pass()
        return self._fail("README is present but empty")


class ReadmeHasHeading(Check):
    check_id = "readme_has_heading"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        content = repo.fs.readme_content
        if content is None:
            return self._fail("README not found")

        lines = content.splitlines()
        if self._has_markdown_heading(lines) or self._has_underline_heading(lines):
            return self._pass()

        return self._fail(
            "README has no headings (expected at least one # heading or underline heading)"
        )

    @staticmethod
    def _has_markdown_heading(lines: list[str]) -> bool:
        return any(line.lstrip().startswith("#") for line in lines)

    @staticmethod
    def _has_underline_heading(lines: list[str]) -> bool:
        for index in range(len(lines) - 1):
            title = lines[index].strip()
            underline = lines[index + 1].strip()
            if not title or not underline:
                continue
            if len(underline) < 3:
                continue
            if all(char == "=" for char in underline) or all(char == "-" for char in underline):
                return True
        return False


class LicenseExists(Check):
    check_id = "license_exists"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        if repo.fs.key_files.get("LICENSE", False):
            return self._pass()
        return self._fail("No LICENSE or COPYING file found")


class GitignoreExists(Check):
    check_id = "gitignore_exists"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        if repo.fs.key_files.get("GITIGNORE", False):
            return self._pass()
        return self._fail("No .gitignore found")


class CiPresent(Check):
    check_id = "ci_present"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        if len(repo.fs.ci_files) > 0:
            return self._pass()
        return self._fail("No CI workflow files found")


class CodeownersExists(Check):
    check_id = "codeowners_exists"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        if repo.fs.key_files.get("CODEOWNERS", False):
            return self._pass()
        return self._fail("No CODEOWNERS file found")


class DependencyUpdateConfig(Check):
    check_id = "dependency_update_config"
    required_layer = "fs"

    def _evaluate(self, repo: NormalizedRepository) -> CheckResult:
        assert repo.fs is not None
        if len(repo.fs.dep_update_files) > 0:
            return self._pass()
        return self._fail("No Dependabot or Renovate config found")
