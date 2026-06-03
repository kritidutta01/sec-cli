package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/net/html"

	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/kritidutta01/sec-cli/internal/ixbrl"
	"github.com/kritidutta01/sec-cli/internal/router"
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
	root.AddCommand(fetchCmd())
	root.AddCommand(detectCmd())
	root.AddCommand(factsCmd())

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

// fetchCmd fetches a filing's primary document and prints the first 200 bytes to
// stdout. It is the Phase 3 demo surface for primary-document fetch; format
// detection and parsing arrive in later phases.
func fetchCmd() *cobra.Command {
	var (
		form string
		year int
	)
	cmd := &cobra.Command{
		Use:   "fetch <ticker>",
		Short: "Fetch a filing's primary document (prints the first 200 bytes)",
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

			var filing edgar.Filing
			if year != 0 {
				filing, err = client.FilingForYear(ctx, cik, form, year)
			} else {
				filing, err = client.LatestFiling(ctx, cik, form)
			}
			if err != nil {
				return err
			}

			body, err := client.FetchPrimaryDocument(ctx, cik, filing)
			if err != nil {
				return err
			}

			if len(body) > 200 {
				body = body[:200]
			}
			_, _ = os.Stdout.Write(body)
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	return cmd
}

// detectCmd fetches a filing's primary document, runs the format router over it,
// and prints the classified format to stdout. It is the Phase 4 demo surface.
func detectCmd() *cobra.Command {
	var (
		form string
		year int
	)
	cmd := &cobra.Command{
		Use:   "detect <ticker>",
		Short: "Classify a filing's format (IXBRL, PartialIXBRL, PlainHTML, PlainText, Unknown)",
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

			var filing edgar.Filing
			if year != 0 {
				filing, err = client.FilingForYear(ctx, cik, form, year)
			} else {
				filing, err = client.LatestFiling(ctx, cik, form)
			}
			if err != nil {
				return err
			}

			raw, err := client.FetchPrimaryDocument(ctx, cik, filing)
			if err != nil {
				return err
			}

			dec, err := router.Detect(ctx, raw, indexFetcher(client, cik, filing))
			if err != nil {
				return err
			}

			for _, w := range dec.Warnings {
				fmt.Fprintln(os.Stderr, "sec-cli: warning:", w)
			}
			fmt.Println(dec.Format)
			return nil
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	return cmd
}

// factsCmd extracts the iXBRL fact stream for a filing and prints the facts,
// optionally filtered to concepts containing --concept. It is the Phase 5 demo
// surface for context + fact extraction.
func factsCmd() *cobra.Command {
	var (
		form    string
		year    int
		concept string
	)
	cmd := &cobra.Command{
		Use:   "facts <ticker>",
		Short: "List the inline-XBRL facts of a filing (optionally filtered by concept)",
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

			var filing edgar.Filing
			if year != 0 {
				filing, err = client.FilingForYear(ctx, cik, form, year)
			} else {
				filing, err = client.LatestFiling(ctx, cik, form)
			}
			if err != nil {
				return err
			}

			raw, err := client.FetchPrimaryDocument(ctx, cik, filing)
			if err != nil {
				return err
			}

			dec, err := router.Detect(ctx, raw, indexFetcher(client, cik, filing))
			if err != nil {
				return err
			}
			if !dec.Format.Supported() {
				return fmt.Errorf("%s is %s; v1.0 supports inline-XBRL filings only", args[0], dec.Format)
			}

			doc, err := html.Parse(bytes.NewReader(dec.PrimaryDocument))
			if err != nil {
				return fmt.Errorf("parse filing: %w", err)
			}
			contexts, err := ixbrl.ParseContexts(doc)
			if err != nil {
				return err
			}
			facts, err := ixbrl.ExtractFacts(doc, contexts)
			if err != nil {
				return err
			}

			needle := strings.ToLower(concept)
			for _, f := range facts {
				if needle != "" && !strings.Contains(strings.ToLower(f.Concept), needle) {
					continue
				}
				printFact(contexts[f.ContextRef], f)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	cmd.Flags().StringVar(&concept, "concept", "", "only facts whose concept contains this string")
	return cmd
}

// printFact writes one fact as a tab-separated line: concept, period, value, unit.
func printFact(c ixbrl.Context, f ixbrl.Fact) {
	var value string
	switch {
	case !f.Numeric:
		value = f.Raw
	case f.Unparsed:
		value = f.Raw + " (unparsed)"
	default:
		value = strconv.FormatFloat(f.Value, 'f', -1, 64)
	}
	fmt.Printf("%s\t%s\t%s\t%s\n", f.Concept, period(c), value, f.UnitRef)
}

// period renders a context's reporting period: a single date for an instant,
// or start..end for a duration.
func period(c ixbrl.Context) string {
	const dateLayout = "2006-01-02"
	if c.IsInstant() {
		return c.Start.Format(dateLayout)
	}
	return c.Start.Format(dateLayout) + ".." + c.End.Format(dateLayout)
}

// indexFetcher returns a router.Fetcher that resolves a filing-index document
// reference against EDGAR. References may be absolute URLs, site-absolute paths,
// or filenames relative to the filing's Archives directory.
func indexFetcher(client *edgar.Client, cik int64, filing edgar.Filing) router.Fetcher {
	base := strings.TrimSuffix(edgar.DocumentURL(cik, filing), filing.PrimaryDocument)
	return func(ctx context.Context, ref string) ([]byte, error) {
		var url string
		switch {
		case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
			url = ref
		case strings.HasPrefix(ref, "/"):
			url = "https://www.sec.gov" + ref
		default:
			url = base + ref
		}
		return client.Get(ctx, url)
	}
}
