# sec-cli

Extract structured data from SEC EDGAR filings for LLM consumption.

> **Status: early development — Phase 2 of 14.** The foundation (EDGAR access +
> filing discovery) works and is tested against the live SEC API. The headline
> features — parsing filings into clean tables and text, JSON/Markdown output,
> and year-over-year diffs — are not built yet. See the [Roadmap](#roadmap).

---

## What it does today

Right now sec-cli is the "find and fetch" layer: it reliably locates the right
filing for any US public company without getting rate-limited or blocked.

- **EDGAR-compliant HTTP client** — sends the SEC-required `User-Agent` and
  self-throttles below EDGAR's 10 req/sec cap, with a one-shot retry on 5xx.
- **Ticker → CIK lookup** — `AAPL` resolves to Apple's SEC ID; the lookup table
  is fetched once and cached for the process lifetime.
- **Filing discovery** — list a company's filings, filter by form type
  (`10-K`, `10-Q`, `8-K`), sorted newest-first; resolve the latest filing or the
  one filed in a given year.
- **One command so far:** `sec-cli latest <ticker>` prints the accession number
  of the most recent filing of a form type.

What it **cannot** do yet: open a filing and read its contents. Today it returns
a filing's *identifier* (accession number), not the financial tables, risk
factors, or any extracted data. That work begins at Phase 4.

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
$ sec-cli latest AAPL              # latest annual report (10-K)
0000320193-25-000079

$ sec-cli latest AAPL -t 10-Q      # latest quarterly report
0000320193-26-000013

$ sec-cli latest MSFT              # works for any US public company
0000950170-25-100235

$ sec-cli latest NOPE              # unknown ticker → clear error, exit 1
sec-cli: edgar: unknown ticker "NOPE"

$ sec-cli version
0.0.0-dev
```

Flags: `-t, --type` selects the form type (default `10-K`). Run
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
| 3 | Primary-document fetch | ⏳ Next |
| 4 | Format router (iXBRL / HTML / text detection) | Planned |
| 5–8 | iXBRL parser: facts, tables, sections, free text | Planned |
| 9 | Normalized model + JSON / Markdown / text output | Planned |
| 10 | SQLite cache | Planned |
| 11 | `get` command (fetch → parse → render) | Planned |
| 12 | `diff` command (year-over-year change) | Planned |
| 13 | Accuracy harness + baselines | Planned |
| 14 | Python wrapper & distribution | Planned |

---

## License

TBD.
