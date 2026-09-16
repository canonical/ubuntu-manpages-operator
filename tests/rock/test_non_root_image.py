# Copyright 2026 Canonical
# See LICENSE file for licensing details.

"""Validate a locally built rock with Docker, without a registry push."""

import os
import subprocess

import pytest


@pytest.mark.parametrize("volumes", [[], ["/app/www/manpages", "/app/www/manpages.gz"]])
def test_non_root_image(volumes):
    """Check the default runtime identity and data-directory operations."""
    image = os.environ.get("MANPAGES_TEST_IMAGE", "ubuntu-manpages:non-root")
    subprocess.run(
        [
            "docker",
            "run",
            "--rm",
            "--network=none",
            "--entrypoint=sh",
            *[f"--volume={path}" for path in volumes],
            image,
            "-ec",
            """
            test "$(id -u)" = 584792
            test "$(id -g)" = 584792
            test -x /usr/bin/server
            test -x /usr/bin/ingest
            for path in /app/www /app/www/manpages /app/www/manpages.gz /app/www/sitemaps /tmp; do
                test -r "$path" && test -w "$path" && test -x "$path"
                work=$(mktemp -d "$path/non-root-test.XXXXXX")
                mkdir "$work/release"
                printf test > "$work/release/manpage"
                test "$(cat "$work/release/manpage")" = test
                rm -r "$work"
            done
            printf '.TH TEST 1\\n.SH NAME\\ntest - test manual\\n' | mandoc -T html > /tmp/test.html
            test -s /tmp/test.html
            """,
        ],
        check=True,
        capture_output=True,
        text=True,
        timeout=60,
    )
