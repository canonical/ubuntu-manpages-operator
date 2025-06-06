#!/usr/bin/env python3
# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Charmed Operator for manpages.ubuntu.com."""

import logging
from subprocess import CalledProcessError

import ops
from charms.operator_libs_linux.v0.apt import PackageError, PackageNotFoundError

from launchpad import LaunchpadClient
from manpages import Manpages

logger = logging.getLogger(__name__)


class ManpagesCharm(ops.CharmBase):
    """Charmed Operator for manpages.ubuntu.com."""

    def __init__(self, *args):
        super().__init__(*args)
        self.framework.observe(self.on.install, self._on_install)
        self.framework.observe(self.on.start, self._on_start)
        self.framework.observe(self.on.upgrade_charm, self._on_install)
        self.framework.observe(self.on.update_status, self._on_update_status)
        self.framework.observe(self.on.update_manpages_action, self._on_config_changed)
        self.framework.observe(self.on.config_changed, self._on_config_changed)

        self._manpages = Manpages(LaunchpadClient())

    def _on_install(self, event: ops.InstallEvent):
        """Define and start a workload using the Pebble API."""
        self.unit.status = ops.MaintenanceStatus("Installing manpages")
        try:
            self._manpages.install()
        except (CalledProcessError, PackageError, PackageNotFoundError):
            self.unit.status = ops.BlockedStatus(
                "Failed to install packages. Check `juju debug-log` for details."
            )
            return

    def _on_config_changed(self, event: ops.ConfigChangedEvent):
        """Update configuration and fetch relevant manpages."""
        self.unit.status = ops.MaintenanceStatus("Updating configuration")
        try:
            self._manpages.configure(self.config["releases"])
        except ValueError:
            self.unit.status = ops.BlockedStatus(
                "Invalid configuration. Check `juju debug-log` for details."
            )
            return

        self.unit.status = ops.MaintenanceStatus("Updating manpages")
        try:
            self._manpages.update_manpages()
        except CalledProcessError:
            self.unit.status = ops.MaintenanceStatus(
                "Failed to update manpages. Check `juju debug-log` for details."
            )
            return

    def _on_start(self, event: ops.StartEvent):
        """Start the manpages service."""
        self.unit.status = ops.MaintenanceStatus("Starting manpages")
        try:
            self._manpages.restart()
        except CalledProcessError:
            self.unit.status = ops.BlockedStatus(
                "Failed to start services. Check `juju debug-log` for details."
            )
            return

        self.unit.set_ports(8080)

        if self._manpages.updating:
            self.unit.status = ops.MaintenanceStatus("Updating manpages")
        else:
            self.unit.status = ops.ActiveStatus()

    def _on_update_status(self, event: ops.UpdateStatusEvent):
        """Update status."""
        if self._manpages.updating:
            self.unit.status = ops.MaintenanceStatus("Updating manpages")
        else:
            self.unit.status = ops.ActiveStatus()


if __name__ == "__main__":  # pragma: nocover
    ops.main(ManpagesCharm)
