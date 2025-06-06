# Copyright 2025 Canonical
# See LICENSE file for licensing details.

import subprocess
from pathlib import Path

import jubilant
from pytest import fixture


@fixture(scope="module")
def juju():
    with jubilant.temp_model() as juju:
        yield juju


@fixture(scope="module")
def manpages_charm(request):
    """Manpages charm used for integration testing."""
    charm_file = request.config.getoption("--charm-path")
    if charm_file:
        return charm_file

    subprocess.run(
        ["/snap/bin/charmcraft", "pack", "--verbose"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )

    return next(Path.glob(Path("."), "*.charm")).absolute()
