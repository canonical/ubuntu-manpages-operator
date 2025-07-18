#!/usr/bin/env python3
# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""A simple Launchpad client implementation."""

import os
from abc import ABC
from typing import Dict, List, Optional

import httplib2
import launchpadlib as lplib
from launchpadlib.launchpad import Launchpad


class LaunchpadClientBase(ABC):
    """Basic Launchpad client interface."""

    def release_map(self, releases: List[str]) -> Dict[str, str]:
        """Return a dictionary that maps release codenames to their corresponding versions.

        Uses the Launchpad API to retrieve the version information for each codename. The
        returned dictionary is sorted in descending order by version.
        """
        return {}


class LaunchpadClient(LaunchpadClientBase):
    """Launchpad client implementation."""

    def release_map(self, releases: List[str]) -> Dict[str, str]:
        """Return a dictionary that maps release codenames to their corresponding versions.

        Uses the Launchpad API to retrieve the version information for each codename. The
        returned dictionary is sorted in descending order by version.
        """
        release_map = {}

        lp = Launchpad.login_anonymously(
            "manpages",
            lplib.uris.LPNET_SERVICE_ROOT,  # ty: ignore[unresolved-attribute]
            proxy_info=_proxy_config,
        )

        for release in releases:
            try:
                vfilter = filter(lambda x: x.name == release, lp.projects["ubuntu"].series)
                version = next(vfilter).version
            except StopIteration:
                raise ValueError(f"release '{release}' not found on Launchpad")

            release_map[release] = version

        # Return the release map, sorted in descending order by version.
        return dict(sorted(release_map.items(), key=lambda item: item[1]))


class MockLaunchpadClient(LaunchpadClientBase):
    """Mock Launchpad client implementation."""

    def release_map(self, releases: List[str]) -> Dict[str, str]:
        """Return a dictionary that maps release codenames to their corresponding versions.

        Uses the Launchpad API to retrieve the version information for each codename. The
        returned dictionary is sorted in descending order by version.
        """
        known_releases = {
            "jammy": "22.04",
            "noble": "24.04",
            "oracular": "24.10",
            "plucky": "25.04",
            "questing": "25.10",
        }

        release_map = {}
        for release in releases:
            version = known_releases.get(release)
            if version is None:
                raise ValueError(f"release '{release}' not found on Launchpad")

            release_map[release] = version

        # Return the release map, sorted in descending order by version.
        return dict(sorted(release_map.items(), key=lambda item: item[1]))


def _proxy_config(method="https") -> Optional[httplib2.ProxyInfo]:
    """Get charm proxy information from juju charm environment."""
    if method not in ("http", "https"):
        return

    env_var = f"JUJU_CHARM_{method.upper()}_PROXY"
    url = os.environ.get(env_var)

    if not url:
        return

    noproxy = os.environ.get("JUJU_CHARM_NO_PROXY", None)

    return httplib2.proxy_info_from_url(url, method, noproxy)
