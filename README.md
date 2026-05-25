# sec-cli

> Fast CLI for SEC filings, built for LLM workflows.

<!-- Hero asset goes here Week 3: asciinema of `sec-cli get AAPL | jq '.sections["Risk Factors"]'` -->

[Design doc](DESIGN.md) · [Blog post](https://kritidutta.dev/sec-cli) · [Tweet thread](https://x.com/kritidutta01)

---

## What this is

sec-cli fetches, parses, and diffs SEC EDGAR filings from the command line. It outputs clean JSON or Markdown — pipe directly into any LLM. No API key. No account. No rate-limit headaches beyond EDGAR's public limits.

It exists because EDGAR's HTML is from 2003 and every existing tool either loses table structure entirely or costs money for someone else's parsing logic. The target consumer of sec-cli's output is a language model, not a human reading a terminal.

## Quickstart

```bash
go install github.com/kritidutta01/sec-cli@latest

# Fetch Apple's latest 10-K as Markdown
sec-cli get AAPL --output md

# Extract just the Risk Factors section as JSON
sec-cli get AAPL --section "Risk Factors" --output json

# Diff two years of MD&A
sec-cli diff AAPL --from 2022 --to 2024 --section "MD&A"
```

Or with Python:

```python
pip install sec-cli

from sec_cli import get_filing, diff_filings
filing = get_filing("AAPL", form="10-K", output="json")
```

## Status

**Week 1 of 13 — early development.**

- [x] EDGAR HTTP client (ticker → CIK → filing → HTML)
- [ ] Section parser
- [ ] Table extractor (the hard part — see [DESIGN.md](DESIGN.md))
- [ ] JSON / Markdown output
- [ ] `sec-cli diff`
- [ ] SQLite cache
- [ ] Held-out table accuracy report (target: >90%)

## Why this exists

The eval/observability layer for finance AI is underbuilt. When you try to feed a 10-K into a language model today, you get one of:

1. Raw HTML tag soup (unusable)
2. Extracted text with no table structure (loses all the numbers)
3. Paid API output you can't inspect or run locally

sec-cli is the missing piece: a fast, local, auditable parser that produces LLM-ready output with financial tables intact. The table parser is the technical moat — see [DESIGN.md § The Table Parser](DESIGN.md#the-table-parser-the-hard-part) for the approach.

## How it works

```
ticker → EDGAR CIK lookup → submissions API → filing HTML → section split → table extraction → JSON/MD
```

Full architecture in [DESIGN.md](DESIGN.md).

## What's in v1.0 (and what's not)

**In scope:**
- `sec-cli get` with section extraction and JSON/Markdown output
- `sec-cli diff` for text and table diffs between periods
- Table parser validated against a 30-table held-out test set
- SQLite cache (no re-fetching already-seen filings)
- `go install` + `brew tap` + `pip install sec-cli`

**Deliberately out of scope (v1.0):**
- XBRL/iXBRL parsing
- Bulk screening across companies
- Windows-native binary (macOS + Linux; Windows via WSL)

## Roadmap

- **v1.1** — 8-K event classification, streaming output for large filings
- **v1.2** — Python-native rewrite of table parser for Jupyter integration
- **v1.3** — Embeddings index over a company's full filing history

## Topics

`sec-edgar` `cli` `finance` `llm-tools` `go` `financial-data` `10-k` `edgar`

---

Built by [Kriti Dutta](https://kritidutta.dev) — engineer at BlackRock, building open infrastructure for financial AI.  
[Twitter](https://x.com/kritidutta01) · [LinkedIn](https://linkedin.com/in/kritidutta) · [Email](mailto:kriti@kritidutta.dev)
