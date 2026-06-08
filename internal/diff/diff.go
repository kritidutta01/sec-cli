// Package diff compares two normalized documents and emits a structured change
// set: numeric statement deltas matched by concept (labels drift, concepts are
// stable) and narrative changes matched by item id and diffed at paragraph
// granularity. It is a pure function over two *model.Document values — it never
// fetches or re-parses — so a diff is just another schema-stamped artifact the
// same way a document is. It depends only on internal/model and the standard
// library.
package diff

import (
	"errors"
	"strings"

	"github.com/kritidutta01/sec-cli/internal/model"
)

// ChangeKind classifies a single change. A row or paragraph is added when it
// appears only in the current filing, removed when only in the previous,
// changed when present in both with differing values, and unchanged when
// present in both with identical values.
type ChangeKind string

// Change kinds.
const (
	Added     ChangeKind = "added"
	Removed   ChangeKind = "removed"
	Changed   ChangeKind = "changed"
	Unchanged ChangeKind = "unchanged"
)

// ChangeSet is the structured result of diffing two documents: what filings
// were compared, the per-statement numeric deltas, and the per-section
// narrative changes. It carries its own SchemaVersion so a rendered diff is a
// versioned artifact like a Document.
type ChangeSet struct {
	SchemaVersion string          `json:"schema_version"`
	Prev          FilingRef       `json:"prev"`
	Curr          FilingRef       `json:"curr"`
	Statements    []StatementDiff `json:"statements"`
	Sections      []SectionDiff   `json:"sections"`
}

// FilingRef identifies one side of the comparison — enough to label the report
// without carrying the whole document.
type FilingRef struct {
	Company    string `json:"company,omitempty"`
	Ticker     string `json:"ticker,omitempty"`
	Form       string `json:"form,omitempty"`
	Accession  string `json:"accession,omitempty"`
	FilingDate string `json:"filing_date,omitempty"`
	PeriodEnd  string `json:"period_end,omitempty"`
}

// StatementDiff is the change to one financial statement, matched between the
// two filings by role URI (or title when no role). Periods are the reporting
// periods present in both filings, in current-filing order; every RowDelta's
// cells align to them.
type StatementDiff struct {
	Title   string     `json:"title,omitempty"`
	RoleURI string     `json:"role_uri,omitempty"`
	Periods []string   `json:"periods"`
	Rows    []RowDelta `json:"rows"`
}

// RowDelta is one statement line matched across the two filings by concept (so
// a relabeled line still matches). Status summarizes the row; Cells carries the
// per-period numbers.
type RowDelta struct {
	Concept string      `json:"concept,omitempty"`
	Label   string      `json:"label"`
	Status  ChangeKind  `json:"status"`
	Cells   []CellDelta `json:"cells"`
}

// CellDelta is the change to one concept in one overlapping period: the two
// values and their absolute and percentage change. Any field is null when it
// cannot be computed — a missing value is null (never 0), and Pct is null when
// the previous value is absent or zero. Pct is a percentage (e.g. 11.5 means
// +11.5%), not a fraction.
type CellDelta struct {
	Period string   `json:"period"`
	Prev   *float64 `json:"prev"`
	Curr   *float64 `json:"curr"`
	Abs    *float64 `json:"abs"`
	Pct    *float64 `json:"pct"`
}

// SectionDiff is the narrative change to one section, matched between filings by
// item id. Paragraphs lists the added and removed paragraphs in merge order; a
// reworded paragraph surfaces as a removed/added pair.
type SectionDiff struct {
	Item       string            `json:"item,omitempty"`
	Title      string            `json:"title,omitempty"`
	Paragraphs []ParagraphChange `json:"paragraphs,omitempty"`
}

// ParagraphChange is one paragraph added or removed from a section's free text.
type ParagraphChange struct {
	Kind ChangeKind `json:"kind"`
	Text string     `json:"text"`
}

// ErrNilDocument is returned when either side of the diff is nil.
var ErrNilDocument = errors.New("diff: nil document")

// Diff compares two assembled documents and returns the change set. prev is the
// earlier filing, curr the later one. It never fetches or re-parses; it only
// reads the two documents.
func Diff(prev, curr *model.Document) (*ChangeSet, error) {
	if prev == nil || curr == nil {
		return nil, ErrNilDocument
	}
	return &ChangeSet{
		SchemaVersion: model.SchemaVersion,
		Prev:          filingRef(prev),
		Curr:          filingRef(curr),
		Statements:    diffStatements(prev.Statements, curr.Statements),
		Sections:      diffSections(prev.Sections, curr.Sections),
	}, nil
}

func filingRef(d *model.Document) FilingRef {
	m := d.Metadata
	return FilingRef{
		Company:    m.Company,
		Ticker:     m.Ticker,
		Form:       m.Form,
		Accession:  m.Accession,
		FilingDate: m.FilingDate,
		PeriodEnd:  m.PeriodEnd,
	}
}

// diffStatements matches statements between the two filings by role URI (or
// title when there is no role) and diffs each matched pair. Statements present
// in only one filing are skipped — the report compares like with like.
func diffStatements(prev, curr []model.Table) []StatementDiff {
	prevByKey := make(map[string]*model.Table, len(prev))
	for i := range prev {
		prevByKey[stmtKey(prev[i])] = &prev[i]
	}
	var out []StatementDiff
	for i := range curr {
		if p, ok := prevByKey[stmtKey(curr[i])]; ok {
			out = append(out, diffStatement(*p, curr[i]))
		}
	}
	return out
}

// stmtKey identifies a statement for matching: its role URI is stable across
// filings, so prefer it; fall back to a normalized title.
func stmtKey(t model.Table) string {
	if t.RoleURI != "" {
		return "r:" + t.RoleURI
	}
	return "t:" + strings.ToLower(strings.TrimSpace(t.Title))
}

// diffStatement diffs one matched statement pair: it finds the periods both
// filings report, matches rows by concept, and produces a RowDelta per concept
// (current-order first, then rows that only the previous filing had).
func diffStatement(prev, curr model.Table) StatementDiff {
	periods := overlappingPeriods(prev, curr)
	labels := make([]string, len(periods))
	for i, p := range periods {
		labels[i] = p.label
	}
	sd := StatementDiff{Title: curr.Title, RoleURI: curr.RoleURI, Periods: labels}

	prevByKey := make(map[string]*model.Row, len(prev.Rows))
	var prevOrder []string
	for i := range prev.Rows {
		k := rowKey(prev.Rows[i])
		if _, dup := prevByKey[k]; !dup {
			prevOrder = append(prevOrder, k)
		}
		prevByKey[k] = &prev.Rows[i]
	}

	seen := make(map[string]bool, len(curr.Rows))
	for i := range curr.Rows {
		cr := &curr.Rows[i]
		k := rowKey(*cr)
		seen[k] = true
		pr := prevByKey[k]
		cells := buildCells(pr, cr, periods)
		sd.Rows = append(sd.Rows, RowDelta{
			Concept: cr.Concept,
			Label:   cr.Label,
			Status:  rowStatus(pr, cr, cells),
			Cells:   cells,
		})
	}
	// Rows the previous filing had and the current one dropped.
	for _, k := range prevOrder {
		if seen[k] {
			continue
		}
		pr := prevByKey[k]
		sd.Rows = append(sd.Rows, RowDelta{
			Concept: pr.Concept,
			Label:   pr.Label,
			Status:  Removed,
			Cells:   buildCells(pr, nil, periods),
		})
	}
	return sd
}

// rowKey identifies a statement line for matching: the concept is stable across
// filings even when the label is reworded, so prefer it; fall back to a
// normalized label only when a row carries no concept.
func rowKey(r model.Row) string {
	if r.Concept != "" {
		return "c:" + r.Concept
	}
	return "l:" + strings.ToLower(strings.TrimSpace(r.Label))
}

// periodPair links a period reported in both filings to its column index in
// each, so a row's value can be read from the right column on each side.
type periodPair struct {
	label   string
	prevIdx int
	currIdx int
}

// overlappingPeriods returns the reporting periods present in both statements,
// in current-filing column order. Periods are matched by their dates (instant
// vs duration, start, end); when a column carries no dates — as a hand-built or
// layout-extracted table may — it falls back to the column label.
func overlappingPeriods(prev, curr model.Table) []periodPair {
	prevByKey := make(map[string]int, len(prev.Columns))
	for i, c := range prev.Columns {
		prevByKey[periodKey(c)] = i
	}
	var out []periodPair
	for j, c := range curr.Columns {
		if i, ok := prevByKey[periodKey(c)]; ok {
			out = append(out, periodPair{label: c.Label, prevIdx: i, currIdx: j})
		}
	}
	return out
}

// periodKey identifies a reporting period for matching across filings.
func periodKey(c model.Column) string {
	if !c.PeriodStart.IsZero() || !c.PeriodEnd.IsZero() {
		const layout = "2006-01-02"
		kind := "d:"
		if c.Instant {
			kind = "i:"
		}
		return kind + c.PeriodStart.Format(layout) + "_" + c.PeriodEnd.Format(layout)
	}
	return "l:" + c.Label
}

// buildCells computes the per-period change for one matched concept. Either row
// may be nil (an added or removed concept), in which case that side's values are
// null.
func buildCells(prevRow, currRow *model.Row, periods []periodPair) []CellDelta {
	cells := make([]CellDelta, 0, len(periods))
	for _, p := range periods {
		var pv, cv *float64
		if prevRow != nil {
			pv = valueAt(prevRow, p.prevIdx)
		}
		if currRow != nil {
			cv = valueAt(currRow, p.currIdx)
		}
		cell := CellDelta{Period: p.label, Prev: pv, Curr: cv}
		if pv != nil && cv != nil {
			d := *cv - *pv
			cell.Abs = &d
			// Percent change is undefined against a zero base; leave it null.
			if *pv != 0 {
				pct := d / *pv * 100
				cell.Pct = &pct
			}
		}
		cells = append(cells, cell)
	}
	return cells
}

// valueAt returns the cell value at a column index, or nil when the row is
// shorter than the index (a missing value is null, never 0).
func valueAt(r *model.Row, idx int) *float64 {
	if idx < 0 || idx >= len(r.Values) {
		return nil
	}
	return r.Values[idx]
}

// rowStatus summarizes a matched row: added/removed when present on only one
// side, changed when any overlapping period differs, unchanged otherwise.
func rowStatus(prevRow, currRow *model.Row, cells []CellDelta) ChangeKind {
	switch {
	case prevRow == nil:
		return Added
	case currRow == nil:
		return Removed
	}
	for _, c := range cells {
		if !floatEq(c.Prev, c.Curr) {
			return Changed
		}
	}
	return Unchanged
}

// floatEq reports whether two optional values are equal, treating two nulls as
// equal and a null-vs-value pair as different.
func floatEq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// diffSections matches sections by item id and diffs each matched pair's free
// text at paragraph granularity. Sections with no item id are skipped (they
// cannot be matched); a section dropped entirely from the current filing reports
// all its paragraphs as removed.
func diffSections(prev, curr []model.Section) []SectionDiff {
	prevByItem := make(map[string]*model.Section, len(prev))
	for i := range prev {
		if prev[i].Item != "" {
			prevByItem[prev[i].Item] = &prev[i]
		}
	}

	seen := make(map[string]bool, len(curr))
	var out []SectionDiff
	for i := range curr {
		c := &curr[i]
		if c.Item == "" {
			continue
		}
		seen[c.Item] = true
		var prevPars []string
		if p, ok := prevByItem[c.Item]; ok {
			prevPars = paragraphs(p.Text)
		}
		changes := diffParagraphs(prevPars, paragraphs(c.Text))
		if len(changes) > 0 {
			out = append(out, SectionDiff{Item: c.Item, Title: c.Title, Paragraphs: changes})
		}
	}
	// Sections the previous filing had and the current one dropped.
	for i := range prev {
		p := &prev[i]
		if p.Item == "" || seen[p.Item] {
			continue
		}
		changes := diffParagraphs(paragraphs(p.Text), nil)
		if len(changes) > 0 {
			out = append(out, SectionDiff{Item: p.Item, Title: p.Title, Paragraphs: changes})
		}
	}
	return out
}

// paragraphs splits a section's free text into trimmed, non-empty paragraphs.
// Free text separates paragraphs with a blank line (see internal/sections), so
// the unit of comparison is the paragraph, not the character.
func paragraphs(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// diffParagraphs reports the paragraphs that differ between two texts as an
// ordered list of added/removed changes, using a longest-common-subsequence
// walk so unchanged paragraphs anchor the alignment and only real edits surface.
func diffParagraphs(a, b []string) []ParagraphChange {
	n, m := len(a), len(b)
	// lcs[i][j] = length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var out []ParagraphChange
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, ParagraphChange{Kind: Removed, Text: a[i]})
			i++
		default:
			out = append(out, ParagraphChange{Kind: Added, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, ParagraphChange{Kind: Removed, Text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, ParagraphChange{Kind: Added, Text: b[j]})
	}
	return out
}
