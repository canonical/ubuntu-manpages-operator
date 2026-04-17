# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Unit tests for the charm.

These tests only cover those methods that do not require internet access,
and do not attempt to manipulate the underlying machine.
"""

from unittest.mock import patch

import pytest
from ops import BlockedStatus
from ops.pebble import CheckLevel, CheckStatus, Layer, ServiceStatus
from ops.testing import ActiveStatus, CheckInfo, Context, MaintenanceStatus, State, TCPPort
from scenario import Container

from charm import ManpagesCharm
from manpages import Manpages


@pytest.fixture
def charm():
    yield ManpagesCharm


@pytest.fixture
def loaded_ctx(charm):
    ctx = Context(charm)
    container = Container(name="manpages", can_connect=True)
    return (ctx, container)


@pytest.fixture
def loaded_ctx_broken_container(charm):
    ctx = Context(charm)
    container = Container(name="manpages", can_connect=False)
    return (ctx, container)


def test_manpages_pebble_ready(loaded_ctx):
    ctx, container = loaded_ctx
    state = State(containers=[container], config={"releases": "noble"})
    manpages = Manpages(container)

    result = ctx.run(ctx.on.pebble_ready(container=container), state)

    layer = manpages.pebble_layer("noble", "http://192.0.2.0:8080")
    assert result.get_container("manpages").layers["manpages"] == layer
    checks = layer.checks
    assert "ready" in checks
    assert checks["ready"].http == {"url": "http://localhost:9090/_/healthz"}
    assert result.get_container("manpages").service_statuses == {
        "manpages": ServiceStatus.ACTIVE,
        "ingest": ServiceStatus.ACTIVE,
    }
    assert result.opened_ports == frozenset({TCPPort(8080)})
    assert result.unit_status == MaintenanceStatus("Updating manpages")


def test_manpages_config_changed_purges_old_releases(loaded_ctx):
    ctx, container = loaded_ctx
    state = State(containers=[container], config={"releases": "noble"})

    result = ctx.run(ctx.on.config_changed(), state)

    container_root_fs = result.get_container(container.name).get_filesystem(ctx)
    noble_dir = container_root_fs / "app" / "www" / "manpages" / "noble"
    # Simulate the actual fetch from online happening and populating the noble directory.
    noble_dir.mkdir(parents=True, exist_ok=True)
    assert noble_dir.exists()

    # Reconfigure to remove the noble release and check the directory is pruned.
    state = State(containers=[container], config={"releases": "questing"})
    result = ctx.run(ctx.on.config_changed(), state)

    container_root_fs = result.get_container(container.name).get_filesystem(ctx)
    noble_dir = container_root_fs / "app" / "www" / "manpages" / "noble"
    assert not noble_dir.exists()


def test_manpages_config_changed_no_pebble(loaded_ctx_broken_container):
    ctx, container = loaded_ctx_broken_container
    state = State(containers=[container], config={"releases": "noble"})
    result = ctx.run(ctx.on.config_changed(), state)

    assert result.unit_status == BlockedStatus(
        "Failed to connect to workload container. Check `juju debug-log` for details."
    )


def test_manpages_update_status_updating(loaded_ctx):
    ctx, container = loaded_ctx
    container = Container(
        name="manpages",
        can_connect=True,
        layers={
            "manpages": Layer(
                {
                    "services": {
                        "ingest": {
                            "override": "replace",
                            "command": "/usr/bin/ingest",
                            "startup": "enabled",
                        },
                    },
                }
            )
        },
        service_statuses={"ingest": ServiceStatus.ACTIVE},
    )
    state = State(containers=[container], config={"releases": "noble"})
    result = ctx.run(ctx.on.update_status(), state)

    assert result.unit_status == MaintenanceStatus("Updating manpages")


def test_manpages_update_status_not_updating(loaded_ctx):
    ctx, container = loaded_ctx
    container = Container(
        name="manpages",
        can_connect=True,
        layers={
            "manpages": Layer(
                {
                    "services": {
                        "ingest": {
                            "override": "replace",
                            "command": "/usr/bin/ingest",
                            "startup": "enabled",
                        },
                    },
                }
            )
        },
        service_statuses={"ingest": ServiceStatus.INACTIVE},
    )
    state = State(containers=[container], config={"releases": "noble"})
    result = ctx.run(ctx.on.update_status(), state)

    assert result.unit_status == ActiveStatus()


CHECKS_LAYER = Layer(
    {
        "checks": {
            "up": {
                "override": "replace",
                "level": "alive",
                "period": "30s",
                "tcp": {"port": 8080},
                "startup": "enabled",
                "threshold": 3,
            },
            "ready": {
                "override": "replace",
                "level": "ready",
                "period": "1m",
                "http": {"url": "http://localhost:9090/_/healthz"},
                "startup": "enabled",
                "threshold": 3,
            },
        },
    }
)


@patch("manpages.Manpages.get_health_error", return_value="low disk space on manpages storage")
def test_pebble_check_failed_disk_full(mock_health, charm):
    ctx = Context(charm)
    check_info = CheckInfo(
        "ready",
        level=CheckLevel.READY,
        failures=3,
        status=CheckStatus.DOWN,
        threshold=3,
    )
    container = Container(
        name="manpages",
        can_connect=True,
        layers={"manpages": CHECKS_LAYER},
        check_infos={check_info},
    )
    state = State(containers=[container], config={"releases": "noble"})

    result = ctx.run(ctx.on.pebble_check_failed(container, info=check_info), state)

    assert result.unit_status == MaintenanceStatus("low disk space on manpages storage")


def test_pebble_check_recovered(charm):
    ctx = Context(charm)
    check_info = CheckInfo(
        "ready",
        level=CheckLevel.READY,
        status=CheckStatus.UP,
        threshold=3,
    )
    container = Container(
        name="manpages",
        can_connect=True,
        layers={"manpages": CHECKS_LAYER},
        check_infos={check_info},
    )
    state = State(containers=[container], config={"releases": "noble"})

    result = ctx.run(ctx.on.pebble_check_recovered(container, info=check_info), state)

    assert result.unit_status == ActiveStatus()


@patch("manpages.Manpages.get_health_error", return_value="low disk space on manpages storage")
def test_pebble_check_failed_ignores_other_checks(mock_health, charm):
    """Only the 'ready' check should trigger MaintenanceStatus."""
    ctx = Context(charm)
    check_info = CheckInfo(
        "up",
        level=CheckLevel.ALIVE,
        failures=3,
        status=CheckStatus.DOWN,
        threshold=3,
    )
    container = Container(
        name="manpages",
        can_connect=True,
        layers={"manpages": CHECKS_LAYER},
        check_infos={check_info},
    )
    state = State(containers=[container], config={"releases": "noble"})

    result = ctx.run(ctx.on.pebble_check_failed(container, info=check_info), state)

    # Status should NOT be changed for the 'up' check.
    assert result.unit_status != MaintenanceStatus("low disk space on manpages storage")
