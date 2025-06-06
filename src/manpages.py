# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Representation of the manpages service."""

import json
import logging
import os
import re
import shutil
from dataclasses import asdict, dataclass, field
from pathlib import Path
from subprocess import CalledProcessError

import charms.operator_libs_linux.v0.apt as apt
from charms.operator_libs_linux.v0.apt import PackageError, PackageNotFoundError
from charms.operator_libs_linux.v1.systemd import service_restart, service_running

logger = logging.getLogger(__name__)

# Used to fetch release codenames from the config string passed to the charm.
RELEASES_PATTERN = re.compile(r"([a-z]+)(?:[,][ ]*)*")

# Directories required by the manpages charm.
APP_DIR = Path("/app")
DEB_DIR = APP_DIR / "ubuntu"
WWW_DIR = APP_DIR / "www"
BIN_DIR = APP_DIR / "bin"

# Configuration files created by the manpages charm.
CONFIG_PATH = WWW_DIR / "config.json"
UPDATE_SERVICE_PATH = Path("/etc/systemd/system/update-manpages.service")
NGINX_SITE_CONFIG_PATH = Path("/etc/nginx/conf.d/manpages.conf")

# Packages installed as part of the update process.
PACKAGES = ["nginx-full", "fcgiwrap", "jq", "curl", "w3m"]


@dataclass
class ManpagesConfig:
    """Configuration for manpages service."""

    site: str = "http://manpages.ubuntu.com"
    archive: str = "http://archive.ubuntu.com/ubuntu"
    debdir: str = str(DEB_DIR)
    public_html_dir: str = str(WWW_DIR)
    releases: dict = field(
        default_factory=lambda: {
            "jammy": "22.04",
            "noble": "24.04",
            "oracular": "24.10",
            "plucky": "25.04",
            "questing": "25.10",
        }
    )
    repos: list = field(default_factory=lambda: ["main", "restricted", "universe", "multiverse"])
    arch: str = "amd64"


class Manpages:
    """Represent a manpages instance in the workload."""

    def __init__(self, launchpad_client):
        self.launchpad_client = launchpad_client

    def install(self):
        """Install manpages."""
        try:
            apt.update()
        except CalledProcessError as e:
            logger.error("failed to update package cache: %s", e)
            raise

        for p in PACKAGES:
            try:
                apt.add_package(p)
            except PackageNotFoundError:
                logger.error("failed to find package %s in package cache", p)
                raise
            except PackageError as e:
                logger.error("failed to install %s: %s", p, e)
                raise

        # Get path to charm source, inside which is the app and its configuration
        source_path = Path(__file__).parent.parent / "app"

        # Install the web assets and maintenance scripts
        APP_DIR.mkdir(parents=True, exist_ok=True)
        (WWW_DIR / "manpages").mkdir(parents=True, exist_ok=True)
        shutil.copytree(source_path / "www", WWW_DIR, dirs_exist_ok=True)
        shutil.copytree(source_path / "bin", BIN_DIR, dirs_exist_ok=True)

        # Install configuration files
        config_path = source_path / "config"
        shutil.copy(config_path / "manpages.conf", NGINX_SITE_CONFIG_PATH)
        shutil.copy(config_path / "update-manpages.service", UPDATE_SERVICE_PATH)

        # Remove default nginx configuration
        Path("/etc/nginx/sites-enables/default").unlink(missing_ok=True)

        # Ensure the "/app" directory is owned by the "www-data" user.
        for dirpath, dirnames, filenames in os.walk(APP_DIR):
            shutil.chown(dirpath, "www-data")
            for filename in filenames:
                path = Path(dirpath) / Path(filename)
                try:
                    shutil.chown(path, "www-data")
                except FileNotFoundError:
                    logger.debug("failed to change ownership of '%s'", path)

    def configure(self, releases: str, url: str):
        """Configure the manpages service."""
        try:
            config = self._build_config(releases, url)
        except ValueError as e:
            logger.error("failed to build manpages configuration: invalid releases spec: %s", e)
            raise

        with open(CONFIG_PATH, "w") as f:
            json.dump(asdict(config), f)

    def restart(self):
        """Restart the manpages services."""
        try:
            service_restart("nginx")
            service_restart("fcgiwrap")
        except CalledProcessError as e:
            logger.error("failed to restart manpages services: %s", e)
            raise

    def update_manpages(self):
        """Update the manpages."""
        try:
            service_restart("update-manpages")
            self.purge_unused_manpages()
        except CalledProcessError as e:
            logger.error("failed to update manpages: %s", e)
            raise

    def purge_unused_manpages(self):
        """Purge unused manpages.

        If a release is no longer configured in the application config, but
        previously was, this function removes the manpages for that release.
        """
        with open(CONFIG_PATH, "r") as f:
            config = json.load(f)

        configured_releases = config["releases"].keys()
        releases_on_disk = [f.name for f in os.scandir(WWW_DIR / "manpages") if f.is_dir()]

        for release in releases_on_disk:
            if release not in configured_releases:
                logger.info("purging manpages for '%s'", release)
                shutil.rmtree(WWW_DIR / "manpages" / release)

    @property
    def updating(self) -> bool:
        """Report whether the manpages are currently being updated."""
        return service_running("update-manpages")

    def _build_config(self, releases: str, url: str) -> ManpagesConfig:
        """Build a ManpagesConfig object using a set of specified release codenames."""
        releases_list = RELEASES_PATTERN.findall(releases)
        if not releases_list:
            raise ValueError("failed to build manpages config: invalid releases specified")

        config = ManpagesConfig()
        config.site = url

        # Get the release map for the specified release codenames.
        try:
            config.releases = self.launchpad_client.release_map(releases_list)
        except ValueError as e:
            logger.error("failed to build manpages config: %s", e)
            raise ValueError(f"failed to build manpages config: {e}")

        return config
