package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kritidutta01/sec-cli/internal/edgar"
)

const version = "0.0.0-dev"

func main() {
	root := &cobra.Command{
		Use:           "sec-cli",
		Short:         "Extract structured data from SEC EDGAR filings for LLM consumption",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the sec-cli version",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	})

	root.AddCommand(latestCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sec-cli:", err)
		os.Exit(1)
	}
}

// latestCmd prints the accession number of a company's most recent filing of a
// given form type. It is the Phase 2 demo surface for CIK lookup + submissions.
func latestCmd() *cobra.Command {
	var form string
	cmd := &cobra.Command{
		Use:   "latest <ticker>",
		Short: "Print the accession number of the latest filing of a form type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := edgar.NewClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			cik, err := client.LookupCIK(ctx, args[0])
			if err != nil {
				return err
			}
			filing, err := client.LatestFiling(ctx, cik, form)
			if err != nil {
				return err
			}
			fmt.Println(filing.AccessionNumber)
			return nil
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	return cmd
}
