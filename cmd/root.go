package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "sec-cli",
	Short: "Fast CLI for SEC filings, built for LLM workflows",
	Long: `sec-cli fetches, parses, and diffs SEC EDGAR filings.

Outputs clean JSON or Markdown — pipe directly into LLMs.
All network traffic goes to EDGAR's public APIs; no API key required.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
