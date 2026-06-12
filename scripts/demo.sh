#!/usr/bin/env bash
# demo.sh — automated demo script for sec-cli asciinema recording.
#
# Usage (from repo root on Linux/macOS/WSL):
#   export SEC_CLI_USER_AGENT="Your Name your-email@example.com"
#   asciinema rec demo.cast --command "bash scripts/demo.sh"
#
# The script simulates typed commands with a brief pause before each one so the
# recording reads like a live session. All commands run against live EDGAR.

set -euo pipefail

BOLD='\033[1m'
DIM='\033[2m'
CYAN='\033[0;36m'
GREEN='\033[0;32m'
RESET='\033[0m'

BIN="${BIN:-./bin/sec-cli}"

# Print a simulated prompt + command, pause, then run it.
run() {
    local label="$1"; shift
    echo
    printf "${DIM}# %s${RESET}\n" "$label"
    sleep 0.4
    printf "${CYAN}\$${RESET} ${BOLD}%s${RESET}\n" "$*"
    sleep 0.8
    "$@"
}

# Header
clear
printf "${GREEN}┌─────────────────────────────────────────────────────────────────┐${RESET}\n"
printf "${GREEN}│  sec-cli  ·  SEC EDGAR → structured data for LLMs              │${RESET}\n"
printf "${GREEN}└─────────────────────────────────────────────────────────────────┘${RESET}\n"
sleep 1.2

# ── 1. version ──────────────────────────────────────────────────────────────
run "confirm the build" "$BIN" version
sleep 1

# ── 2. list sections ────────────────────────────────────────────────────────
run "see every Item section in Apple's latest 10-K" "$BIN" sections AAPL
sleep 1.5

# ── 3. detect format ────────────────────────────────────────────────────────
run "detect filing format (iXBRL = structured data available)" \
    "$BIN" detect AAPL --year 2024
sleep 0.8

# ── 4. get: full latest 10-K as Markdown ────────────────────────────────────
run "fetch Apple's latest 10-K — Risk Factors section only" \
    "$BIN" get AAPL --section "1A" --output md
sleep 1.5

# ── 5. diff: year-over-year structural diff ──────────────────────────────────
run "year-over-year diff: what changed between FY2023 and FY2024?" \
    "$BIN" diff AAPL --from 2023 --to 2024 --output md
sleep 1.5

# ── 6. accuracy harness ─────────────────────────────────────────────────────
run "run the extraction accuracy harness" \
    "$BIN" accuracy internal/accuracy/testdata/corpus
sleep 1

# Footer
echo
printf "${GREEN}────────────────────────────────────────────────────────────────────${RESET}\n"
printf "${BOLD}sec-cli v%s  ·  github.com/kritidutta01/sec-cli${RESET}\n" \
    "$("$BIN" version 2>/dev/null)"
printf "${DIM}No API key. No paid service. Pure EDGAR.${RESET}\n"
sleep 2
