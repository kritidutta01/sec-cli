#!/usr/bin/env bash
# gen-cast-short.sh — compact 3-scene demo (< 30 seconds, small GIF).
#
# Usage:
#   export SEC_CLI_USER_AGENT="Your Name your-email@example.com"
#   bash scripts/gen-cast-short.sh > demo-short.cast
#   /tmp/agg --speed 1.2 --cols 90 --rows 28 demo-short.cast demo.gif

set -euo pipefail
BIN="${BIN:-./bin/sec-cli-linux}"
T=0; EVENTS=""

tick() { T=$(python3 -c "print(f'{$T + $1:.3f}')"); }
event() {
    local enc
    enc=$(python3 -c "import json,sys; print(json.dumps(sys.argv[1]))" "$1")
    EVENTS+="[$T, \"o\", ${enc}]"$'\n'
}
scene() {
    local comment="$1" cmd_display="$2"; shift 2
    tick 0.4; event $'\r\n'; event "# ${comment}"$'\r\n'; tick 0.4
    event "$ "
    local i; for ((i=0;i<${#cmd_display};i++)); do event "${cmd_display:$i:1}"; tick 0.05; done
    event $'\r\n'; tick 0.6
    local line
    while IFS= read -r line; do event "${line}"$'\r\n'; tick 0.025; done < <("$@" 2>&1 || true)
    tick 0.5
}

python3 - <<'EOF'
import json,time
print(json.dumps({"version":2,"width":90,"height":28,"timestamp":int(time.time()),
    "title":"sec-cli demo","env":{"SHELL":"/bin/bash","TERM":"xterm-256color"}}))
EOF

tick 0.3
event "sec-cli  —  SEC EDGAR to structured data for LLMs"$'\r\n'
event "No API key. No paid service. Pure EDGAR."$'\r\n'
tick 0.8

# scene 1: list sections
scene "every Item section in Apple's latest 10-K" \
    "sec-cli sections AAPL" \
    "$BIN" sections AAPL

# scene 2: fetch Risk Factors (first 25 lines only)
scene "Risk Factors as Markdown — pipe directly into an LLM" \
    "sec-cli get AAPL --section 1A --output md | head -25" \
    bash -c "'$BIN' get AAPL --section 1A --output md | head -25"

# scene 3: accuracy harness (fast, hermetic)
scene "extraction accuracy — 100% on 4 real-format fixtures" \
    "sec-cli accuracy internal/accuracy/testdata/corpus" \
    "$BIN" accuracy internal/accuracy/testdata/corpus

tick 0.5; event $'\r\n'
event "go install github.com/kritidutta01/sec-cli/cmd/sec-cli@latest"$'\r\n'
tick 1.5

printf '%s' "$EVENTS"
