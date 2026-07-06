# sec-cli output schema

**schemaVersion `1.0.0`**

This is the field-by-field contract for the canonical JSON that `sec-cli`
emits (`--format json`). It is the artifact external consumers and the Python
wrapper (Phase 14) code against. The Go source of truth is
[`internal/model`](../internal/model); this document mirrors it.

Stability follows semver on `schemaVersion`: **additive** fields are a minor
bump; a **rename or removal** is a major bump. `schemaVersion` is the
consumer-facing output contract and is independent of `parserVersion`, which
identifies the extraction code (the cache keys parsed output on it).

A missing value is always JSON `null` — never `0` or `""`. Dates are plain
`YYYY-MM-DD` strings, not RFC3339 timestamps.

## Worked example

The example below shows how to read a small part of an AAPL FY2024 JSON response.
It uses values that are already hand-verified in
`internal/accuracy/testdata/corpus/aapl/baseline.json`.

```json
{
  "metadata": {
    "ticker": "AAPL",
    "form": "10-K",
    "schema_version": "1.0.0"
  },
  "statements": [
    {
      "title": "CONSOLIDATED STATEMENTS OF OPERATIONS",
      "columns": [
        { "label": "2024", "period_end": "2024-09-28" },
        { "label": "2023", "period_end": "2023-09-30" }
      ],
      "rows": [
        {
          "label": "Net sales",
          "concept": "us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax",
          "type": "data",
          "values": [391035000000, 383285000000]
        },
        {
          "label": "Net income",
          "concept": "us-gaap:NetIncomeLoss",
          "type": "total",
          "values": [93736000000, 96995000000]
        }
      ]
    }
  ]
}
```

Read the row values by position: the first value belongs to the first column
(`2024`), and the second value belongs to the second column (`2023`). Consumers
that compare filings should prefer the stable `concept` field over the display
`label`, because labels can vary between companies and years.

## Document

The top-level object: one normalized filing.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `metadata` | [Metadata](#metadata) | no | Filing identity, period, and stamps. |
| `sections` | [Section](#section)[] | no (may be `[]`) | The filing partitioned into named items, in document order. |
| `statements` | [Table](#table)[] | no (may be `[]`) | The projected financial statements. |

## Metadata

Identifies the filing and stamps the output.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `company` | string | omitted if empty | Registrant name. |
| `cik` | integer | omitted if 0 | SEC Central Index Key. |
| `ticker` | string | omitted if empty | Ticker symbol, as requested. |
| `form` | string | omitted if empty | Form type, e.g. `10-K`. |
| `accession` | string | omitted if empty | EDGAR accession number. |
| `filing_date` | string (date) | omitted if empty | Date the filing was filed. |
| `period_start` | string (date) | omitted if empty | Start of the period the filing reports. |
| `period_end` | string (date) | omitted if empty | End of the reported period (fiscal year/quarter end). |
| `schema_version` | string | no | Output contract version (this document's version). |
| `parser_version` | string | no | Extraction code identity. |
| `source` | [Source](#source) | no | Which extractor produced the document. |
| `confidence` | [Confidence](#confidence) | no | Document-level confidence (combining statement and section confidences). |

## Section

One named part of the filing.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `item` | string | no | 10-K item identifier, e.g. `1A`. |
| `title` | string | no | Canonical item title, e.g. `Risk Factors`. |
| `kind` | string | no | `narrative` or `financial`. |
| `text` | string | omitted if empty | Rendered free text, iXBRL scaffolding removed. |
| `tables` | [Table](#table)[] | omitted if empty | Tables that fall inside the section. |

## Table

A statement or narrative table projected onto rows × columns.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `schema_version` | string | no | Schema version stamped at extraction. |
| `title` | string | omitted if empty | Statement/table title. |
| `role_uri` | string | omitted if empty | Source presentation-linkbase role URI. |
| `columns` | [Column](#column)[] | no | One entry per reporting period. |
| `rows` | [Row](#row)[] | no | One entry per line item. |
| `footnotes` | object (string→string) | omitted if empty | Footnote marker → text. |
| `confidence` | [Confidence](#confidence) | no | Table confidence. |
| `source` | [Source](#source) | no | Which extractor produced the table. |

## Column

One reporting period.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `label` | string | no | Column header, e.g. the period-end year. |
| `context_ref` | string | omitted if empty | iXBRL context id backing the column. |
| `period_start` | string (date) | omitted if empty | Period start (absent for layout tables). |
| `period_end` | string (date) | omitted if empty | Period end. |
| `instant` | bool | omitted if false | True for a point-in-time (balance-sheet) column. |

## Row

One statement line.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `label` | string | no | Display label from the filing. |
| `concept` | string | omitted if empty | Taxonomy concept (stable across filings; matched on by the differ). |
| `type` | string | no | `data`, `subtotal`, or `total`. |
| `depth` | integer | omitted if 0 | Presentation nesting depth. |
| `values` | (number \| null)[] | values may be `null` | One value per column; `null` is a cell with no matching fact (never `0`). |

## Confidence

The calibrated trust signal for an artifact.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `level` | string | no | `high`, `medium`, or `low`. |
| `row_match_rate` | number | no | Fraction of rows with every column filled (0 for non-table partitions). |
| `cell_resolved_rate` | number | no | Fraction of cells filled by a fact. |
| `untagged_cell_count` | integer | no | Count of cells with no resolved value. |

## Source

Provenance of an artifact.

| Field | Type | Null? | Meaning |
|------|------|:----:|---------|
| `extractor` | string | no | `ixbrl.facts` (fact-stream projection) or `ixbrl.layout` (spatial fallback). |
| `parser_version` | string | no | Extraction code identity. |
