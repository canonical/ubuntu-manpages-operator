# Copyright 2025 Canonical
# See LICENSE file for licensing details.

"""Unit tests for the charm.

These tests only cover those methods that do not require internet access,
and do not attempt to manipulate the underlying machine.
"""

from subprocess import CalledProcessError
from unittest.mock import PropertyMock, patch

import pytest
from charms.operator_libs_linux.v0.apt import PackageError, PackageNotFoundError
from ops.testing import (
    ActiveStatus,
    Address,
    BindAddress,
    BlockedStatus,
    Context,
    MaintenanceStatus,
    Network,
    Relation,
    State,
    TCPPort,
)

from charm import ManpagesCharm


@pytest.fixture
def ctx():
    return Context(ManpagesCharm)


@pytest.fixture
def base_state(ctx):
    return State(leader=True)


@patch("charm.Manpages.install")
def test_install_success(install_mock, ctx, base_state):
    install_mock.return_value = True
    out = ctx.run(ctx.on.install(), base_state)
    assert out.unit_status == MaintenanceStatus("Installing manpages")
    assert install_mock.called


@patch("charm.Manpages.install")
@pytest.mark.parametrize(
    "exception", [PackageError, PackageNotFoundError, CalledProcessError(1, "foo")]
)
def test_install_failure(mock, exception, ctx, base_state):
    mock.side_effect = exception
    out = ctx.run(ctx.on.install(), base_state)
    assert out.unit_status == BlockedStatus(
        "Failed to install packages. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.install")
def test_upgrade_success(install_mock, ctx, base_state):
    install_mock.return_value = True
    out = ctx.run(ctx.on.upgrade_charm(), base_state)
    assert out.unit_status == MaintenanceStatus("Installing manpages")
    assert install_mock.called


@patch("charm.Manpages.install")
@pytest.mark.parametrize(
    "exception", [PackageError, PackageNotFoundError, CalledProcessError(1, "foo")]
)
def test_upgrade_failure(mock, exception, ctx, base_state):
    mock.side_effect = exception
    out = ctx.run(ctx.on.upgrade_charm(), base_state)
    assert out.unit_status == BlockedStatus(
        "Failed to install packages. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_config_changed(update_manpages_mock, configure_mock, ctx, base_state):
    out = ctx.run(ctx.on.config_changed(), base_state)
    assert out.unit_status == MaintenanceStatus("Updating manpages")
    assert configure_mock.called
    assert update_manpages_mock.called


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_update_manpages_action(update_manpages_mock, configure_mock, ctx, base_state):
    out = ctx.run(ctx.on.action("update-manpages"), base_state)
    assert out.unit_status == MaintenanceStatus("Updating manpages")
    assert configure_mock.called
    assert update_manpages_mock.called


@patch("charm.Manpages.configure")
def test_config_changed_failed_bad_config(configure_mock, ctx, base_state):
    configure_mock.side_effect = ValueError
    out = ctx.run(ctx.on.config_changed(), base_state)
    assert out.unit_status == BlockedStatus(
        "Invalid configuration. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_config_changed_failed_bad_update(update_manpages_mock, configure_mock, ctx, base_state):
    update_manpages_mock.side_effect = CalledProcessError(1, "foo")
    out = ctx.run(ctx.on.config_changed(), base_state)
    assert configure_mock.called
    assert out.unit_status == MaintenanceStatus(
        "Failed to update manpages. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.restart")
def test_start_success(restart_mock, ctx, base_state):
    out = ctx.run(ctx.on.start(), base_state)
    assert out.unit_status == ActiveStatus()
    assert restart_mock.called
    assert out.opened_ports == {TCPPort(port=8080, protocol="tcp")}


@patch("charm.Manpages.restart")
@pytest.mark.parametrize("exception", [CalledProcessError(1, "foo")])
def test_start_failure(mock, exception, ctx, base_state):
    mock.side_effect = exception
    out = ctx.run(ctx.on.start(), base_state)
    assert out.unit_status == BlockedStatus(
        "Failed to start services. Check `juju debug-log` for details."
    )
    assert out.opened_ports == frozenset()


@patch("charm.Manpages.updating", new_callable=PropertyMock)
def test_update_status_updating(updating_mock, ctx, base_state):
    updating_mock.return_value = True
    out = ctx.run(ctx.on.update_status(), base_state)
    assert out.unit_status == MaintenanceStatus("Updating manpages")
    assert updating_mock.called


@patch("charm.Manpages.updating", new_callable=PropertyMock)
def test_update_status_idle(updating_mock, ctx, base_state):
    updating_mock.return_value = False
    out = ctx.run(ctx.on.update_status(), base_state)
    assert out.unit_status == ActiveStatus()
    assert updating_mock.called


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_ingress_no_ingress_workload_url(update_manpages_mock, configure_mock):
    ctx = Context(ManpagesCharm)

    state = State(config={"releases": "noble"})
    ctx.run(ctx.on.config_changed(), state)

    configure_mock.assert_called_with("noble", "http://192.0.2.0:8080")


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_ingress_relation_no_ingress_juju_info_binding(update_manpages_mock, configure_mock):
    ctx = Context(ManpagesCharm)

    state = State(
        config={"releases": "noble"},
        networks={Network("juju-info", [BindAddress([Address("10.10.10.10")])])},
    )
    ctx.run(ctx.on.config_changed(), state)

    configure_mock.assert_called_with("noble", "http://10.10.10.10:8080")


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_ingress_relation_updates_workload_config(update_manpages_mock, configure_mock):
    rel = Relation(
        endpoint="ingress",
        interface="ingress",
        remote_app_name="haproxy",
        local_unit_data={
            "model": "testing",
            "name": "ubuntu-manpages",
            "port": "8080",
            "strip-prefix": "true",
        },
        remote_app_data={
            "ingress": '{"url": "https://manpages.internal/testing-ubuntu-manpages/"}'
        },
    )

    ctx = Context(ManpagesCharm)

    state = State(relations=[rel], config={"releases": "noble"})
    ctx.run(ctx.on.config_changed(), state)

    configure_mock.assert_called_with(
        "noble", "https://manpages.internal/testing-ubuntu-manpages/"
    )
