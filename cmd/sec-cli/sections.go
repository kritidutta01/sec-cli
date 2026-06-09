package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/net/html"

	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/kritidutta01/sec-cli/internal/pipeline"
	"github.com/kritidutta01/sec-cli/internal/router"
	"github.com/kritidutta01/sec-cli/internal/sections"
)

// sectionsCmd partitions a filing into its named item sections and prints one
// line per section: item id, title, and a character/paragraph count. It is the
// Phase 8 demo surface for section partitioning.
func sectionsCmd() *cobra.Command {
	var (
		form string
		year int
	)
	cmd := &cobra.Command{
		Use:   "sections <ticker>",
		Short: "List a filing's detected item sections (Business, Risk Factors, MD&A, …)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := partitionFiling(cmd, args[0], form, year)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "sec-cli: partition confidence: %s (strategy: %s)\n",
				p.Confidence.Level, p.Strategy)
			for _, s := range p.Sections {
				fmt.Printf("Item %s\t%s\t%d chars\t%d paragraphs\n",
					s.Item, s.Title, len(s.Text), paragraphCount(s.Text))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	return cmd
}

// textCmd prints the rendered free text of one section, selected by item id or a
// title/heading substring. It is the Phase 8 demo surface for free-text
// rendering.
func textCmd() *cobra.Command {
	var (
		form    string
		year    int
		section string
	)
	cmd := &cobra.Command{
		Use:   "text <ticker> --section <name|item>",
		Short: "Print the rendered free text of one section",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(section) == "" {
				return fmt.Errorf("--section is required (an item id like 1A or a title substring like \"risk\")")
			}
			p, err := partitionFiling(cmd, args[0], form, year)
			if err != nil {
				return err
			}
			sec, ok := selectSection(p, section)
			if !ok {
				return fmt.Errorf("section %q not found", section)
			}
			fmt.Println(sec.Text)
			return nil
		},
	}
	cmd.Flags().StringVarP(&form, "type", "t", "10-K", "filing form type (e.g. 10-K, 10-Q, 8-K)")
	cmd.Flags().IntVar(&year, "year", 0, "target filing year (default: latest)")
	cmd.Flags().StringVar(&section, "section", "", "section to print: an item id (1A) or a title/heading substring")
	return cmd
}

// partitionFiling runs the fetch→detect→parse→partition preamble shared by the
// sections and text commands. For PartialIXBRL filings it records the per-section
// formats on the decision (wiring the long-standing SectionFormats placeholder).
func partitionFiling(cmd *cobra.Command, ticker, form string, year int) (sections.Result, error) {
	fc, err := newFetchContext(cmd, ticker, form, year)
	if err != nil {
		return sections.Result{}, err
	}
	defer func() { _ = fc.close() }()
	ctx := cmd.Context()

	raw, err := fc.fetch(ctx, edgar.DocumentURL(fc.cik, fc.filing))
	if err != nil {
		return sections.Result{}, err
	}

	dec, err := router.Detect(ctx, raw, pipeline.IndexFetcher(fc.fetch, fc.cik, fc.filing))
	if err != nil {
		return sections.Result{}, err
	}
	if !dec.Format.Supported() {
		return sections.Result{}, fmt.Errorf("%s is %s; v1.0 supports inline-XBRL filings only", ticker, dec.Format)
	}

	doc, err := html.Parse(bytes.NewReader(dec.PrimaryDocument))
	if err != nil {
		return sections.Result{}, fmt.Errorf("parse filing: %w", err)
	}

	p, err := sections.Partition(doc)
	if err != nil {
		return sections.Result{}, err
	}
	if dec.Format == router.PartialIXBRL {
		dec.SectionFormats = sections.SectionFormats(p)
	}
	return p, nil
}

// selectSection finds the section matching sel: an exact (case-insensitive) item
// id first, then a title or heading substring.
func selectSection(p sections.Result, sel string) (sections.Section, bool) {
	needle := strings.ToLower(strings.TrimSpace(sel))
	for _, s := range p.Sections {
		if strings.ToLower(s.Item) == needle {
			return s, true
		}
	}
	for _, s := range p.Sections {
		if strings.Contains(strings.ToLower(s.Title), needle) ||
			strings.Contains(strings.ToLower(s.Heading), needle) {
			return s, true
		}
	}
	return sections.Section{}, false
}

// paragraphCount counts the blank-line-separated paragraphs in rendered text.
func paragraphCount(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return strings.Count(text, "\n\n") + 1
}
