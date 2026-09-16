# Copyright 2025 Canonical
# See LICENSE file for licensing details.

from pathlib import Path

import jubilant
import pytest
import requests
import yaml

from . import MANPAGES, retry
from .non_root import (
    assert_security_context,
    assert_workload_access,
    generate_container_securitycontext_map,
    get_pods,
)

CONTAINERS_SECURITY_CONTEXT_MAP = generate_container_securitycontext_map(
    yaml.safe_load(Path("charmcraft.yaml").read_text())
)


def deploy_wait_func(status):
    """Wait function to ensure the app is in maintenance mode and updating manpages."""
    all_maint = jubilant.all_maintenance(status)
    status_message = status.apps[MANPAGES].app_status.message == "Updating manpages"
    return all_maint and status_message


def address(juju: jubilant.Juju):
    """Report the IP address of the application."""
    return juju.status().apps[MANPAGES].units[f"{MANPAGES}/0"].public_address


def test_deploy(juju: jubilant.Juju, manpages_charm, manpages_oci_image):
    juju.deploy(
        manpages_charm,
        app=MANPAGES,
        config={"releases": "noble"},
        resources={"manpages-image": manpages_oci_image},
    )
    juju.wait(deploy_wait_func, timeout=600)


@pytest.mark.parametrize("container_name", list(CONTAINERS_SECURITY_CONTEXT_MAP))
def test_container_security_context(juju: jubilant.Juju, container_name: str):
    """Verify all deployed pods run the charm and workload with non-root identities."""
    assert juju.model is not None
    for pod in get_pods(juju.model, MANPAGES):
        assert_security_context(
            pod, container_name, CONTAINERS_SECURITY_CONTEXT_MAP[container_name]
        )


@retry(retry_num=10, retry_sleep_sec=3)
def test_application_is_up(juju: jubilant.Juju):
    address = juju.status().apps[MANPAGES].units[f"{MANPAGES}/0"].address
    response = requests.get(f"http://{address}:8080")
    assert response.status_code == 200
    assert "<title>Ubuntu Manpages</title>" in response.text


def test_workload_data_access(juju: jubilant.Juju):
    """Check non-root access to the data tree, including Juju storage mounts."""
    assert juju.model is not None
    for pod in get_pods(juju.model, MANPAGES):
        assert_workload_access(juju.model, pod["metadata"]["name"], "manpages")


@retry(retry_num=10, retry_sleep_sec=3)
def test_application_is_downloading_manpages(juju: jubilant.Juju):
    address = juju.status().apps[MANPAGES].units[f"{MANPAGES}/0"].address
    response = requests.get(f"http://{address}:8080/manpages/noble/")
    assert response.status_code == 200
