# Copyright 2026 Canonical
# See LICENSE file for licensing details.

"""Helpers for checking deployed container security contexts."""

import json
import subprocess


def generate_container_securitycontext_map(metadata: dict) -> dict[str, dict[str, int]]:
    """Read workload identities and include the Juju charm identity."""
    contexts = {
        name: {"runAsUser": container["uid"], "runAsGroup": container["gid"]}
        for name, container in metadata["containers"].items()
    }
    contexts["charm"] = {"runAsUser": 170, "runAsGroup": 170}
    return contexts


def get_pods(model: str, application: str) -> list[dict]:
    """Fetch all application pods using the integration environment's kubectl."""
    result = subprocess.run(
        [
            "/snap/bin/kubectl",
            "get",
            "pods",
            "-n",
            model,
            "-l",
            f"app.kubernetes.io/name={application}",
            "-o=json",
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    pods = json.loads(result.stdout)["items"]
    assert pods, f"No pods found for {application} in {model}"
    return pods


def assert_security_context(pod: dict, name: str, expected: dict[str, int]) -> None:
    """Check effective identities, including any pod-level defaults."""
    container = next(
        container for container in pod["spec"]["containers"] if container["name"] == name
    )
    context = {
        **pod["spec"].get("securityContext", {}),
        **container.get("securityContext", {}),
    }
    for key, value in expected.items():
        assert context.get(key) == value, (name, key, context)


def assert_workload_access(model: str, pod_name: str, container_name: str) -> None:
    """Verify the runtime identity can write to the mounted data directories."""
    subprocess.run(
        [
            "/snap/bin/kubectl",
            "exec",
            "-n",
            model,
            pod_name,
            "-c",
            container_name,
            "--",
            "sh",
            "-ec",
            """
            test "$(id -u)" = 584792
            test "$(id -g)" = 584792
            for path in /app/www /app/www/manpages /app/www/manpages.gz /app/www/sitemaps; do
                work=$(mktemp -d "$path/non-root-test.XXXXXX")
                printf test > "$work/test"
                test "$(cat "$work/test")" = test
                rm -r "$work"
            done
            """,
        ],
        check=True,
        capture_output=True,
        text=True,
        timeout=60,
    )
