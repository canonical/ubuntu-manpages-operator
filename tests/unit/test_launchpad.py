# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Unit tests for the launchpad module.

These tests only cover those methods that do not require internet access,
and do not attempt to manipulate the underlying machine.
"""

import os

import pytest
from httplib2 import ProxyInfo

from launchpad import MockLaunchpadClient, _proxy_config


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


@pytest.mark.parametrize(
    "env_var",
    [
        {
            "method": "http",
            "var": "JUJU_CHARM_HTTP_PROXY",
            "url": "http://proxy.example.com",
            "expected": ProxyInfo(3, "proxy.example.com", 80),
        },
        {
            "method": "https",
            "var": "JUJU_CHARM_HTTPS_PROXY",
            "url": "https://proxy.example.com",
            "expected": ProxyInfo(3, "proxy.example.com", 443),
        },
        {
            "method": "https",
            "var": "JUJU_CHARM_HTTPS_PROXY",
            "url": "https://proxy.example.com:8080",
            "expected": ProxyInfo(3, "proxy.example.com", 8080),
        },
        {
            "method": "http",
            "var": "JUJU_CHARM_HTTP_PROXY",
            "url": "",
            "expected": None,
        },
    ],
)
def test_proxy_config(env_var):
    os.environ[env_var["var"]] = env_var["url"]

    proxy_info = _proxy_config(env_var["method"])

    if proxy_info is not None:
        assert proxy_info.proxy_type == env_var["expected"].proxy_type
        assert proxy_info.proxy_host == env_var["expected"].proxy_host
        assert proxy_info.proxy_port == env_var["expected"].proxy_port

    del os.environ[env_var["var"]]
