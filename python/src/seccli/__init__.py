"""seccli — a thin Python wrapper over the sec-cli binary.

It locates the binary, invokes ``get``/``diff`` with ``--format json``, and
deserializes the canonical JSON into typed dataclasses. The Phase 9 JSON schema
is the only contract; the wrapper adds no parsing logic of its own, so the Go and
Python views never diverge.

    >>> import seccli
    >>> doc = seccli.get("AAPL")            # latest 10-K
    >>> doc.metadata.company
    >>> changes = seccli.diff("AAPL", frm=2023, to=2024)
"""

from __future__ import annotations

from .client import diff, get
from .errors import SecCliError, SecCliNotFoundError
from .models import (
    CellDelta,
    ChangeSet,
    Column,
    Confidence,
    Document,
    FilingRef,
    Metadata,
    ParagraphChange,
    Row,
    RowDelta,
    Section,
    SectionDiff,
    Source,
    StatementDiff,
    Table,
)

__version__ = "0.1.0"

__all__ = [
    "get",
    "diff",
    "SecCliError",
    "SecCliNotFoundError",
    "Document",
    "Metadata",
    "Section",
    "Table",
    "Column",
    "Row",
    "Confidence",
    "Source",
    "ChangeSet",
    "FilingRef",
    "StatementDiff",
    "RowDelta",
    "CellDelta",
    "SectionDiff",
    "ParagraphChange",
    "__version__",
]
