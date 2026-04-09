from __future__ import annotations

import pytest

from baseliner.checks.base import Check
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
from baseliner.models.repository import FilesystemContext, NormalizedRepository
from baseliner.models.result import CheckStatus


def make_fs(
    *,
    readme: bool = True,
    license_file: bool = True,
    gitignore: bool = True,
    codeowners: bool = True,
    readme_content: str | None = "# Title\n",
    ci_files: list[str] | None = None,
    dep_update_files: list[str] | None = None,
) -> FilesystemContext:
    return FilesystemContext(
        files=[],
        key_files={
            "README": readme,
            "LICENSE": license_file,
            "GITIGNORE": gitignore,
            "CODEOWNERS": codeowners,
        },
        readme_content=readme_content,
        ci_files=ci_files if ci_files is not None else [".github/workflows/ci.yml"],
        dep_update_files=(
            dep_update_files if dep_update_files is not None else [".github/dependabot.yml"]
        ),
    )


def make_repo(fs: FilesystemContext | None) -> NormalizedRepository:
    return NormalizedRepository(source_type="local", slug="/tmp/repo", name="repo", fs=fs)


@pytest.mark.parametrize(
    ("check", "pass_fs", "fail_fs", "fail_message"),
    [
        (
            ReadmeExists(),
            make_fs(),
            make_fs(readme=False, readme_content=None),
            "No README file found",
        ),
        (
            ReadmeNonEmpty(),
            make_fs(readme_content="# Title"),
            make_fs(readme_content="  \n "),
            "README is present but empty",
        ),
        (
            ReadmeHasHeading(),
            make_fs(readme_content="# Title"),
            make_fs(readme_content="This readme has text only."),
            "README has no headings",
        ),
        (
            LicenseExists(),
            make_fs(),
            make_fs(license_file=False),
            "No LICENSE or COPYING file found",
        ),
        (
            GitignoreExists(),
            make_fs(),
            make_fs(gitignore=False),
            "No .gitignore found",
        ),
        (
            CiPresent(),
            make_fs(),
            make_fs(ci_files=[]),
            "No CI workflow files found",
        ),
        (
            CodeownersExists(),
            make_fs(),
            make_fs(codeowners=False),
            "No CODEOWNERS file found",
        ),
        (
            DependencyUpdateConfig(),
            make_fs(),
            make_fs(dep_update_files=[]),
            "No Dependabot or Renovate config found",
        ),
    ],
)
def test_hygiene_checks_pass_fail_skip(
    check: Check, pass_fs: FilesystemContext, fail_fs: FilesystemContext, fail_message: str
) -> None:
    pass_result = check.evaluate(make_repo(pass_fs))
    assert pass_result.status == CheckStatus.PASS

    fail_result = check.evaluate(make_repo(fail_fs))
    assert fail_result.status == CheckStatus.FAIL
    assert fail_result.message is not None
    assert fail_message in fail_result.message

    skip_result = check.evaluate(make_repo(None))
    assert skip_result.status == CheckStatus.SKIP


def test_readme_nonempty_fails_when_missing_readme() -> None:
    result = ReadmeNonEmpty().evaluate(make_repo(make_fs(readme=False, readme_content=None)))
    assert result.status == CheckStatus.FAIL
    assert result.message == "README not found"


def test_readme_has_heading_fails_when_missing_readme() -> None:
    result = ReadmeHasHeading().evaluate(make_repo(make_fs(readme=False, readme_content=None)))
    assert result.status == CheckStatus.FAIL
    assert result.message == "README not found"


def test_readme_has_heading_accepts_underline_style_heading() -> None:
    fs = make_fs(readme_content="Project\n=======\n\nBody")
    result = ReadmeHasHeading().evaluate(make_repo(fs))
    assert result.status == CheckStatus.PASS


def test_hygiene_checks_have_expected_ids_and_required_layer() -> None:
    checks = [
        (ReadmeExists(), "readme_exists"),
        (ReadmeNonEmpty(), "readme_nonempty"),
        (ReadmeHasHeading(), "readme_has_heading"),
        (LicenseExists(), "license_exists"),
        (GitignoreExists(), "gitignore_exists"),
        (CiPresent(), "ci_present"),
        (CodeownersExists(), "codeowners_exists"),
        (DependencyUpdateConfig(), "dependency_update_config"),
    ]
    for check, expected_id in checks:
        assert check.check_id == expected_id
        assert check.required_layer == "fs"
