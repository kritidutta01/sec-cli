"""Typed views over sec-cli's canonical JSON output.

These dataclasses mirror the Go ``internal/model`` schema (and the
``internal/diff`` change-set schema) field for field. They add **no** parsing
logic beyond JSON decoding — every ``from_dict`` is a straight key lookup — so
the Go output schema stays the single source of truth and the two views can
never drift. See ../../../docs/schema.md for the field reference.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


def _opt_float_list(values: Any) -> List[Optional[float]]:
    """Decode a JSON values array, preserving null as None (never 0)."""
    if not values:
        return []
    return [None if v is None else float(v) for v in values]


@dataclass
class Source:
    extractor: str = ""
    parser_version: str = ""

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Source":
        return cls(d.get("extractor", ""), d.get("parser_version", ""))


@dataclass
class Confidence:
    level: str = ""
    row_match_rate: float = 0.0
    cell_resolved_rate: float = 0.0
    untagged_cell_count: int = 0

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Confidence":
        return cls(
            d.get("level", ""),
            d.get("row_match_rate", 0.0),
            d.get("cell_resolved_rate", 0.0),
            d.get("untagged_cell_count", 0),
        )


@dataclass
class Column:
    label: str = ""
    context_ref: str = ""
    period_start: Optional[str] = None
    period_end: Optional[str] = None
    instant: bool = False

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Column":
        return cls(
            d.get("label", ""),
            d.get("context_ref", ""),
            d.get("period_start"),
            d.get("period_end"),
            d.get("instant", False),
        )


@dataclass
class Row:
    label: str = ""
    concept: str = ""
    type: str = ""
    depth: int = 0
    values: List[Optional[float]] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Row":
        return cls(
            d.get("label", ""),
            d.get("concept", ""),
            d.get("type", ""),
            d.get("depth", 0),
            _opt_float_list(d.get("values")),
        )


@dataclass
class Table:
    schema_version: str = ""
    title: str = ""
    role_uri: str = ""
    columns: List[Column] = field(default_factory=list)
    rows: List[Row] = field(default_factory=list)
    footnotes: Dict[str, str] = field(default_factory=dict)
    confidence: Confidence = field(default_factory=Confidence)
    source: Source = field(default_factory=Source)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Table":
        return cls(
            d.get("schema_version", ""),
            d.get("title", ""),
            d.get("role_uri", ""),
            [Column.from_dict(c) for c in d.get("columns") or []],
            [Row.from_dict(r) for r in d.get("rows") or []],
            d.get("footnotes") or {},
            Confidence.from_dict(d.get("confidence") or {}),
            Source.from_dict(d.get("source") or {}),
        )


@dataclass
class Section:
    item: str = ""
    title: str = ""
    kind: str = ""
    text: str = ""
    tables: List[Table] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Section":
        return cls(
            d.get("item", ""),
            d.get("title", ""),
            d.get("kind", ""),
            d.get("text", ""),
            [Table.from_dict(t) for t in d.get("tables") or []],
        )


@dataclass
class Metadata:
    company: str = ""
    cik: int = 0
    ticker: str = ""
    form: str = ""
    accession: str = ""
    filing_date: str = ""
    period_start: str = ""
    period_end: str = ""
    schema_version: str = ""
    parser_version: str = ""
    source: Source = field(default_factory=Source)
    confidence: Confidence = field(default_factory=Confidence)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Metadata":
        return cls(
            d.get("company", ""),
            d.get("cik", 0),
            d.get("ticker", ""),
            d.get("form", ""),
            d.get("accession", ""),
            d.get("filing_date", ""),
            d.get("period_start", ""),
            d.get("period_end", ""),
            d.get("schema_version", ""),
            d.get("parser_version", ""),
            Source.from_dict(d.get("source") or {}),
            Confidence.from_dict(d.get("confidence") or {}),
        )


@dataclass
class Document:
    metadata: Metadata = field(default_factory=Metadata)
    sections: List[Section] = field(default_factory=list)
    statements: List[Table] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Document":
        return cls(
            Metadata.from_dict(d.get("metadata") or {}),
            [Section.from_dict(s) for s in d.get("sections") or []],
            [Table.from_dict(t) for t in d.get("statements") or []],
        )

    @classmethod
    def from_json(cls, text: str) -> "Document":
        return cls.from_dict(json.loads(text))


# --- diff change set -------------------------------------------------------


@dataclass
class FilingRef:
    company: str = ""
    ticker: str = ""
    form: str = ""
    accession: str = ""
    filing_date: str = ""
    period_end: str = ""

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "FilingRef":
        return cls(
            d.get("company", ""),
            d.get("ticker", ""),
            d.get("form", ""),
            d.get("accession", ""),
            d.get("filing_date", ""),
            d.get("period_end", ""),
        )


@dataclass
class CellDelta:
    period: str = ""
    prev: Optional[float] = None
    curr: Optional[float] = None
    abs: Optional[float] = None
    pct: Optional[float] = None

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "CellDelta":
        return cls(d.get("period", ""), d.get("prev"), d.get("curr"), d.get("abs"), d.get("pct"))


@dataclass
class RowDelta:
    concept: str = ""
    label: str = ""
    status: str = ""
    cells: List[CellDelta] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "RowDelta":
        return cls(
            d.get("concept", ""),
            d.get("label", ""),
            d.get("status", ""),
            [CellDelta.from_dict(c) for c in d.get("cells") or []],
        )


@dataclass
class StatementDiff:
    title: str = ""
    role_uri: str = ""
    periods: List[str] = field(default_factory=list)
    rows: List[RowDelta] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "StatementDiff":
        return cls(
            d.get("title", ""),
            d.get("role_uri", ""),
            list(d.get("periods") or []),
            [RowDelta.from_dict(r) for r in d.get("rows") or []],
        )


@dataclass
class ParagraphChange:
    kind: str = ""
    text: str = ""

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "ParagraphChange":
        return cls(d.get("kind", ""), d.get("text", ""))


@dataclass
class SectionDiff:
    item: str = ""
    title: str = ""
    paragraphs: List[ParagraphChange] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "SectionDiff":
        return cls(
            d.get("item", ""),
            d.get("title", ""),
            [ParagraphChange.from_dict(p) for p in d.get("paragraphs") or []],
        )


@dataclass
class ChangeSet:
    schema_version: str = ""
    prev: FilingRef = field(default_factory=FilingRef)
    curr: FilingRef = field(default_factory=FilingRef)
    statements: List[StatementDiff] = field(default_factory=list)
    sections: List[SectionDiff] = field(default_factory=list)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "ChangeSet":
        return cls(
            d.get("schema_version", ""),
            FilingRef.from_dict(d.get("prev") or {}),
            FilingRef.from_dict(d.get("curr") or {}),
            [StatementDiff.from_dict(s) for s in d.get("statements") or []],
            [SectionDiff.from_dict(s) for s in d.get("sections") or []],
        )

    @classmethod
    def from_json(cls, text: str) -> "ChangeSet":
        return cls.from_dict(json.loads(text))
