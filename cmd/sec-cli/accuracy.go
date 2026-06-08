package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kritidutta01/sec-cli/internal/accuracy"
)

// accuracyCmd is a hidden developer surface: it runs the accuracy harness over a
// corpus directory and prints the report (per-filing statement accuracy, section
// coverage, and per-bucket confidence calibration). It is not a user-facing
// feature — the harness's real home is `go test ./internal/accuracy`, the CI
// regression gate — but the subcommand makes local iteration on the corpus and
// the confidence thresholds quick. The corpus path is passed explicitly so the
// command stays hermetic (it never reaches the network).
func accuracyCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "accuracy <corpus-dir>",
		Short:  "Score the extraction pipeline against a corpus of hand-verified filings",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			// The harness builds EDGAR clients via edgar.NewClient, which requires a
			// Fair-Access contact identifier even though every fetch is served from the
			// corpus. Default one for this local-only, network-free run.
			if os.Getenv("SEC_CLI_USER_AGENT") == "" {
				_ = os.Setenv("SEC_CLI_USER_AGENT", "sec-cli accuracy harness (local)")
			}
			rep, err := accuracy.Score(args[0])
			if err != nil {
				return err
			}
			fmt.Print(rep.String())
			return nil
		},
	}
}
