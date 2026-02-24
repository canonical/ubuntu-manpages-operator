#!/usr/bin/env python3
# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Charmed Operator for manpages.ubuntu.com."""

import logging
import socket

import ops
from charms.traefik_k8s.v2.ingress import IngressPerAppRequirer as IngressRequirer
from ops.pebble import APIError, ConnectionError, ProtocolError

from manpages import PORT, Manpages

logger = logging.getLogger(__name__)


class ManpagesCharm(ops.CharmBase):
    """Charmed Operator for manpages.ubuntu.com."""

    def __init__(self, framework: ops.Framework):
        super().__init__(framework)
        framework.observe(self.on.manpages_pebble_ready, self._on_manpages_pebble_ready)

        self._container = self.unit.get_container("manpages")
        self._manpages = Manpages(self._container)

        self.ingress = IngressRequirer(
            self,
            host=f"{self.app.name}.{self.model.name}.svc.cluster.local",
            port=PORT,
            strip_prefix=True,
        )

        framework.observe(self.on.update_status, self._on_update_status)
        framework.observe(self.on.update_manpages_action, self._on_config_changed)
        framework.observe(self.on.config_changed, self._on_config_changed)

        # Ingress URL changes require updating the configuration and also regenerating sitemaps,
        # therefore we can bind events for this relation to the config_changed event.
        framework.observe(self.ingress.on.ready, self._on_config_changed)
        framework.observe(self.ingress.on.revoked, self._on_config_changed)

    def _on_manpages_pebble_ready(self, event: ops.PebbleReadyEvent):
        """Add the manpages layer to Pebble and start the services."""
        self._replan_workload()

    def _on_config_changed(self, event):
        """Update configuration and fetch relevant manpages."""
        self.unit.status = ops.MaintenanceStatus("Updating configuration")
        self._replan_workload()

    def _replan_workload(self):
        container = self._container

        try:
            self._manpages.configure(str(self.config["releases"]), self._get_external_url())
        except ValueError:
            self.unit.status = ops.BlockedStatus(
                "Invalid configuration. Check `juju debug-log` for details."
            )
            return
        except (ProtocolError, ConnectionError, APIError) as e:
            logger.error("failed to configure manpages app: %s", e)
            self.unit.status = ops.BlockedStatus(
                "Failed to connect to workload container. Check `juju debug-log` for details."
            )
            return

        try:
            container.add_layer("manpages", self._manpages.pebble_layer(), combine=True)
            container.replan()
        except (ConnectionError, ProtocolError, APIError) as e:
            logger.error("failed to add manpages layer to pebble: %s", e)
            self.unit.status = ops.BlockedStatus(
                "Failed to connect to workload container. Check `juju debug-log` for details."
            )
            return

        self.unit.open_port(protocol="tcp", port=PORT)

        self.unit.status = ops.MaintenanceStatus("Updating manpages")
        try:
            self._manpages.update_manpages()
        except (ProtocolError, ConnectionError, APIError) as e:
            logger.error("failed to ingest manpages: %s", e)
            self.unit.status = ops.BlockedStatus(
                "Failed to connect to workload container. Check `juju debug-log` for details."
            )
            return

    def _on_update_status(self, event: ops.UpdateStatusEvent):
        """Update status."""
        self.unit.status = self._get_status()

    def _get_status(self) -> ops.StatusBase:
        """Return the current status of the unit."""
        if self._manpages.updating:
            return ops.MaintenanceStatus("Updating manpages")
        else:
            return ops.ActiveStatus()

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
