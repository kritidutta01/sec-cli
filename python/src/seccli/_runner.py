"""Locating and invoking the sec-cli binary.

This is the only module that touches the filesystem or a subprocess; it is the
seam the tests stub. The wrapper adds no parsing of its own — it runs the binary
with ``--format json`` and hands the bytes to the model decoders.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from typing import List

from .errors import SecCliError, SecCliNotFoundError

#: Environment variable that pins an explicit binary path (highest priority).
BINARY_ENV = "SECCLI_BINARY"

#: The binary's base name on PATH.
BINARY_NAME = "sec-cli"


def _bundled_path() -> str:
    """Path to the binary bundled inside the wheel, if a wheel was built with one."""
    name = BINARY_NAME + (".exe" if os.name == "nt" else "")
    return os.path.join(os.path.dirname(__file__), "_bin", name)


def find_binary() -> str:
    """Locate the sec-cli binary.

    Resolution order: ``$SECCLI_BINARY`` → the binary bundled in the wheel
    (``seccli/_bin/``) → ``PATH``. Raises :class:`SecCliNotFoundError` if none
    resolve.
    """
    override = os.environ.get(BINARY_ENV)
    if override:
        return override

    bundled = _bundled_path()
    if os.path.exists(bundled):
        return bundled

    on_path = shutil.which(BINARY_NAME)
    if on_path:
        return on_path

    raise SecCliNotFoundError(
        "sec-cli binary not found; set %s, install the binary on PATH, or "
        "install a seccli wheel that bundles it" % BINARY_ENV
    )


def run(args: List[str]) -> str:
    """Invoke the binary with ``args`` and return its stdout.

    Raises :class:`SecCliError` (carrying the binary's stderr) on a non-zero exit
    and :class:`SecCliNotFoundError` if the binary cannot be located.
    """
    binary = find_binary()
    try:
        proc = subprocess.run(
            [binary, *args],
            capture_output=True,
            text=True,
        )
    except OSError as exc:  # binary present but not executable, etc.
        raise SecCliError("failed to execute %s: %s" % (binary, exc)) from exc

    if proc.returncode != 0:
        message = proc.stderr.strip() or proc.stdout.strip() or (
            "sec-cli exited with status %d" % proc.returncode
        )
        raise SecCliError(message)
    return proc.stdout
