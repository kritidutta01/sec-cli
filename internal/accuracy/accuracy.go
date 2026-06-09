// Package accuracy is the corpus scoring harness: it runs the full Phase 11
// pipeline against a set of recorded filings (hermetic — the corpus ships the
// bytes, nothing touches the network) and scores the assembled documents
// against hand-verified baselines. It measures three things — per-field
// statement accuracy, section coverage, and confidence calibration (accuracy
// within each confidence bucket) — so "looks right against one filing" becomes
// a measured, regression-gated number, and so the confidence thresholds in
// internal/ixbrl can be checked against reality rather than intuition.
package accuracy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/kritidutta01/sec-cli/internal/model"
	"github.com/kritidutta01/sec-cli/internal/pipeline"
)

// Baseline is the hand-verified truth for one corpus filing: the expected
// statement values (by statement role, concept, and period) and the expected
// section item ids in order. It is the independent ground truth the parser's
// output is scored against — authored by reading the filing, not by running the
// parser — so a divergence means a parser regression.
type Baseline struct {
	Ticker     string              `json:"ticker"`
	Form       string              `json:"form"`
	Format     string              `json:"format"`
	Confidence string              `json:"confidence"`
	Sections   []string            `json:"sections"`
	Statements []BaselineStatement `json:"statements"`
}

// BaselineStatement is the expected content of one financial statement.
type BaselineStatement struct {
	Role  string         `json:"role"`
	Title string         `json:"title"`
	Cells []BaselineCell `json:"cells"`
}

// BaselineCell is one expected value: a concept in a period. A nil Value means
// the cell is expected to be absent (null) — and must score as null, never 0.
type BaselineCell struct {
	Concept string   `json:"concept"`
	Period  string   `json:"period"`
	Value   *float64 `json:"value"`
}

// Report is the harness's measurement across the corpus: per-filing scores, the
// corpus totals, and the per-confidence-bucket calibration that says whether a
// "high" label actually means high accuracy.
type Report struct {
	Filings []FilingScore
	Totals  Totals
	Buckets map[string]BucketStats
}

// FilingScore is one filing's measured accuracy.
type FilingScore struct {
	Name               string
	Ticker             string
	Format             string
	ExpectedConfidence string
	GotConfidence      string
	MatchedCells       int
	TotalCells         int
	MatchedSections    int
	TotalSections      int
}

// StatementAccuracy is the fraction of expected statement cells the parser got
// right (1.0 when there is nothing to score).
func (f FilingScore) StatementAccuracy() float64 { return ratio(f.MatchedCells, f.TotalCells) }

// SectionCoverage is the fraction of expected sections the parser bounded.
func (f FilingScore) SectionCoverage() float64 {
	return ratio(f.MatchedSections, f.TotalSections)
}

// Totals aggregates the corpus.
type Totals struct {
	MatchedCells    int
	TotalCells      int
	MatchedSections int
	TotalSections   int
}

// StatementAccuracy is the corpus-wide fraction of statement cells matched.
func (t Totals) StatementAccuracy() float64 { return ratio(t.MatchedCells, t.TotalCells) }

// SectionCoverage is the corpus-wide fraction of sections covered.
func (t Totals) SectionCoverage() float64 { return ratio(t.MatchedSections, t.TotalSections) }

// BucketStats is the calibration evidence for one confidence level: how many
// filings landed in the bucket and the mean statement accuracy across them. If
// "high" is calibrated, its accuracy should exceed "medium"'s.
type BucketStats struct {
	Filings      int
	MatchedCells int
	TotalCells   int
}

// Accuracy is the mean statement accuracy of the filings in this bucket.
func (b BucketStats) Accuracy() float64 { return ratio(b.MatchedCells, b.TotalCells) }

// Score runs the pipeline against every filing under corpusDir and scores it
// against its baseline.json. Each entry is a subdirectory holding the recorded
// EDGAR bytes (primary document, linkbases, submissions, index) and a
// baseline.json. It is fully hermetic: a fixture transport serves the recorded
// bytes, so no request reaches the network. The caller must have
// SEC_CLI_USER_AGENT set (edgar.NewClient requires it).
func Score(corpusDir string) (*Report, error) {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		return nil, fmt.Errorf("accuracy: read corpus: %w", err)
	}
	rep := &Report{Buckets: map[string]BucketStats{}}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(corpusDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "baseline.json")); err != nil {
			continue // not a corpus entry
		}
		fs, err := scoreFiling(e.Name(), dir)
		if err != nil {
			return nil, err
		}
		rep.Filings = append(rep.Filings, fs)

		rep.Totals.MatchedCells += fs.MatchedCells
		rep.Totals.TotalCells += fs.TotalCells
		rep.Totals.MatchedSections += fs.MatchedSections
		rep.Totals.TotalSections += fs.TotalSections

		b := rep.Buckets[fs.GotConfidence]
		b.Filings++
		b.MatchedCells += fs.MatchedCells
		b.TotalCells += fs.TotalCells
		rep.Buckets[fs.GotConfidence] = b
	}
	sort.Slice(rep.Filings, func(i, j int) bool { return rep.Filings[i].Name < rep.Filings[j].Name })
	return rep, nil
}

// scoreFiling runs and scores one corpus entry.
func scoreFiling(name, dir string) (FilingScore, error) {
	base, err := loadBaseline(dir)
	if err != nil {
		return FilingScore{}, err
	}

	client, err := edgar.NewClient(edgar.WithHTTPClient(&http.Client{Transport: fileTransport{dir: dir}}))
	if err != nil {
		return FilingScore{}, err
	}
	doc, err := pipeline.Run(context.Background(), pipeline.Options{
		Ticker: base.Ticker,
		Form:   base.Form,
		Client: client,
		// nil cache → no-op, so the harness writes no cache files and every run is fresh.
	})
	if err != nil {
		return FilingScore{}, fmt.Errorf("accuracy: %s: pipeline: %w", name, err)
	}

	fs := FilingScore{
		Name:               name,
		Ticker:             base.Ticker,
		Format:             base.Format,
		ExpectedConfidence: base.Confidence,
		GotConfidence:      doc.Metadata.Confidence.Level,
	}
	scoreStatements(&fs, base, doc)
	scoreSections(&fs, base, doc)
	return fs, nil
}

// scoreStatements counts how many expected cells the projection reproduced. A
// statement the parser failed to produce scores all its cells as missed.
func scoreStatements(fs *FilingScore, base Baseline, doc *model.Document) {
	for _, bs := range base.Statements {
		fs.TotalCells += len(bs.Cells)
		tbl, ok := findStatement(doc.Statements, bs)
		if !ok {
			continue
		}
		for _, bc := range bs.Cells {
			if cellMatch(bc.Value, lookupCell(tbl, bc.Concept, bc.Period)) {
				fs.MatchedCells++
			}
		}
	}
}

// scoreSections counts how many expected section item ids the partition bounded.
func scoreSections(fs *FilingScore, base Baseline, doc *model.Document) {
	got := make(map[string]bool, len(doc.Sections))
	for _, s := range doc.Sections {
		got[s.Item] = true
	}
	fs.TotalSections = len(base.Sections)
	for _, item := range base.Sections {
		if got[item] {
			fs.MatchedSections++
		}
	}
}

// findStatement matches a baseline statement to a produced table by role URI
// (stable across filings) or, failing that, by title.
func findStatement(tables []model.Table, bs BaselineStatement) (model.Table, bool) {
	for _, t := range tables {
		if bs.Role != "" && t.RoleURI == bs.Role {
			return t, true
		}
	}
	for _, t := range tables {
		if bs.Title != "" && strings.EqualFold(t.Title, bs.Title) {
			return t, true
		}
	}
	return model.Table{}, false
}

// lookupCell returns the produced value for a concept in a period (by column
// label), or nil when the row or column is absent — a missing value is null.
func lookupCell(t model.Table, concept, period string) *float64 {
	col := -1
	for i, c := range t.Columns {
		if c.Label == period {
			col = i
			break
		}
	}
	if col < 0 {
		return nil
	}
	for _, r := range t.Rows {
		if r.Concept == concept {
			if col < len(r.Values) {
				return r.Values[col]
			}
			return nil
		}
	}
	return nil
}

// cellMatch reports whether a produced value matches the expected one. Two nulls
// match; a null against a value does not (a dropped cell is a miss, and a
// phantom value where none was expected is a miss). Present values match within
// a small absolute-plus-relative tolerance.
func cellMatch(expected, actual *float64) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	// A small relative tolerance absorbs rounding/scale arithmetic; a sub-dollar
	// floor keeps small expected values (including 0) from matching anything.
	tol := math.Max(0.5, 1e-6*math.Abs(*expected))
	return math.Abs(*expected-*actual) <= tol
}

// loadBaseline reads and parses an entry's baseline.json.
func loadBaseline(dir string) (Baseline, error) {
	b, err := os.ReadFile(filepath.Join(dir, "baseline.json"))
	if err != nil {
		return Baseline{}, fmt.Errorf("accuracy: read baseline: %w", err)
	}
	var base Baseline
	if err := json.Unmarshal(b, &base); err != nil {
		return Baseline{}, fmt.Errorf("accuracy: parse baseline %s: %w", dir, err)
	}
	return base, nil
}

// ratio is matched/total, or 1.0 when there is nothing to score (an empty
// expectation is vacuously met).
func ratio(matched, total int) float64 {
	if total == 0 {
		return 1
	}
	return float64(matched) / float64(total)
}

// fileTransport serves a corpus entry's recorded bytes by routing each EDGAR URL
// to a file in the entry directory: company_tickers.json and the submissions
// document are matched by their URL shape, everything else (primary document,
// index.json, linkbases) by the URL's base name. A missing file answers 404, so
// a fetch the corpus forgot to record fails loudly rather than reaching the net.
type fileTransport struct{ dir string }

func (t fileTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	b, err := os.ReadFile(filepath.Join(t.dir, routeName(r.URL)))
	status := http.StatusOK
	if err != nil {
		status, b = http.StatusNotFound, []byte("not found: "+r.URL.String())
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(b)),
		Header:     make(http.Header),
	}, nil
}

// routeName maps an EDGAR URL to the corpus file that answers it.
func routeName(u *url.URL) string {
	s := u.String()
	switch {
	case strings.Contains(s, "company_tickers.json"):
		return "company_tickers.json"
	case strings.Contains(s, "/submissions/"):
		return "submissions.json"
	default:
		return path.Base(u.Path)
	}
}

// String renders the report as a plain-text summary: a per-filing table, the
// corpus totals, and the per-bucket confidence calibration.
func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-10s %-7s %-14s %-6s→%-6s %10s %10s\n",
		"filing", "ticker", "format", "exp", "got", "stmt-acc", "sect-cov")
	b.WriteString(strings.Repeat("-", 76) + "\n")
	for _, f := range r.Filings {
		fmt.Fprintf(&b, "%-10s %-7s %-14s %-6s→%-6s %9.1f%% %9.1f%%\n",
			f.Name, f.Ticker, f.Format, f.ExpectedConfidence, f.GotConfidence,
			100*f.StatementAccuracy(), 100*f.SectionCoverage())
	}
	b.WriteString(strings.Repeat("-", 76) + "\n")
	fmt.Fprintf(&b, "overall: statement accuracy %.1f%% (%d/%d), section coverage %.1f%% (%d/%d)\n",
		100*r.Totals.StatementAccuracy(), r.Totals.MatchedCells, r.Totals.TotalCells,
		100*r.Totals.SectionCoverage(), r.Totals.MatchedSections, r.Totals.TotalSections)
	b.WriteString("\nconfidence calibration (accuracy within each bucket):\n")
	for _, level := range []string{"high", "medium", "low"} {
		if bs, ok := r.Buckets[level]; ok {
			fmt.Fprintf(&b, "  %-6s %d filing(s): %.1f%% cell accuracy\n",
				level, bs.Filings, 100*bs.Accuracy())
		}
	}
	return b.String()
}
