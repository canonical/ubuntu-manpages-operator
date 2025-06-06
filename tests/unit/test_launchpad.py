# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Unit tests for the launchpad module.

These tests only cover those methods that do not require internet access,
and do not attempt to manipulate the underlying machine.
"""

import pytest

from launchpad import MockLaunchpadClient


@pytest.fixture
def lp():
    return MockLaunchpadClient()


def test_release_map_success(lp):
    releases = ["questing", "plucky", "oracular", "noble", "jammy"]
    expected = {
        "jammy": "22.04",
        "noble": "24.04",
        "oracular": "24.10",
        "plucky": "25.04",
        "questing": "25.10",
    }
    result = lp.release_map(releases)
    assert result == expected


def test_release_map_invalid_release(lp):
    releases = ["foobar", "plucky", "oracular", "noble", "jammy"]
    try:
        lp.release_map(releases)
    except Exception as e:
        assert str(e) == "release 'foobar' not found on Launchpad"
