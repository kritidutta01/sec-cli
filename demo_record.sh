#!/bin/bash
# Full-feature asciinema recording script
# Usage: asciinema rec demo.cast -c "bash demo_record.sh" --cols 120 --rows 40 --overwrite
# All EDGAR calls use the SQLite cache — run cache-warm.sh once before recording.

BINARY="$(dirname "$(realpath "$0")")/bin/sec-cli-linux"
CORPUS="$(dirname "$(realpath "$0")")/internal/accuracy/testdata/corpus"
export SEC_CLI_USER_AGENT="Kriti Dutta data.kriti.dutta@gmail.com"

G='\033[1;32m'
B='\033[1m'
DIM='\033[2m'
R='\033[0m'

section() {
    echo ""
    printf "${DIM}── $* ──${R}\n"
    sleep 0.4
}

cmd() {
    echo ""
    printf "${G}\$${R} ${B}$*${R}\n"
    sleep 0.8
}

clear
printf "${B}sec-cli${R}  —  SEC EDGAR filings → LLM-ready JSON / Markdown\n"
printf "No API key · No paid service · Single static binary\n"
printf "github.com/kritidutta01/sec-cli\n"
sleep 2.5

# ── 1. Format detection ─────────────────────────────────────────────────────
section "Format detection — iXBRL / PlainHTML / PlainText router"

cmd "sec-cli detect AAPL"
"$BINARY" detect AAPL 2>&1
sleep 1.2

cmd "sec-cli detect AAPL --year 2018"
"$BINARY" detect AAPL --year 2018 2>&1
sleep 1.2

# Refuse cleanly — never silently parse bad data
cmd "sec-cli get AAPL --year 2018"
"$BINARY" get AAPL --year 2018 2>&1
sleep 2

# ── 2. Section listing ──────────────────────────────────────────────────────
section "List all detected sections — 10-K items, 8-K items, confidence signal"

cmd "sec-cli sections AAPL"
"$BINARY" sections AAPL 2>&1
sleep 2.5

# ── 3. Section extraction — Markdown ────────────────────────────────────────
section "Extract a section as clean Markdown — pipe directly into an LLM"

cmd "sec-cli get AAPL --section 'Risk Factors' --output md | head -16"
"$BINARY" get AAPL --section "Risk Factors" --output md 2>/dev/null | head -16
sleep 2

# ── 4. Schema-stable JSON ────────────────────────────────────────────────────
section "Schema-stable JSON — schema_version field, metadata, sections, tables"

cmd "sec-cli get AAPL --output json | head -20"
"$BINARY" get AAPL --output json 2>/dev/null | head -20
sleep 2

# ── 5. Year-over-year diff — structural ─────────────────────────────────────
section "Year-over-year diff — rows aligned by GAAP concept, not string match"

cmd "sec-cli diff NVDA --from 2023 --to 2024 --output md | head -18"
"$BINARY" diff NVDA --from 2023 --to 2024 --output md 2>/dev/null | head -18
sleep 2

# ── 6. Lexical diff ──────────────────────────────────────────────────────────
section "Lexical layer — word-level diff, year-roll noise filtered"

cmd "sec-cli diff NVDA --from 2023 --to 2024 --layer lexical --output md | head -14"
"$BINARY" diff NVDA --from 2023 --to 2024 --layer lexical --output md 2>/dev/null | head -14
sleep 2.5

# ── 7. Raw XBRL facts ────────────────────────────────────────────────────────
section "Raw XBRL fact stream — same data path as FactSet and Bloomberg"

cmd "sec-cli facts AAPL --year 2025 --concept RevenueFromContract | head -6"
"$BINARY" facts AAPL --year 2025 --concept RevenueFromContract 2>/dev/null | head -6
sleep 2

# ── 8. Accuracy harness ──────────────────────────────────────────────────────
section "Accuracy harness — 7 filings, 76 hand-verified cells, 0 misses"

cmd "sec-cli accuracy internal/accuracy/testdata/corpus"
"$BINARY" accuracy "$CORPUS"
sleep 2.5

# ── 9. Cache ─────────────────────────────────────────────────────────────────
section "SQLite cache — raw tier (immutable) + parsed tier (schema-version keyed)"

cmd "sec-cli cache path"
"$BINARY" cache path 2>&1
sleep 2

# ── CTA ───────────────────────────────────────────────────────────────────────
echo ""
printf "${G}go install github.com/kritidutta01/sec-cli/cmd/sec-cli@latest${R}\n"
printf "${DIM}github.com/kritidutta01/sec-cli${R}\n"
sleep 2
