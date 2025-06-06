# Copyright 2025 Canonical
# See LICENSE file for licensing details.

from urllib.request import urlopen

import jubilant

from . import retry

MANPAGES = "manpages"


def wait_func(status):
    """Wait function to ensure the app is in maintenance mode and updating manpages."""
    all_maint = jubilant.all_maintenance(status)
    status_message = status.apps[MANPAGES].app_status.message == "Updating manpages"
    return all_maint and status_message


def address(juju: jubilant.Juju):
    """Report the IP address of the application."""
    return juju.status().apps[MANPAGES].units[f"{MANPAGES}/0"].public_address


def test_deploy(juju: jubilant.Juju, manpages_charm):
    juju.deploy(manpages_charm, app=MANPAGES, config={"releases": "plucky"})
    juju.wait(wait_func)


@retry(retry_num=10, retry_sleep_sec=3)
def test_application_is_up(juju: jubilant.Juju):
    response = urlopen(f"http://{address(juju)}:8080")
    assert response.status == 200


@retry(retry_num=10, retry_sleep_sec=3)
def test_application_is_downloading_manpages(juju: jubilant.Juju):
    response = urlopen(f"http://{address(juju)}:8080/manpages/plucky/")
    assert response.status == 200
