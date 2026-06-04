# sec-cli

Extract structured data from SEC EDGAR filings for LLM consumption.

> **Status: early development — Phase 7 of 14.** EDGAR access, filing discovery,
> format detection, the inline-XBRL **fact stream**, **presentation-linkbase
> table projection**, and a **layout fallback for narrative tables** all work and
> are tested against real filings. sec-cli can render a filing's income statement,
> balance sheet, and cash flow as clean rows × periods, and pull untagged MD&A
> tables out of the surrounding HTML. Still ahead: section and free-text
> extraction, JSON/Markdown output, and year-over-year diffs. See the
> [Roadmap](#roadmap).

---

## What it does today

sec-cli can now find a filing, fetch it, classify its format, pull out the raw
inline-XBRL fact stream, and project that stream onto the rows and columns of a
financial statement using the filing's own presentation linkbase.

- **EDGAR-compliant HTTP client** — sends the SEC-required `User-Agent` and
  self-throttles below EDGAR's 10 req/sec cap, with a one-shot retry on 5xx.
- **Ticker → CIK lookup** — `AAPL` resolves to Apple's SEC ID; the lookup table
  is fetched once and cached for the process lifetime.
- **Filing discovery** — list a company's filings, filter by form type
  (`10-K`, `10-Q`, `8-K`), sorted newest-first; resolve the latest filing or the
  one filed in a given year.
- **Primary-document fetch** — pull the raw bytes of a filing's main document.
- **Format router** — classify a filing as `IXBRL`, `PartialIXBRL`, `PlainHTML`,
  `PlainText`, or `Unknown`, resolving filing-index pages to the real document.
  v1.0 parses the iXBRL family and refuses the rest cleanly.
- **iXBRL fact stream** — parse `xbrli:context` periods/segments and extract
  every `ix:nonFraction` / `ix:nonNumeric` fact, with scale, sign, and
  dash-as-zero handled. Verified against Apple's reported FY2024 figures.
- **Table projection** — parse the presentation (`*_pre.xml`) and label
  (`*_lab.xml`) linkbases and project the fact stream onto a statement's rows
  (concepts in linkbase order, with the filing's own labels) × columns (periods,
  newest first). Totals are classified from the `totalLabel` role; cash-flow
  opening/closing balances are placed from period-boundary instants; empty cells
  are `null`, never `0`. Each table carries a calibrated `confidence` signal.
  AAPL's FY2024 income statement, balance sheet, and cash flow all project at
  `high` confidence, matching the filed figures.
- **Layout fallback for narrative tables** — extract tables that aren't
  fact-tagged (MD&A, exhibits) straight from the HTML: read each row's label and
  values by sequence, skipping the spacer `$`/`%` columns EDGAR splits cells
  into, strip footnote markers into a `footnotes` map, normalize parentheses to
  negatives and em-dashes to `null`. Tagged `source.extractor: "ixbrl.layout"`
  and capped at `medium` confidence — the number-to-label binding is
  reconstructed from position, not read from the source.

Commands: `latest`, `fetch`, `detect`, `facts`, `table` (plus `version`).

What it **cannot** do yet: split a filing into named sections, reconstruct free
text, or emit a normalized JSON/Markdown document. The `table` command prints one
table as JSON — a statement, or (with `--layout`) a narrative table by position;
section-name targeting and the schema-stable output model are later phases.

---

## Requirements

- **Go 1.22+**
- A `SEC_CLI_USER_AGENT` environment variable. SEC's Fair Access policy requires
  every request to identify the caller with a real contact; without it EDGAR
  returns HTTP 403, so sec-cli refuses to run until it is set.

```bash
# macOS / Linux
export SEC_CLI_USER_AGENT="Your Name your-email@example.com"
```
```powershell
# Windows PowerShell (current session)
$env:SEC_CLI_USER_AGENT = "Your Name your-email@example.com"
# …or persist it for all future terminals:
[Environment]::SetEnvironmentVariable("SEC_CLI_USER_AGENT", "Your Name your-email@example.com", "User")
```

---

## Build & run

From the repository root:

```bash
# build the binary into ./bin
go build -o bin/sec-cli ./cmd/sec-cli        # bin/sec-cli.exe on Windows

# or run straight from source
go run ./cmd/sec-cli latest AAPL
```

### Usage

```text
$ sec-cli latest AAPL                       # accession of latest 10-K
0000320193-25-000079

$ sec-cli detect AAPL --year 2024           # classify the filing's format
IXBRL

$ sec-cli detect AAPL --year 2018           # pre-iXBRL filings are detected…
PlainHTML

$ sec-cli fetch AAPL --year 2024            # first 200 bytes of the document
<?xml version='1.0' encoding='ASCII'?>...

$ sec-cli facts AAPL --year 2024 --concept RevenueFromContract
us-gaap:RevenueFromContract...  2023-10-01..2024-09-28  391035000000  usd
...

$ sec-cli table AAPL --year 2024 --statement income   # project a statement as JSON
{
  "title": "CONSOLIDATED STATEMENTS OF OPERATIONS",
  "columns": [{"label": "2024", ...}, {"label": "2023", ...}, {"label": "2022", ...}],
  "rows": [
    {"label": "Net sales", "concept": "us-gaap:RevenueFromContract...",
     "type": "data", "values": [391035000000, 383285000000, 394328000000]},
    ...
  ],
  "confidence": {"level": "high", "untagged_cell_count": 0, ...}
}

$ sec-cli table AAPL --year 2024 --layout --index 15   # a narrative MD&A table
{
  "columns": [{"label": "2024"}, {"label": "Change"}, {"label": "2023"}, ...],
  "rows": [
    {"label": "iPhone", "type": "data", "values": [201183, null, 200583, -2, 205489]},
    ...
    {"label": "Total net sales", "type": "total", "values": [391035, 2, 383285, ...]}
  ],
  "footnotes": {"1": ""},
  "source": {"extractor": "ixbrl.layout", "parser_version": "0.1.0"}
}

$ sec-cli latest NOPE                       # unknown ticker → clear error, exit 1
sec-cli: edgar: unknown ticker "NOPE"
```

Common flags: `-t, --type` selects the form type (default `10-K`); `--year`
targets a filing year (default: latest); `--concept` filters `facts` to concepts
containing a string; `--statement` selects which statement `table` projects
(`income`, `balance`, `cashflow`, `equity`, `comprehensive`); `--layout --index N`
extracts the N-th narrative table via the layout fallback. Run
`sec-cli <command> --help` for details.

---

## Tests

The test suite is hermetic — it uses recorded fixtures and never touches the
live SEC API, so no network or `SEC_CLI_USER_AGENT` is needed:

```bash
go test ./...                          # all packages
go test ./internal/edgar/ -v -cover    # verbose, with coverage (~85%)
```

Notes for contributors on Windows: the suite uses a fake `http.RoundTripper`
rather than `httptest.Server`, which hangs on some Windows/Go builds due to a
`cancelIO` defect in the standard library's listener teardown. `go test -race`
requires a C toolchain (cgo) and runs in CI.

---

## Roadmap

sec-cli v1.0 targets **iXBRL-era filings** (modern 10-K/10-Q/8-K). Pre-iXBRL
filings are detected and refused cleanly rather than parsed badly. The
phase-by-phase build progresses through the table below.

| Phase | Scope | Status |
|------:|-------|--------|
| 0 | Scaffold (module, CI, Makefile) | ✅ Done |
| 1 | EDGAR HTTP client (User-Agent, rate limit) | ✅ Done |
| 2 | Submissions metadata & CIK lookup | ✅ Done |
| 3 | Primary-document fetch | ✅ Done |
| 4 | Format router (iXBRL / HTML / text detection) | ✅ Done |
| 5 | iXBRL parser: fact stream & contexts | ✅ Done |
| 6 | Presentation linkbase & table projection | ✅ Done |
| 7 | Layout fallback for narrative tables | ✅ Done |
| 8 | iXBRL parser: sections & free text | ⏳ Next |
| 9 | Normalized model + JSON / Markdown / text output | Planned |
| 10 | SQLite cache | Planned |
| 11 | `get` command (fetch → parse → render) | Planned |
| 12 | `diff` command (year-over-year change) | Planned |
| 13 | Accuracy harness + baselines | Planned |
| 14 | Python wrapper & distribution | Planned |

---

## License

TBD.
