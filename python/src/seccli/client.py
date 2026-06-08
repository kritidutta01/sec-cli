"""The seccli public API: drive `sec-cli get`/`diff` and return typed objects."""

from __future__ import annotations

from typing import List, Optional

from . import _runner
from .models import ChangeSet, Document


def get(
    ticker: str,
    *,
    form: str = "10-K",
    year: Optional[int] = None,
    section: Optional[str] = None,
    no_cache: bool = False,
) -> Document:
    """Fetch, parse, and render a filing, returning a :class:`Document`.

    Mirrors ``sec-cli get``. ``year`` selects a filing year (default: latest);
    ``section`` narrows output to one section by item id or title substring.
    Raises :class:`~seccli.errors.SecCliError` if the binary fails.
    """
    args: List[str] = ["get", ticker, "--format", "json", "--type", form]
    if year is not None:
        args += ["--year", str(year)]
    if section is not None:
        args += ["--section", section]
    if no_cache:
        args.append("--no-cache")
    return Document.from_json(_runner.run(args))


def diff(
    ticker: str,
    *,
    frm: int,
    to: int,
    form: str = "10-K",
    no_cache: bool = False,
) -> ChangeSet:
    """Compare two filings, returning a :class:`ChangeSet`.

    Mirrors ``sec-cli diff``. ``frm`` and ``to`` are the filing years to compare.
    Raises :class:`~seccli.errors.SecCliError` if the binary fails.
    """
    args: List[str] = [
        "diff",
        ticker,
        "--from",
        str(frm),
        "--to",
        str(to),
        "--format",
        "json",
        "--type",
        form,
    ]
    if no_cache:
        args.append("--no-cache")
    return ChangeSet.from_json(_runner.run(args))
