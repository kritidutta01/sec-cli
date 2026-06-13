#!/bin/bash
# asciinema recording script — run as:
#   asciinema rec demo.cast -c "bash demo_record.sh" --title "sec-cli demo"
# Cache must be pre-warmed before recording (sec-cli get AAPL --section "Risk Factors").

BINARY="$(dirname "$(realpath "$0")")/bin/sec-cli-linux"
CORPUS="$(dirname "$(realpath "$0")")/internal/accuracy/testdata/corpus"
export SEC_CLI_USER_AGENT="Kriti Dutta data.kriti.dutta@gmail.com"

G='\033[1;32m'  # green
B='\033[1m'     # bold
R='\033[0m'     # reset

cmd() {
    echo ""
    printf "${G}\$${R} ${B}$*${R}\n"
    sleep 0.7
}

clear
printf "${B}sec-cli${R}  —  SEC EDGAR filings → LLM-ready JSON / Markdown\n"
printf "No API key. No paid service. Single binary.\n"
sleep 2

# 1. List all detected sections of Apple's latest 10-K
cmd "sec-cli sections AAPL"
"$BINARY" sections AAPL 2>&1
sleep 1.8

# 2. Extract Risk Factors as clean Markdown (uses cache, fast)
cmd "sec-cli get AAPL --section 'Risk Factors' --output md | head -18"
"$BINARY" get AAPL --section "Risk Factors" --output md 2>/dev/null | head -18
sleep 1.8

# 3. Run the accuracy harness (hermetic — no network, 7 filings, 76 cells)
cmd "sec-cli accuracy internal/accuracy/testdata/corpus"
"$BINARY" accuracy "$CORPUS"
sleep 2

echo ""
printf "${G}go install github.com/kritidutta01/sec-cli/cmd/sec-cli@latest${R}\n"
printf "github.com/kritidutta01/sec-cli\n"
sleep 1.5
