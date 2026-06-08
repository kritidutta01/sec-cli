package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/net/html"

	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/kritidutta01/sec-cli/internal/ixbrl"
	"github.com/kritidutta01/sec-cli/internal/model"
	"github.com/kritidutta01/sec-cli/internal/pipeline"
	"github.com/kritidutta01/sec-cli/internal/render"
	"github.com/kritidutta01/sec-cli/internal/router"
)

// version is the build identity. It defaults to a dev sentinel and is overridden
// at release time via the linker: `-ldflags "-X main.version=<tag>"` (goreleaser
// injects the git tag). It is a var, not a const, precisely so the linker can set
// it. See .goreleaser.yaml and docs/versioning.md.
var version = "0.0.0-dev"

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

	// --no-cache is global: any fetching subcommand reads it (via flag
	// inheritance) to bypass the SQLite cache for a one-off, uncached run.
	root.PersistentFlags().Bool("no-cache", false, "bypass the local EDGAR cache for this run")

	root.AddCommand(getCmd())
	root.AddCommand(diffCmd())
	root.AddCommand(latestCmd())
	root.AddCommand(fetchCmd())
	root.AddCommand(detectCmd())
	root.AddCommand(factsCmd())
	root.AddCommand(tableCmd())
	root.AddCommand(sectionsCmd())
	root.AddCommand(textCmd())
	root.AddCommand(cacheCmd())
	root.AddCommand(accuracyCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sec-cli:", err)
		os.Exit(1)
	}
}

// getCmd is the headline command: it turns a ticker into a finished, rendered
// document — fetch → detect → parse → project → partition → render — with the
// cache underneath. It drives internal/pipeline, the one orchestrator both this
// command and the Python wrapper share.
func getCmd() *cobra.Command {
	var (
		form      string
		year      int
		format    string
		section   string
		accession string
	)
	cmd := &cobra.Command{
		Use:   "get <ticker>",
		Short: "Fetch, parse, and render a filing as a normalized document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			renderer, err := render.For(format)
			if err != nil {
				return err
			}
			c, err := openCache(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = c.Close() }()

			doc, err := pipeline.Run(cmd.Context(), pipeline.Options{
				Ticker:    args[0],
				Form:      form,
				Year:      year,
				Accession: accession,
				Cache:     c,
			})
			if err != nil {
				return err
			}
			if section != "" {
				doc, err = filterSection(doc, section)
				if err != nil {
					return err
				}
			}
			return renderer.Render(doc, os.Stdout)
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	cmd.Flags().StringVarP(&format, "output", "o", "json", "output format (json, md, text)")
	cmd.Flags().StringVarP(&format, "format", "f", "json", "output format (alias for --output)")
	cmd.Flags().StringVar(&section, "section", "", "render only the section matching this item id or title substring")
	cmd.Flags().StringVar(&accession, "accession", "", "pin to a specific accession number (overrides --year)")
	return cmd
}

// filterSection narrows a document to the single section the user named — by
// item id (e.g. "1A") or a title substring — for focused output. The
// financial-statements section additionally carries the projected statements.
// On no match it exits with code 1 and lists available sections.
func filterSection(doc *model.Document, sel string) (*model.Document, error) {
	needle := strings.ToLower(strings.TrimSpace(sel))
	for _, s := range doc.Sections {
		if strings.EqualFold(s.Item, sel) || (needle != "" && strings.Contains(strings.ToLower(s.Title), needle)) {
			out := &model.Document{Metadata: doc.Metadata, Sections: []model.Section{s}}
			if s.Kind == model.KindFinancial {
				out.Statements = doc.Statements
			}
			return out, nil
		}
	}
	// List available sections so the user knows what to ask for.
	fmt.Fprintf(os.Stderr, "sec-cli: no section matching %q\n\nAvailable sections:\n", sel)
	for _, s := range doc.Sections {
		fmt.Fprintf(os.Stderr, "  Item %-4s  %s\n", s.Item, s.Title)
	}
	os.Exit(1)
	return nil, nil // unreachable
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
// stdout. It is the Phase 3 demo surface for primary-document fetch.
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
			fc, err := newFetchContext(cmd, args[0], form, year)
			if err != nil {
				return err
			}
			defer func() { _ = fc.close() }()

			body, err := fc.fetch(cmd.Context(), edgar.DocumentURL(fc.cik, fc.filing))
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
			fc, err := newFetchContext(cmd, args[0], form, year)
			if err != nil {
				return err
			}
			defer func() { _ = fc.close() }()
			ctx := cmd.Context()

			raw, err := fc.fetch(ctx, edgar.DocumentURL(fc.cik, fc.filing))
			if err != nil {
				return err
			}

			dec, err := router.Detect(ctx, raw, pipeline.IndexFetcher(fc.fetch, fc.cik, fc.filing))
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
			fc, err := newFetchContext(cmd, args[0], form, year)
			if err != nil {
				return err
			}
			defer func() { _ = fc.close() }()
			ctx := cmd.Context()

			raw, err := fc.fetch(ctx, edgar.DocumentURL(fc.cik, fc.filing))
			if err != nil {
				return err
			}

			dec, err := router.Detect(ctx, raw, pipeline.IndexFetcher(fc.fetch, fc.cik, fc.filing))
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

// tableCmd renders a filing's table through internal/render in the chosen
// format (json|md|text). By default it projects a financial statement from the
// fact stream and presentation linkbase (Phase 6); with --layout it extracts a
// narrative table by position via the layout fallback (Phase 7).
func tableCmd() *cobra.Command {
	var (
		form      string
		year      int
		statement string
		layout    bool
		index     int
		format    string
	)
	cmd := &cobra.Command{
		Use:   "table <ticker>",
		Short: "Render a filing's table (a statement, or a narrative table with --layout)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			renderer, err := render.For(format)
			if err != nil {
				return err
			}
			tbl, err := produceTable(cmd, args[0], form, year, statement, layout, index)
			if err != nil {
				return err
			}
			doc := &model.Document{Statements: []model.Table{*tbl}}
			return renderer.Render(doc, os.Stdout)
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	cmd.Flags().StringVar(&statement, "statement", "income", "statement to project (income, balance, cashflow, equity, comprehensive)")
	cmd.Flags().BoolVar(&layout, "layout", false, "extract a narrative (non-fact-tagged) table by position via the layout fallback")
	cmd.Flags().IntVar(&index, "index", 0, "with --layout, the zero-based index of the table to extract")
	cmd.Flags().StringVarP(&format, "format", "f", "json", "output format (json, md, text)")
	return cmd
}

// produceTable fetches a filing, classifies it, and produces one table: a
// projected statement by default, or a narrative table by position with
// --layout. It uses the pipeline's shared orchestration helpers so there is one
// copy of the role-loading and table-collection logic.
func produceTable(cmd *cobra.Command, ticker, form string, year int, statement string, layout bool, index int) (*model.Table, error) {
	var keywords []string
	if !layout {
		k, ok := pipeline.StatementKeywords[statement]
		if !ok {
			return nil, fmt.Errorf("unknown statement %q (choose income, balance, cashflow, equity, comprehensive)", statement)
		}
		keywords = k
	}

	fc, err := newFetchContext(cmd, ticker, form, year)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fc.close() }()
	ctx := cmd.Context()

	raw, err := fc.fetch(ctx, edgar.DocumentURL(fc.cik, fc.filing))
	if err != nil {
		return nil, err
	}
	dec, err := router.Detect(ctx, raw, pipeline.IndexFetcher(fc.fetch, fc.cik, fc.filing))
	if err != nil {
		return nil, err
	}
	if !dec.Format.Supported() {
		return nil, fmt.Errorf("%s is %s; v1.0 supports inline-XBRL filings only", ticker, dec.Format)
	}

	node, err := html.Parse(bytes.NewReader(dec.PrimaryDocument))
	if err != nil {
		return nil, fmt.Errorf("parse filing: %w", err)
	}

	if layout {
		tables := pipeline.CollectTables(node)
		if index < 0 || index >= len(tables) {
			return nil, fmt.Errorf("table index %d out of range (filing has %d tables)", index, len(tables))
		}
		return ixbrl.ExtractLayoutTable(tables[index]), nil
	}

	roles, err := pipeline.LoadRoles(ctx, fc.client, fc.fetch, fc.cik, fc.filing)
	if err != nil {
		return nil, err
	}
	role, ok := pipeline.SelectRole(roles, keywords)
	if !ok {
		return nil, fmt.Errorf("no matching statement role in filing")
	}
	contexts, err := ixbrl.ParseContexts(node)
	if err != nil {
		return nil, err
	}
	facts, err := ixbrl.ExtractFacts(node, contexts)
	if err != nil {
		return nil, err
	}
	return ixbrl.ProjectTable(role, facts, contexts), nil
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
