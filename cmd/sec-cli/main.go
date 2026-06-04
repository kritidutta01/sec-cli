package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	root.AddCommand(tableCmd())

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

// statementKeywords maps the --statement flag to the role-URI fragments that
// identify that financial statement. A role matches if its URI (case-folded,
// punctuation removed) contains any fragment.
var statementKeywords = map[string][]string{
	"income":        {"statementsofoperations", "statementsofincome"},
	"balance":       {"balancesheets", "financialposition"},
	"cashflow":      {"statementsofcashflows", "cashflow"},
	"equity":        {"stockholdersequity", "shareholdersequity"},
	"comprehensive": {"comprehensiveincome"},
}

// tableCmd prints a filing's table as JSON. By default it projects a financial
// statement from the fact stream and presentation linkbase (Phase 6); with
// --layout it extracts a narrative table by position via the layout fallback
// (Phase 7). Section-name targeting of layout tables arrives with sections in a
// later phase; --index selects among the document's tables for now.
func tableCmd() *cobra.Command {
	var (
		form      string
		year      int
		statement string
		layout    bool
		index     int
	)
	cmd := &cobra.Command{
		Use:   "table <ticker>",
		Short: "Print a filing's table as JSON (a statement, or a narrative table with --layout)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var keywords []string
			if !layout {
				k, ok := statementKeywords[statement]
				if !ok {
					return fmt.Errorf("unknown statement %q (choose income, balance, cashflow, equity, comprehensive)", statement)
				}
				keywords = k
			}

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

			var tbl *ixbrl.Table
			if layout {
				tables := collectTables(doc)
				if index < 0 || index >= len(tables) {
					return fmt.Errorf("table index %d out of range (filing has %d tables)", index, len(tables))
				}
				tbl = ixbrl.ExtractLayoutTable(tables[index])
			} else {
				role, err := loadRole(ctx, client, cik, filing, keywords)
				if err != nil {
					return err
				}
				contexts, err := ixbrl.ParseContexts(doc)
				if err != nil {
					return err
				}
				facts, err := ixbrl.ExtractFacts(doc, contexts)
				if err != nil {
					return err
				}
				tbl = ixbrl.ProjectTable(role, facts, contexts)
			}

			out, err := json.MarshalIndent(tbl, "", "  ")
			if err != nil {
				return fmt.Errorf("encode table: %w", err)
			}
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	cmd.Flags().StringVar(&statement, "statement", "income", "statement to project (income, balance, cashflow, equity, comprehensive)")
	cmd.Flags().BoolVar(&layout, "layout", false, "extract a narrative (non-fact-tagged) table by position via the layout fallback")
	cmd.Flags().IntVar(&index, "index", 0, "with --layout, the zero-based index of the table to extract")
	return cmd
}

// collectTables returns every <table> element in the document, in document order.
func collectTables(doc *html.Node) []*html.Node {
	var tables []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			tables = append(tables, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return tables
}

// loadRole fetches a filing's presentation and label linkbases plus its schema,
// parses them, and returns the role whose URI matches one of keywords. Roles for
// disclosure details, tables, and parentheticals are skipped so the keyword
// resolves to the primary statement.
func loadRole(ctx context.Context, client *edgar.Client, cik int64, filing edgar.Filing, keywords []string) (ixbrl.LinkbaseRole, error) {
	files, err := client.FilingFiles(ctx, cik, filing)
	if err != nil {
		return ixbrl.LinkbaseRole{}, err
	}
	preName := firstWithSuffix(files, "_pre.xml")
	labName := firstWithSuffix(files, "_lab.xml")
	if preName == "" || labName == "" {
		return ixbrl.LinkbaseRole{}, fmt.Errorf("filing has no presentation/label linkbase (pre=%q lab=%q)", preName, labName)
	}

	preXML, err := client.Get(ctx, edgar.CompanionURL(cik, filing, preName))
	if err != nil {
		return ixbrl.LinkbaseRole{}, fmt.Errorf("fetch presentation linkbase: %w", err)
	}
	labXML, err := client.Get(ctx, edgar.CompanionURL(cik, filing, labName))
	if err != nil {
		return ixbrl.LinkbaseRole{}, fmt.Errorf("fetch label linkbase: %w", err)
	}

	var titles map[string]string
	if xsdName := firstSchema(files); xsdName != "" {
		if xsd, err := client.Get(ctx, edgar.CompanionURL(cik, filing, xsdName)); err == nil {
			titles, _ = ixbrl.ParseRoleTitles(xsd)
		}
	}

	roles, err := ixbrl.ParseLinkbase(preXML, labXML, titles)
	if err != nil {
		return ixbrl.LinkbaseRole{}, err
	}

	role, ok := selectRole(roles, keywords)
	if !ok {
		return ixbrl.LinkbaseRole{}, fmt.Errorf("no matching statement role in filing")
	}
	return role, nil
}

// selectRole returns the first non-disclosure statement role whose URI matches a
// keyword.
func selectRole(roles []ixbrl.LinkbaseRole, keywords []string) (ixbrl.LinkbaseRole, bool) {
	for _, r := range roles {
		u := foldRoleURI(r.RoleURI)
		if strings.Contains(u, "details") || strings.Contains(u, "tables") ||
			strings.Contains(u, "parenthetical") || strings.Contains(u, "disclosure") {
			continue
		}
		for _, k := range keywords {
			if strings.Contains(u, k) {
				return r, true
			}
		}
	}
	return ixbrl.LinkbaseRole{}, false
}

// foldRoleURI lowercases a role URI and drops everything but letters and digits,
// so "CONSOLIDATEDSTATEMENTSOFOPERATIONS" matches the keyword
// "statementsofoperations".
func foldRoleURI(uri string) string {
	tail := uri
	if i := strings.LastIndex(uri, "/"); i >= 0 {
		tail = uri[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(tail) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstWithSuffix(names []string, suffix string) string {
	for _, n := range names {
		if strings.HasSuffix(n, suffix) {
			return n
		}
	}
	return ""
}

// firstSchema returns the filing's own taxonomy schema (the top-level
// "<ticker>-<date>.xsd"), distinguished from linkbase files by its .xsd suffix.
func firstSchema(names []string) string {
	for _, n := range names {
		if strings.HasSuffix(n, ".xsd") {
			return n
		}
	}
	return ""
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
