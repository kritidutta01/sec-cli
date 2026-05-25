package cmd

import (
	"fmt"
	"os"

	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/spf13/cobra"
)

var (
	filingType string
	outputFmt  string
)

var getCmd = &cobra.Command{
	Use:   "get <ticker>",
	Short: "Fetch a SEC filing",
	Example: `  sec-cli get AAPL
  sec-cli get MSFT --type 10-Q
  sec-cli get NVDA --output json`,
	Args: cobra.ExactArgs(1),
	RunE: runGet,
}

func init() {
	rootCmd.AddCommand(getCmd)
	getCmd.Flags().StringVarP(&filingType, "type", "t", "10-K", "Filing type (10-K, 10-Q, 8-K)")
	getCmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, md")
}

func runGet(cmd *cobra.Command, args []string) error {
	ticker := args[0]
	client := edgar.NewClient()

	fmt.Fprintf(os.Stderr, "→ Looking up %s...\n", ticker)
	cik, err := client.LookupCIK(ticker)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "→ CIK %d — fetching latest %s...\n", cik, filingType)
	filing, err := client.LatestFiling(cik, filingType)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "→ Filed %s — downloading %s...\n", filing.FilingDate, filing.PrimaryDocument)
	html, err := client.FetchFilingHTML(cik, filing)
	if err != nil {
		return err
	}

	// Week 1: raw HTML to stdout. Week 2 adds section parsing and proper --output handling.
	_, err = os.Stdout.Write(html)
	return err
}
