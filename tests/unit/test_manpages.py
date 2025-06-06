# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Unit tests for the manpages module.

These tests only cover those methods that do not require internet access,
and do not attempt to manipulate the underlying machine.
"""

import pytest

from launchpad import MockLaunchpadClient
from manpages import (
    Manpages,
    ManpagesConfig,
)


@pytest.fixture
def manpages():
    lp = MockLaunchpadClient()
    return Manpages(lp)


def test_build_config_successful(manpages):
    cfg = ManpagesConfig()
    cfg.releases = {
        "jammy": "22.04",
        "noble": "24.04",
        "oracular": "24.10",
        "plucky": "25.04",
        "questing": "25.10",
    }

    config = manpages._build_config(
        "questing, plucky, oracular, noble, jammy", "http://manpages.ubuntu.com"
    )
    assert config == cfg


def test_build_config_unknown_release(manpages):
    try:
        manpages._build_config(
            "foobar, plucky, oracular, noble, jammy", "http://manpages.ubuntu.com"
        )
    except Exception as e:
        assert isinstance(e, ValueError)
        assert str(e) == "failed to build manpages config: release 'foobar' not found on Launchpad"


def test_build_config_invalid_release(manpages):
    try:
        manpages._build_config("!!!!!!!!", "http://manpages.ubuntu.com")
    except Exception as e:
        assert isinstance(e, ValueError)
        assert str(e) == "failed to build manpages config: invalid releases specified"
