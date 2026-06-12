#!/usr/bin/env bash
# gen-cast.sh — generate an asciinema v2 cast file by running real commands.
#
# Usage (from repo root on Linux/macOS/WSL):
#   export SEC_CLI_USER_AGENT="Your Name your-email@example.com"
#   bash scripts/gen-cast.sh > demo.cast
#   asciinema upload demo.cast

set -euo pipefail

BIN="${BIN:-./bin/sec-cli-linux}"

# Accumulate cast event lines; flush at the end so header is printed first.
T=0
EVENTS=""

tick() { T=$(python3 -c "print(f'{$T + $1:.3f}')"); }

# Append one asciinema v2 event line. Uses Python to JSON-encode the text
# so control characters are properly represented.
event() {
    local text="$1"
    local enc
    enc=$(python3 -c "import json,sys; print(json.dumps(sys.argv[1]))" "$text")
    EVENTS+="[$T, \"o\", ${enc}]"$'\n'
}

# Show a comment line, simulate typing the command, then run it and stream output.
scene() {
    local comment="$1" cmd_display="$2"; shift 2

    tick 0.5
    event $'\r\n'
    event "# ${comment}"$'\r\n'
    tick 0.5

    event "$ "
    local i
    for ((i = 0; i < ${#cmd_display}; i++)); do
        event "${cmd_display:$i:1}"
        tick 0.045
    done
    event $'\r\n'
    tick 0.7

    local line
    while IFS= read -r line; do
        event "${line}"$'\r\n'
        tick 0.03
    done < <("$@" 2>&1 || true)
    tick 0.6
}

# ── header line (required first line of every cast file) ─────────────────────
python3 - <<'EOF'
import json, time
print(json.dumps({
    "version": 2,
    "width": 100,
    "height": 36,
    "timestamp": int(time.time()),
    "title": "sec-cli — SEC EDGAR to structured data for LLMs",
    "env": {"SHELL": "/bin/bash", "TERM": "xterm-256color"},
}))
EOF

# ── title card ────────────────────────────────────────────────────────────────
tick 0.4
event "sec-cli  |  SEC EDGAR -> structured data for LLMs"$'\r\n'
event "No API key. No paid service. Pure EDGAR."$'\r\n'
event "github.com/kritidutta01/sec-cli"$'\r\n'
tick 1.0

# ── scene 1: version ──────────────────────────────────────────────────────────
scene "confirm the build" \
    "sec-cli version" \
    "$BIN" version

# ── scene 2: detect format ────────────────────────────────────────────────────
scene "check Apple's 2024 10-K format (iXBRL = machine-readable)" \
    "sec-cli detect AAPL --year 2024" \
    "$BIN" detect AAPL --year 2024

# ── scene 3: list sections ───────────────────────────────────────────────────
scene "every Item section in Apple's latest 10-K" \
    "sec-cli sections AAPL" \
    "$BIN" sections AAPL

# ── scene 4: get Risk Factors ────────────────────────────────────────────────
scene "Risk Factors as Markdown — ready to pipe into an LLM" \
    "sec-cli get AAPL --section 1A --output md" \
    "$BIN" get AAPL --section 1A --output md

# ── scene 5: year-over-year diff ─────────────────────────────────────────────
scene "what changed between FY2023 and FY2024?" \
    "sec-cli diff AAPL --from 2023 --to 2024 --output md" \
    "$BIN" diff AAPL --from 2023 --to 2024 --output md

# ── scene 6: accuracy harness ────────────────────────────────────────────────
scene "extraction accuracy harness — hermetic, no network" \
    "sec-cli accuracy internal/accuracy/testdata/corpus" \
    "$BIN" accuracy internal/accuracy/testdata/corpus

tick 0.8
event $'\r\n'
event "go install github.com/kritidutta01/sec-cli/cmd/sec-cli@latest"$'\r\n'
tick 2.0

# ── flush events ──────────────────────────────────────────────────────────────
printf '%s' "$EVENTS"
