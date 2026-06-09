"""Exceptions raised by the seccli wrapper."""

from __future__ import annotations


class SecCliError(Exception):
    """A sec-cli invocation failed.

    Raised when the binary exits non-zero; the message carries the binary's
    ``sec-cli: <err>`` stderr line so the Go-side failure surfaces unchanged.
    """


class SecCliNotFoundError(SecCliError):
    """The sec-cli binary could not be located.

    Checked locations, in order: the ``SECCLI_BINARY`` environment variable, the
    binary bundled in the wheel (``seccli/_bin/``), and ``PATH``.
    """
