# Copyright 2025 Canonical
# See LICENSE file for licensing details.

import json

import jubilant
import requests

from . import MANPAGES, TRAEFIK, retry


def deploy_wait_func(status):
    """Wait function to ensure desired state of the model post-deploy."""
    manpages_maint = status.apps[MANPAGES].is_maintenance
    status_message = status.apps[MANPAGES].app_status.message == "Updating manpages"
    traefik_active = status.apps[TRAEFIK].is_active
    return manpages_maint and status_message and traefik_active


def test_deploy(juju: jubilant.Juju, manpages_charm, manpages_oci_image):
    juju.deploy(
        manpages_charm,
        app=MANPAGES,
        config={"releases": "noble"},
        resources={"manpages-image": manpages_oci_image},
    )

    traefik_config = {"routing_mode": "subdomain", "external_hostname": "manpages.internal"}
    juju.deploy(TRAEFIK, config=traefik_config, trust=True)

    juju.integrate(MANPAGES, TRAEFIK)

    juju.wait(deploy_wait_func, timeout=1800)


def test_ingress_setup(juju: jubilant.Juju):
    """Test that Manpages/Traefik are configured correctly."""
    result = juju.run(f"{TRAEFIK}/0", "show-external-endpoints")
    j = json.loads(result.results["external-endpoints"])

    model_name = juju.model
    assert model_name is not None
    assert j[MANPAGES] == {"url": f"http://{model_name}-{MANPAGES}.manpages.internal/"}


@retry(retry_num=24, retry_sleep_sec=5)
def test_ingress_functions_correctly(juju: jubilant.Juju, traefik_lb_ip):
    model_name = juju.model
    assert model_name is not None

    response = requests.get(
        f"http://{traefik_lb_ip}/",
        headers={"Host": f"{model_name}-{MANPAGES}.manpages.internal"},
        timeout=30,
    )

    assert response.status_code == 200
    assert "<title>Ubuntu Manpages</title>" in response.text
