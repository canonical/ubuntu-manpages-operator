# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Unit tests for the charm.

These tests only cover those methods that do not require internet access,
and do not attempt to manipulate the underlying machine.
"""

import json
from unittest import mock

import pytest
from ops import BlockedStatus
from ops.pebble import Layer, ServiceStatus
from ops.testing import ActiveStatus, Context, MaintenanceStatus, State, TCPPort
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
    manpages = Manpages(None, container)

    result = ctx.run(ctx.on.pebble_ready(container=container), state)

    assert result.get_container("manpages").layers["manpages"] == manpages.pebble_layer()
    assert result.get_container("manpages").service_statuses == {
        "manpages": ServiceStatus.ACTIVE,
        "ingest": ServiceStatus.ACTIVE,
    }
    assert result.opened_ports == frozenset({TCPPort(8080)})
    assert result.unit_status == MaintenanceStatus("Updating manpages")


def test_manpages_config_changed(loaded_ctx):
    ctx, container = loaded_ctx
    state = State(containers=[container], config={"releases": "noble"})

    # Start with intial config changed to populate the config file in the container.
    result = ctx.run(ctx.on.config_changed(), state)
    container_root_fs = result.get_container(container.name).get_filesystem(ctx)
    cfg_file = container_root_fs / "app" / "www" / "config.json"
    cfg_json = json.loads(cfg_file.read_text())

    # Ensure the config file is correctly rendered with the noble release.
    assert cfg_json == {
        "site": "http://192.0.2.0:8080",
        "archive": "http://archive.ubuntu.com/ubuntu",
        "public_html_dir": "/app/www",
        "releases": {"noble": "24.04"},
        "repos": ["main", "restricted", "universe", "multiverse"],
        "arch": "amd64",
    }

    # Run config changed with a new release
    state = State(containers=[container], config={"releases": "questing"})
    result = ctx.run(ctx.on.config_changed(), state)
    container_root_fs = result.get_container(container.name).get_filesystem(ctx)
    cfg_file = container_root_fs / "app" / "www" / "config.json"
    cfg_json = json.loads(cfg_file.read_text())

    # Ensure the nwe config file reflects the new release and old release is removed.
    assert cfg_json == {
        "site": "http://192.0.2.0:8080",
        "archive": "http://archive.ubuntu.com/ubuntu",
        "public_html_dir": "/app/www",
        "releases": {"questing": "25.10"},
        "repos": ["main", "restricted", "universe", "multiverse"],
        "arch": "amd64",
    }


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


def test_manpages_config_changed_invalid_value(loaded_ctx):
    ctx, container = loaded_ctx
    state = State(containers=[container], config={"releases": "foobarbaz"})
    result = ctx.run(ctx.on.config_changed(), state)

    assert result.unit_status == BlockedStatus(
        "Invalid configuration. Check `juju debug-log` for details."
    )


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
                            "command": "/usr/bin/ingest -config=/app/www/config.json",
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
                            "command": "/usr/bin/ingest -config=/app/www/config.json",
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
