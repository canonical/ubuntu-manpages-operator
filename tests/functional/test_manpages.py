# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Functional tests for the manpages module.

These tests will manipulate the underlying machine, and thus are
run in a fresh VM with spread. They also expect to have access to
the internet.
"""

import json
from dataclasses import asdict
from pathlib import Path

import charms.operator_libs_linux.v0.apt as apt
import pytest
from charms.operator_libs_linux.v1.systemd import service_running

from launchpad import LaunchpadClient
from manpages import (
    APP_DIR,
    BIN_DIR,
    CONFIG_PATH,
    NGINX_SITE_CONFIG_PATH,
    PACKAGES,
    UPDATE_SERVICE_PATH,
    WWW_DIR,
    Manpages,
    ManpagesConfig,
)


@pytest.fixture
def manpages():
    lp = LaunchpadClient()
    return Manpages(lp)


def test_install_manpages(manpages):
    manpages.install()

    # Ensure that packages are installed.
    for p in PACKAGES:
        package = apt.DebianPackage.from_system(p)
        assert package.state == apt.PackageState.Present

    # Ensure the www and bin directories have been created.
    assert WWW_DIR.exists()
    assert BIN_DIR.exists()

    # Ensure the nginx config and systemd service have been created.
    assert NGINX_SITE_CONFIG_PATH.exists()
    assert UPDATE_SERVICE_PATH.exists()

    assert not Path("/etc/nginx/sites-enables/default").exists(follow_symlinks=False)  # ty: ignore[unknown-argument]

    # Ensure ownership of directories and files was set correctly.
    assert WWW_DIR.owner() == "www-data"
    assert BIN_DIR.owner() == "www-data"
    assert APP_DIR.owner() == "www-data"


def test_configure_manpages(manpages):
    releases = "questing, plucky, oracular, noble, jammy"
    manpages.configure(releases, "http://foo.bar")

    assert CONFIG_PATH.exists()

    cfg = ManpagesConfig()
    cfg.releases = {
        "jammy": "22.04",
        "noble": "24.04",
        "oracular": "24.10",
        "plucky": "25.04",
        "questing": "25.10",
    }
    cfg.site = "http://foo.bar"

    with open(CONFIG_PATH, "r") as f:
        content = json.load(f)
        assert content == asdict(cfg)


def test_configure_manpages_bad_codename(manpages):
    releases = "foobar, plucky, oracular, noble, jammy"
    try:
        manpages.configure(releases, "http://foo.bar")
    except Exception as e:
        assert isinstance(e, ValueError)


def test_restart_manpages(manpages):
    manpages.restart()
    assert service_running("nginx")
    assert service_running("fcgiwrap")
    assert not service_running("update-manpages")


def test_update_manpages(manpages):
    manpages.update_manpages()
    assert manpages.updating


def test_purge_unused_manpages(manpages):
    # Place the directories for the full set of configured releases.
    releases = ["questing", "plucky", "oracular", "noble", "jammy"]
    for r in releases:
        (WWW_DIR / "manpages" / r).mkdir(parents=True, exist_ok=True)

    # Reconfigure to only keep one release.
    releases = "questing"
    manpages.configure(releases, "http://foo.bar")
    manpages.purge_unused_manpages()

    # Ensure the purged releases have been removed from disk.
    purged_releases = ["plucky", "oracular", "noble", "jammy"]
    for p in purged_releases:
        assert not (WWW_DIR / "manpages" / p).exists()

    # Ensure the remaining configured release exists.
    assert (WWW_DIR / "manpages" / "questing").exists()
