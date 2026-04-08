import baseliner


def test_version_is_set() -> None:
    assert isinstance(baseliner.__version__, str)
    assert len(baseliner.__version__) > 0
