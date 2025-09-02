# Copyright 2025 Canonical
# See LICENSE file for licensing details.

import jubilant
from requests import Session

from . import HAPROXY, MANPAGES, SSC, DNSResolverHTTPSAdapter, retry


def deploy_wait_func(status):
    """Wait function to ensure desired state of the model post-deploy."""
    manpages_maint = status.apps[MANPAGES].is_maintenance
    status_message = status.apps[MANPAGES].app_status.message == "Updating manpages"
    haproxy_active = status.apps[HAPROXY].is_active
    ssc_active = status.apps[SSC].is_active
    return manpages_maint and status_message and haproxy_active and ssc_active


def test_deploy(juju: jubilant.Juju, manpages_charm):
    juju.deploy(manpages_charm, app=MANPAGES, config={"releases": "noble"})
    juju.deploy(HAPROXY, channel="2.8/edge", config={"external-hostname": "manpages.internal"})
    juju.deploy(SSC, channel="1/edge")

    juju.integrate(MANPAGES, HAPROXY)
    juju.integrate(f"{HAPROXY}:certificates", f"{SSC}:certificates")

    juju.wait(deploy_wait_func, timeout=1800)


@retry(retry_num=24, retry_sleep_sec=5)
def test_ingress_functions_correctly(juju: jubilant.Juju):
    model_name = juju.model
    assert model_name is not None

    haproxy_ip = juju.status().apps[HAPROXY].units[f"{HAPROXY}/0"].public_address
    external_hostname = "manpages.internal"

    session = Session()
    session.mount("https://", DNSResolverHTTPSAdapter(external_hostname, haproxy_ip))
    response = session.get(
        f"https://{haproxy_ip}/{model_name}-{MANPAGES}/",
        headers={"Host": external_hostname},
        verify=False,
        timeout=30,
    )

    assert response.status_code == 200
    assert "Welcome to the Ubuntu Manpage Repository" in response.text
