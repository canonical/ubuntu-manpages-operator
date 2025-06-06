# Copyright 2025 Canonical
# See LICENSE file for licensing details.

import jubilant
import requests

from . import MANPAGES, retry


def deploy_wait_func(status):
    """Wait function to ensure the app is in maintenance mode and updating manpages."""
    all_maint = jubilant.all_maintenance(status)
    status_message = status.apps[MANPAGES].app_status.message == "Updating manpages"
    return all_maint and status_message


def address(juju: jubilant.Juju):
    """Report the IP address of the application."""
    return juju.status().apps[MANPAGES].units[f"{MANPAGES}/0"].public_address


def test_deploy(juju: jubilant.Juju, manpages_charm):
    juju.deploy(manpages_charm, app=MANPAGES, config={"releases": "noble"})
    juju.wait(deploy_wait_func)


@retry(retry_num=10, retry_sleep_sec=3)
def test_application_is_up(juju: jubilant.Juju):
    response = requests.get(f"http://{address(juju)}:8080")
    assert response.status_code == 200
    assert "Welcome to the Ubuntu Manpage Repository" in response.text


@retry(retry_num=10, retry_sleep_sec=3)
def test_application_is_downloading_manpages(juju: jubilant.Juju):
    response = requests.get(f"http://{address(juju)}:8080/manpages/noble/")
    assert response.status_code == 200
