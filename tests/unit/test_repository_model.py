from __future__ import annotations

from baseliner.models.repository import FilesystemContext, NormalizedRepository


def test_normalized_repository_partial_layers_serialize() -> None:
    repo = NormalizedRepository(
        source_type="local",
        slug="/tmp/demo",
        name="demo",
        fs=FilesystemContext(
            files=["README.md"],
            key_files={
                "README": True,
                "LICENSE": False,
                "GITIGNORE": False,
                "CODEOWNERS": False,
            },
            readme_content="# Demo",
            ci_files=[],
            dep_update_files=[],
        ),
        git=None,
        platform=None,
    )

    dumped = repo.model_dump()
    assert dumped["source_type"] == "local"
    assert dumped["git"] is None
    assert dumped["platform"] is None
