# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Representation of the manpages service."""

import logging
import os
import re
from pathlib import Path

import ops
from ops.pebble import APIError, ConnectionError, PathError, ProtocolError

logger = logging.getLogger(__name__)

WWW_DIR = Path("/app") / "www"
PORT = 8080

# Used to fetch release codenames from the config string passed to the charm.
RELEASES_PATTERN = re.compile(r"([a-z]+)(?:[,][ ]*)*")


class Manpages:
    """Represent a manpages instance in the workload."""

    def __init__(self, container: ops.Container):
        self.container = container

    def pebble_layer(self, releases, external_url) -> ops.pebble.Layer:
        """Return a Pebble layer for managing manpages server and ingestion."""
        # Validate the releases string before building the layer
        releases_list = RELEASES_PATTERN.findall(releases)
        if not releases_list:
            raise ValueError("failed to build manpages config: invalid releases specified")

        proxy_env = {
            "HTTP_PROXY": os.environ.get("JUJU_CHARM_HTTP_PROXY", ""),
            "HTTPS_PROXY": os.environ.get("JUJU_CHARM_HTTPS_PROXY", ""),
            "NO_PROXY": os.environ.get("JUJU_CHARM_NO_PROXY", ""),
        }
        app_config = {
            "MANPAGES_RELEASES": releases,
            "MANPAGES_SITE": external_url,
            "MANPAGES_ARCHIVE": "https://archive.ubuntu.com/ubuntu",
            "MANPAGES_PUBLIC_HTML_DIR": str(WWW_DIR),
            "MANPAGES_REPOS": "main, restricted, universe, multiverse",
            "MANPAGES_ARCH": "amd64",
            "MANPAGES_LOG_LEVEL": "info",
        }

        return ops.pebble.Layer(
            {
                "services": {
                    "manpages": {
                        "override": "replace",
                        "summary": "manpages server",
                        "command": "/usr/bin/server",
                        "startup": "enabled",
                        "environment": {**proxy_env, **app_config},
                    },
                    "ingest": {
                        "override": "replace",
                        "summary": "manpages ingestion",
                        "command": "/usr/bin/ingest",
                        "startup": "enabled",
                        "on-success": "ignore",
                        "environment": {**proxy_env, **app_config},
                    },
                },
                "checks": {
                    "up": {
                        "override": "replace",
                        "level": "alive",
                        "period": "30s",
                        "tcp": {"port": PORT},
                        "startup": "enabled",
                    },
                },
            }
        )

    def update_manpages(self, releases):
        """Update the manpages."""
        try:
            self.container.restart("ingest")
            self.purge_unused_manpages(releases)
        except (ProtocolError, ConnectionError, APIError) as e:
            logger.error("failed to ingest manpages: %s", e)
            raise

    def purge_unused_manpages(self, releases):
        """Purge unused manpages.

        If a release is no longer configured in the application config, but
        previously was, this function removes the manpages for that release.
        """
        # No releases have yet been downloaded, skip this step
        try:
            if not self.container.exists(WWW_DIR / "manpages"):
                return
        except (ProtocolError, ConnectionError, APIError) as e:
            logger.error("failed to check existence of manpages directory: %s", e)
            raise

        releases_list = RELEASES_PATTERN.findall(releases)
        if not releases_list:
            raise ValueError("failed to build manpages config: invalid releases specified")

        try:
            files = self.container.list_files(WWW_DIR / "manpages")
        except (ProtocolError, ConnectionError, PathError, APIError) as e:
            logger.error("failed to list manpages directory: %s", e)
            raise

        releases_on_disk = [f.name for f in files if f.type == ops.pebble.FileType.DIRECTORY]

        for release in releases_on_disk:
            if release in releases_list:
                continue

            logger.info("purging manpages for '%s'", release)
            try:
                self.container.remove_path(WWW_DIR / "manpages" / release, recursive=True)
            except (ProtocolError, ConnectionError, PathError, APIError) as e:
                logger.error("failed to remove manpages for '%s': %s", release, e)
                raise

    @property
    def updating(self) -> bool:
        """Report whether the manpages are currently being updated."""
        try:
            running = self.container.get_service("ingest").is_running()
            return running
        except (ProtocolError, ConnectionError, APIError, ops.ModelError) as e:
            logger.error("failed to get manpages ingest service status: %s", e)
            return False
