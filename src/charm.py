#!/usr/bin/env python3
# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Charmed Operator for manpages.ubuntu.com."""

import logging
import socket
from subprocess import CalledProcessError

import ops
from charms.operator_libs_linux.v0.apt import PackageError, PackageNotFoundError
from charms.traefik_k8s.v2.ingress import IngressPerAppRequirer as IngressRequirer

from launchpad import LaunchpadClient
from manpages import Manpages

logger = logging.getLogger(__name__)

PORT = 8080


class ManpagesCharm(ops.CharmBase):
    """Charmed Operator for manpages.ubuntu.com."""

    def __init__(self, framework: ops.Framework):
        super().__init__(framework)
        self.ingress = IngressRequirer(self, port=PORT, strip_prefix=True, relation_name="ingress")

        framework.observe(self.on.install, self._on_install)
        framework.observe(self.on.start, self._on_start)
        framework.observe(self.on.upgrade_charm, self._on_install)
        framework.observe(self.on.update_status, self._on_update_status)
        framework.observe(self.on.update_manpages_action, self._on_config_changed)
        framework.observe(self.on.config_changed, self._on_config_changed)

        # Ingress URL changes require updating the configuration and also regenerating sitemaps,
        # therefore we can bind events for this relation to the config_changed event.
        framework.observe(self.on.ingress_relation_changed, self._on_config_changed)
        framework.observe(self.on.ingress_relation_departed, self._on_config_changed)

        self._manpages = Manpages(LaunchpadClient())

    def _on_install(self, event: ops.InstallEvent):
        """Install the packages and configuration for ubuntu-manpages."""
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
            self._manpages.configure(self.config["releases"], self._get_external_url())
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

        self.unit.set_ports(PORT)

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

    def _get_external_url(self) -> str:
        """Report URL to access Ubuntu Manpages."""
        # Default: FQDN
        external_url = f"http://{socket.getfqdn()}:{PORT}"
        # If can connect to juju-info, get unit IP
        if binding := self.model.get_binding("juju-info"):
            unit_ip = str(binding.network.bind_address)
            external_url = f"http://{unit_ip}:{PORT}"
        # If ingress is set, get ingress url
        if self.ingress.url:
            external_url = self.ingress.url
        return external_url


if __name__ == "__main__":  # pragma: nocover
    ops.main(ManpagesCharm)
