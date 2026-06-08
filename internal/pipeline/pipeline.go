// Package pipeline is the conductor: the one place that knows the full
// ticker → fetch → detect → parse → project → partition → assemble sequence and
// returns a normalized *model.Document. Both the `get` command and (eventually)
// the Python wrapper drive it, so the orchestration lives here once rather than
// being re-implemented per surface.
//
// Failure is partial, not total: if a statement does not project or section
// partitioning is low-confidence, the document still assembles with what
// succeeded, and the document-level confidence reflects the gap. This is the
// LLM-consumption stance — return the best available structured view, labeled
// with how much to trust it.
package pipeline

import (
	"bytes"
	"context"
	"fmt"

	"golang.org/x/net/html"

	"github.com/kritidutta01/sec-cli/internal/cache"
	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/kritidutta01/sec-cli/internal/ixbrl"
	"github.com/kritidutta01/sec-cli/internal/model"
	"github.com/kritidutta01/sec-cli/internal/router"
	"github.com/kritidutta01/sec-cli/internal/sections"
)

// defaultStatements is the statement projection order tried when Options.Statements
// is empty. Statements that do not resolve are skipped (partial success), not fatal.
var defaultStatements = []string{"income", "balance", "cashflow", "equity", "comprehensive"}

// Options drives one pipeline run.
type Options struct {
	// Ticker is the company symbol (case-insensitive).
	Ticker string
	// Form is the filing form type; empty defaults to "10-K".
	Form string
	// Year selects the filing filed in that calendar year; 0 means latest.
	Year int
	// Statements is the set of statement keys to project (income, balance,
	// cashflow, equity, comprehensive); empty means the default set.
	Statements []string
	// Cache is the raw+parsed cache. A nil cache behaves as a no-op (every lookup
	// misses, every write is dropped), so callers can pass nil for an uncached run.
	Cache *cache.Cache
	// Client is the EDGAR client; nil means construct one with edgar.NewClient.
	// Tests inject a fixture-backed client here.
	Client *edgar.Client
}

// Run executes the full pipeline and returns the assembled Document. It resolves
// the filing, serves an already-parsed document straight from the cache when one
// exists for this accession and parser version, and otherwise fetches, detects,
// parses, projects, partitions, assembles, and caches.
func Run(ctx context.Context, opts Options) (*model.Document, error) {
	client := opts.Client
	if client == nil {
		var err error
		client, err = edgar.NewClient()
		if err != nil {
			return nil, err
		}
	}
	form := opts.Form
	if form == "" {
		form = "10-K"
	}

	cik, err := client.LookupCIK(ctx, opts.Ticker)
	if err != nil {
		return nil, err
	}

	var filing edgar.Filing
	if opts.Year != 0 {
		filing, err = client.FilingForYear(ctx, cik, form, opts.Year)
	} else {
		filing, err = client.LatestFiling(ctx, cik, form)
	}
	if err != nil {
		return nil, err
	}

	// A parsed document for this accession under the current parserVersion is the
	// finished artifact — return it without re-fetching or re-parsing. (--no-cache
	// passes a no-op cache, so this always misses and the full path runs.)
	if doc, ok := opts.Cache.GetDocument(filing.AccessionNumber); ok {
		return doc, nil
	}

	// Every EDGAR document fetch from here flows through the cache's raw layer:
	// the primary document, the router's index resolution, and the linkbases.
	fetch := opts.Cache.Fetching(client.Get)

	raw, err := fetch(ctx, edgar.DocumentURL(cik, filing))
	if err != nil {
		return nil, fmt.Errorf("pipeline: fetch primary document: %w", err)
	}

	dec, err := router.Detect(ctx, raw, IndexFetcher(fetch, cik, filing))
	if err != nil {
		return nil, err
	}
	if !dec.Format.Supported() {
		return nil, fmt.Errorf("%s is %s; v1.0 supports inline-XBRL filings only", opts.Ticker, dec.Format)
	}

	node, err := html.Parse(bytes.NewReader(dec.PrimaryDocument))
	if err != nil {
		return nil, fmt.Errorf("pipeline: parse filing: %w", err)
	}

	wantStatements := opts.Statements
	if len(wantStatements) == 0 {
		wantStatements = defaultStatements
	}
	statements := projectStatements(ctx, client, fetch, cik, filing, node, wantStatements)

	// Section partitioning is best-effort: a filing with no recoverable structure
	// (e.g. a 10-Q whose item grammar differs) yields no sections rather than an
	// error, and the document still carries its statements.
	secResult, _ := sections.Partition(node)
	modelSections := make([]model.Section, 0, len(secResult.Sections))
	for _, s := range secResult.Sections {
		modelSections = append(modelSections, s.Model())
	}

	doc := &model.Document{
		Metadata:   buildMetadata(opts.Ticker, cik, filing, secResult.Confidence, statements),
		Sections:   modelSections,
		Statements: statements,
	}

	if err := opts.Cache.PutDocument(filing.AccessionNumber, doc); err != nil {
		// A cache write failure must not fail the run — the document is still good.
		// Surface nothing here; the next run simply re-parses.
		_ = err
	}
	return doc, nil
}

// projectStatements loads the filing's statement roles once, then projects each
// requested statement from the shared fact stream. A statement whose role does
// not resolve, or that projects no rows, is skipped — the run continues with
// whatever succeeded.
func projectStatements(ctx context.Context, client *edgar.Client, fetch cache.FetchFunc, cik int64, filing edgar.Filing, node *html.Node, want []string) []model.Table {
	roles, err := LoadRoles(ctx, client, fetch, cik, filing)
	if err != nil {
		return nil
	}
	contexts, err := ixbrl.ParseContexts(node)
	if err != nil {
		return nil
	}
	facts, err := ixbrl.ExtractFacts(node, contexts)
	if err != nil {
		return nil
	}

	out := make([]model.Table, 0, len(want))
	for _, key := range want {
		keywords, ok := StatementKeywords[key]
		if !ok {
			continue
		}
		role, ok := SelectRole(roles, keywords)
		if !ok {
			continue
		}
		tbl := ixbrl.ProjectTable(role, facts, contexts)
		if len(tbl.Rows) == 0 {
			continue
		}
		out = append(out, *tbl)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildMetadata stamps the document: the filing coordinates, the period it
// reports, the schema/parser versions, and a document-level confidence combining
// the section partition and statement confidences.
func buildMetadata(ticker string, cik int64, filing edgar.Filing, sectionConf model.Confidence, statements []model.Table) model.Metadata {
	const dateLayout = "2006-01-02"
	m := model.Metadata{
		CIK:           cik,
		Ticker:        ticker,
		Form:          filing.Form,
		Accession:     filing.AccessionNumber,
		SchemaVersion: model.SchemaVersion,
		ParserVersion: model.ParserVersion,
		Source:        model.Source{Extractor: "pipeline", ParserVersion: model.ParserVersion},
		Confidence:    combineConfidence(sectionConf, statements),
	}
	if !filing.FilingDate.IsZero() {
		m.FilingDate = filing.FilingDate.Format(dateLayout)
	}
	if !filing.ReportDate.IsZero() {
		m.PeriodEnd = filing.ReportDate.Format(dateLayout)
	}
	return m
}

// confidenceRank orders the calibrated buckets so the worst one can be picked.
var confidenceRank = map[string]int{"high": 3, "medium": 2, "low": 1, "": 0}

// combineConfidence is the document-level signal: the weakest level among the
// section partition and the projected statements (a document is only as
// trustworthy as its softest part), with the resolved/match rates averaged over
// the statements so the numbers describe the whole filing.
func combineConfidence(sectionConf model.Confidence, statements []model.Table) model.Confidence {
	level := sectionConf.Level
	worst := func(other string) {
		if confidenceRank[other] != 0 && (confidenceRank[level] == 0 || confidenceRank[other] < confidenceRank[level]) {
			level = other
		}
	}

	var sumRow, sumCell float64
	var untagged int
	for _, t := range statements {
		worst(t.Confidence.Level)
		sumRow += t.Confidence.RowMatchRate
		sumCell += t.Confidence.CellResolvedRate
		untagged += t.Confidence.UntaggedCellCount
	}
	if level == "" {
		level = "low"
	}

	conf := model.Confidence{Level: level, UntaggedCellCount: untagged}
	if n := len(statements); n > 0 {
		conf.RowMatchRate = sumRow / float64(n)
		conf.CellResolvedRate = sumCell / float64(n)
	}
	return conf
}
