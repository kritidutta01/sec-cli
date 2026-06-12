# sec-cli

Extract structured data from SEC EDGAR filings for LLM consumption.

> **Status: v1.0, all 14 phases complete.** `sec-cli get`, `sec-cli diff`, and the full parser pipeline (iXBRL fact stream, presentation linkbase, section extraction, three-layer diff, SQLite cache) are implemented and tested. See [Roadmap](#roadmap).

---

## Demo

![sec-cli demo](demo.gif)

```bash
# Fetch Apple's latest 10-K Risk Factors as clean Markdown
sec-cli get AAPL --section "Risk Factors" --output md | head -40

# Year-over-year diff of Nvidia's 10-K (2023 → 2024)
sec-cli diff NVDA --from 2023 --to 2024 --output md
```

---

## What it does

sec-cli is a fast CLI (and importable Go library) that turns SEC EDGAR filings into clean, structured output you can pipe directly into a language model or analysis pipeline, no API key, no paid service, no runtime dependencies.

- **Fetches** any 10-K, 10-Q, or 8-K by ticker + year with no API key required
- **Detects** filing format (iXBRL, PartialIXBRL, PlainHTML, PlainText) and refuses non-iXBRL filings cleanly with a v1.1 pointer, never silently parses bad data
- **Extracts** financial tables using the iXBRL fact stream + presentation linkbase as the authoritative source, the same data FactSet and Bloomberg read, not HTML table walking
- **Extracts** free-text sections (Risk Factors, MD&A, Business Overview, 8-K items) using distribution-relative heading detection, structure derived from each document's own inline CSS, not fixed font-size thresholds
- **Outputs** versioned, schema-stable JSON or Markdown with a calibrated confidence signal per table (`schema_version: "1.0.0"`)
- **Diffs** two years of the same filing at structural, lexical, or semantic granularity to surface what substantively changed, not every comma rephrasing

**v1.0 supports iXBRL-era filings only** (2019+ for large accelerated filers, 2021+ for all filers). Pre-iXBRL filings are detected and refused with a clear error and a v1.1 pointer, never parsed badly.

---

## Requirements

- **Go 1.22+**
- `SEC_CLI_USER_AGENT` environment variable. EDGAR's Fair Access policy requires every request to identify the caller; without it EDGAR returns HTTP 403.

```bash
# macOS / Linux
export SEC_CLI_USER_AGENT="Your Name your-email@example.com"
```

```powershell
# Windows PowerShell (current session)
$env:SEC_CLI_USER_AGENT = "Your Name your-email@example.com"
# persist for all future terminals:
[Environment]::SetEnvironmentVariable("SEC_CLI_USER_AGENT", "Your Name your-email@example.com", "User")
```

---

## Build & run

```bash
# build binary to ./bin/
go build -o bin/sec-cli ./cmd/sec-cli        # bin/sec-cli.exe on Windows

# or run straight from source
go run ./cmd/sec-cli get AAPL
```

Available make targets:

```
make build       # compile binary to ./bin/
make test        # go test ./...
make accuracy    # run accuracy harness against internal corpus
make lint        # staticcheck + go vet
```

---

## Commands

### `get`, fetch, parse, and render a filing

```bash
sec-cli get AAPL                                   # latest 10-K, JSON
sec-cli get AAPL --year 2023 --output md           # 2023 10-K as Markdown
sec-cli get AAPL --section "Risk Factors"          # single section only
sec-cli get AAPL --section 1A                      # by item id
sec-cli get AAPL --type 10-Q --output text         # latest 10-Q as plain text
sec-cli get AAPL --accession 0000320193-24-000123  # pin to exact filing/amendment
sec-cli get AAPL --no-cache                        # bypass local SQLite cache
```

Flags: `--type/-t` (default `10-K`), `--year`, `--accession`, `--output/-o` (`json`, `md`, `text`), `--section/-s`, `--no-cache`.

`--section` accepts an item ID (`1A`) or a title substring (`risk`). On a miss, exits 1 and lists all available sections. `--accession` overrides `--year` and handles amendments (10-K/A, 10-Q/A).

Every JSON response carries `schema_version: "1.0.0"`. Every Markdown/text response opens with `<!-- sec-cli v0.1.0 schema 1.0.0 -->`.

---

### `diff`, compare two filings year-over-year

```bash
sec-cli diff AAPL --from 2022 --to 2024                   # structural diff, JSON
sec-cli diff AAPL --from 2022 --to 2024 --output md       # Markdown report
sec-cli diff AAPL --from 2022 --to 2024 --layer lexical   # word-level diff
sec-cli diff MSFT --from 2021 --to 2023 --section "1A"    # Risk Factors only
```

Flags: `--type/-t`, `--from`, `--to`, `--layer` (`structural`, `lexical`, `semantic`), `--output/-o` (`json`, `md`).

Three diff layers:

| Layer | What it shows | Status |
|---|---|---|
| `structural` (default) | Subsection-grain: added / removed / modified / unchanged | ✅ |
| `lexical` | Word-level diffs within modified paragraphs, annotated `[+..+]` / `[-..-]` | ✅ |
| `semantic` | Embedding-distance ranking of modified paragraphs, surfaces substantive change, filters year-roll noise | planned v1.0.1 |

Financial tables diff by aligning rows on GAAP concept (`us-gaap:Revenues` matches whether labeled "Net revenues" or "Net sales") and columns by period. Concept-based alignment is free from the iXBRL fact-stream path.

Both endpoints must be iXBRL filings, diffs that span the iXBRL boundary fail fast on the pre-iXBRL side, before the iXBRL filing is fetched.

---

### `sections`, list detected item sections

```bash
sec-cli sections AAPL
sec-cli sections AAPL --year 2023 --type 10-Q
sec-cli sections MSFT --type 8-K
```

Lists all detected Item sections with resolved titles. Useful to know what `--section` accepts before calling `get` or `diff`.

8-K items use the dotted sub-numbering scheme (`Item 1.01`, `Item 5.02`); 10-K/10-Q items use the standard scheme (`Item 1A`, `Item 7`). Both are classified correctly.

---

### `accuracy`, run the extraction harness

```bash
sec-cli accuracy internal/accuracy/testdata/corpus
# or via make:
make accuracy
```

Scores the pipeline against a corpus of hand-verified filings. Reports statement cell accuracy and section coverage per filing, split by confidence bucket. All fixtures are hermetic, no network calls.

Current corpus results (synthetic fixtures):

```
filing     ticker  format         exp   →got      stmt-acc   sect-cov
----------------------------------------------------------------------------
acme       ACME    IXBRL          medium→medium     100.0%     100.0%
globex     GLBX    IXBRL          high  →high       100.0%     100.0%
initech    INTH    IXBRL          medium→medium     100.0%     100.0%
umbrella   UMBR    PartialIXBRL   high  →high       100.0%     100.0%
----------------------------------------------------------------------------
overall: statement accuracy 100.0% (32/32), section coverage 100.0% (13/13)

confidence calibration: high 100.0% · medium 100.0%
```

The current corpus uses synthetic hermetic fixtures. Real-filing corpus expansion (AAPL, MSFT, JPM) is planned for v1.0.1, those numbers will be the honest headline once that corpus is in.

---

### `cache`, inspect or clear the local SQLite cache

```bash
sec-cli cache path          # print the cache file location
sec-cli cache clear         # delete all cached raw bytes and parsed documents
```

Cache lives at `%LOCALAPPDATA%\sec-cli\cache.db` on Windows or `~/.cache/sec-cli/cache.db` on Linux/macOS. Two-tier design:

| Tier | Key | Invalidation |
|---|---|---|
| Raw | `(cik, accession)` | Never, EDGAR filings are immutable; amendments get new accession numbers |
| Parsed | `(accession, parser_version, schema_version)` | Automatic when either version bumps, raw bytes are reused, no re-fetch |

---

### Low-level commands

These pre-`get` commands expose individual pipeline stages and are useful for debugging.

```bash
sec-cli latest AAPL                            # accession of latest 10-K
sec-cli detect AAPL --year 2024                # IXBRL
sec-cli detect AAPL --year 2018                # PlainHTML → refused cleanly in v1.0
sec-cli fetch AAPL --year 2024                 # first 200 bytes of primary document
sec-cli facts AAPL --year 2024 --concept RevenueFromContract
sec-cli table AAPL --year 2024 --statement income
sec-cli table AAPL --year 2024 --layout --index 15   # narrative MD&A table by position
sec-cli text AAPL --year 2024 --section "Risk Factors"
```

---

## Tests

The suite is hermetic, recorded fixtures and a fake `http.RoundTripper` mean no network calls and no `SEC_CLI_USER_AGENT` needed:

```bash
go test ./...                            # all packages
go test ./internal/diff/... -v           # diff + word-level lexical tests
go test ./internal/sections/... -v       # section classification (10-K + 8-K items)
go test ./internal/accuracy/... -v       # accuracy harness against synthetic corpus
go test ./internal/cache/... -v          # cache + schema-version migration
```

> **Windows note:** the suite uses a fake `http.RoundTripper` rather than `httptest.Server`, which hangs on some Windows/Go builds due to a `cancelIO` defect in the standard library's listener teardown. `go test -race` requires a C toolchain (cgo); it runs in CI.

---

## Python wrapper

A thin Python wrapper (`pip install seccli`) drives the binary via subprocess and deserializes canonical JSON into typed dataclasses. The JSON schema is the only contract, the Go and Python views never diverge.

```python
import seccli

doc = seccli.get("AAPL")               # latest 10-K → Document
doc.metadata.company                    # "Apple Inc."
doc.tables[0].rows[0].values           # [391035000000, 383285000000, ...]

changes = seccli.diff("AAPL", frm=2022, to=2024)
for s in changes.sections:
    print(s.item, s.status)            # "1A", "modified"

# search() is planned for v1.1
seccli.search("revenue recognition")  # raises SecCliError (not yet implemented)
```

See [python/README.md](python/README.md) for full installation and API surface.

---

## Distribution

**Go install:**

```bash
go install github.com/kritidutta01/sec-cli/cmd/sec-cli@latest
```

**Homebrew (macOS / Linux):**

```bash
brew tap kritidutta01/sec-cli
brew install sec-cli
```

Cross-compiled binaries for darwin/amd64, darwin/arm64, linux/amd64, and linux/arm64 are built via GoReleaser on GitHub Actions on each tagged release.

---

## Architecture

```
CLI (cmd/)
  └─ edgar client       rate-limited (9 req/sec), User-Agent enforced
      └─ format router  classifies IXBRL / PartialIXBRL / PlainHTML / PlainText / Unknown
          └─ iXBRL parser
              ├─ fact-stream extractor   PRIMARY, financial statements
              │   fact stream + presentation linkbase → rows × columns × periods
              ├─ layout extractor        FALLBACK, untagged narrative tables
              │   5-phase HTML pipeline → rows × columns (lower accuracy ceiling)
              └─ section extractor       10-K/10-Q item patterns + 8-K dotted items
                  └─ normalized model    Document → Metadata, []Section, []Table
                      └─ cache           SQLite two-tier (raw + parsed)
                          └─ renderer    JSON / Markdown / text, schema-stamped
```

Pre-iXBRL paths (PlainHTML, PlainText) are detected and refused in v1.0. The layout extractor exists for narrative tables within iXBRL filings; whole pre-iXBRL support is v1.1.

---

## Roadmap

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
| 8 | Section extraction: 10-K + 8-K item pattern tables | ✅ Done |
| 9 | Normalized model + JSON / Markdown / text output | ✅ Done |
| 10 | SQLite cache (two-tier, schema-version keyed) | ✅ Done |
| 11 | `get` command, fetch → parse → render pipeline | ✅ Done |
| 12 | `diff` command, structural + lexical layers; semantic stub | ✅ Done |
| 13 | Accuracy harness + hermetic corpus | ✅ Done |
| 14 | Python wrapper & Homebrew distribution template | ✅ Done |

**v1.0.1 (next):** `--layer semantic` embedding-distance diff; real-filing accuracy corpus (AAPL, MSFT, JPM).  
**v1.1:** Whole plain-HTML filings (2010–2018 era). The layout extractor built for v1.0 narrative tables gets wired to handle whole pre-iXBRL filings.

---

## License

[MIT](LICENSE)
