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
from ops import testing

from charm import ManpagesCharm


@pytest.fixture
def ctx():
    return testing.Context(ManpagesCharm)


@pytest.fixture
def base_state(ctx):
    return testing.State(leader=True)


@patch("charm.Manpages.install")
def test_install_success(install_mock, ctx, base_state):
    install_mock.return_value = True
    out = ctx.run(ctx.on.install(), base_state)
    assert out.unit_status == testing.MaintenanceStatus("Installing manpages")
    assert install_mock.called


@patch("charm.Manpages.install")
@pytest.mark.parametrize(
    "exception", [PackageError, PackageNotFoundError, CalledProcessError(1, "foo")]
)
def test_install_failure(mock, exception, ctx, base_state):
    mock.side_effect = exception
    out = ctx.run(ctx.on.install(), base_state)
    assert out.unit_status == testing.BlockedStatus(
        "Failed to install packages. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.install")
def test_upgrade_success(install_mock, ctx, base_state):
    install_mock.return_value = True
    out = ctx.run(ctx.on.upgrade_charm(), base_state)
    assert out.unit_status == testing.MaintenanceStatus("Installing manpages")
    assert install_mock.called


@patch("charm.Manpages.install")
@pytest.mark.parametrize(
    "exception", [PackageError, PackageNotFoundError, CalledProcessError(1, "foo")]
)
def test_upgrade_failure(mock, exception, ctx, base_state):
    mock.side_effect = exception
    out = ctx.run(ctx.on.upgrade_charm(), base_state)
    assert out.unit_status == testing.BlockedStatus(
        "Failed to install packages. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_config_changed(update_manpages_mock, configure_mock, ctx, base_state):
    out = ctx.run(ctx.on.config_changed(), base_state)
    assert out.unit_status == testing.MaintenanceStatus("Updating manpages")
    assert configure_mock.called
    assert update_manpages_mock.called


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_update_manpages_action(update_manpages_mock, configure_mock, ctx, base_state):
    out = ctx.run(ctx.on.action("update-manpages"), base_state)
    assert out.unit_status == testing.MaintenanceStatus("Updating manpages")
    assert configure_mock.called
    assert update_manpages_mock.called


@patch("charm.Manpages.configure")
def test_config_changed_failed_bad_config(configure_mock, ctx, base_state):
    configure_mock.side_effect = ValueError
    out = ctx.run(ctx.on.config_changed(), base_state)
    assert out.unit_status == testing.BlockedStatus(
        "Invalid configuration. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.configure")
@patch("charm.Manpages.update_manpages")
def test_config_changed_failed_bad_update(update_manpages_mock, configure_mock, ctx, base_state):
    update_manpages_mock.side_effect = CalledProcessError(1, "foo")
    out = ctx.run(ctx.on.config_changed(), base_state)
    assert configure_mock.called
    assert out.unit_status == testing.MaintenanceStatus(
        "Failed to update manpages. Check `juju debug-log` for details."
    )


@patch("charm.Manpages.restart")
def test_start_success(restart_mock, ctx, base_state):
    out = ctx.run(ctx.on.start(), base_state)
    assert out.unit_status == testing.ActiveStatus()
    assert restart_mock.called
    assert out.opened_ports == {testing.TCPPort(port=8080, protocol="tcp")}


@patch("charm.Manpages.restart")
@pytest.mark.parametrize("exception", [CalledProcessError(1, "foo")])
def test_start_failure(mock, exception, ctx, base_state):
    mock.side_effect = exception
    out = ctx.run(ctx.on.start(), base_state)
    assert out.unit_status == testing.BlockedStatus(
        "Failed to start services. Check `juju debug-log` for details."
    )
    assert out.opened_ports == frozenset()


@patch("charm.Manpages.updating", new_callable=PropertyMock)
def test_update_status_updating(updating_mock, ctx, base_state):
    updating_mock.return_value = True
    out = ctx.run(ctx.on.update_status(), base_state)
    assert out.unit_status == testing.MaintenanceStatus("Updating manpages")
    assert updating_mock.called


@patch("charm.Manpages.updating", new_callable=PropertyMock)
def test_update_status_idle(updating_mock, ctx, base_state):
    updating_mock.return_value = False
    out = ctx.run(ctx.on.update_status(), base_state)
    assert out.unit_status == testing.ActiveStatus()
    assert updating_mock.called
