package ixbrl

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// schemaVersion and parserVersion stamp projected output. schemaVersion tracks
// the JSON output contract (semver, consumer-facing); parserVersion tracks the
// extraction code so the cache (Phase 10) can invalidate parsed output on a
// parser fix without re-fetching. Both are placeholders here and move to
// internal/model in Phase 9.
const (
	schemaVersion  = "1.0.0"
	parserVersion  = "0.1.0"
	extractorFacts = "ixbrl.facts"

	dateLayout = "2006-01-02"
)

// RowType classifies a statement row. A total caps a section, a subtotal is a
// parent line that also carries a value, and data is a leaf line item.
type RowType string

const (
	RowData     RowType = "data"
	RowSubtotal RowType = "subtotal"
	RowTotal    RowType = "total"
)

// Table is a financial statement projected from the fact stream onto rows ×
// columns. It is a placeholder for the internal/model.Table that Phase 9 will
// promote; the JSON tags already track the design's output schema.
type Table struct {
	SchemaVersion string            `json:"schema_version"`
	Title         string            `json:"title,omitempty"`
	RoleURI       string            `json:"role_uri,omitempty"`
	Columns       []Column          `json:"columns"`
	Rows          []Row             `json:"rows"`
	Footnotes     map[string]string `json:"footnotes,omitempty"`
	Confidence    Confidence        `json:"confidence"`
	Source        Source            `json:"source"`
}

// Column is one reporting period: the contextRef whose facts fill the column and
// the period it covers.
type Column struct {
	Label       string
	ContextRef  string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Instant     bool
}

// MarshalJSON renders period dates as plain YYYY-MM-DD, matching the design's
// output schema rather than Go's default RFC3339 time encoding.
func (c Column) MarshalJSON() ([]byte, error) {
	out := struct {
		Label       string `json:"label"`
		ContextRef  string `json:"context_ref,omitempty"`
		PeriodStart string `json:"period_start,omitempty"`
		PeriodEnd   string `json:"period_end,omitempty"`
		Instant     bool   `json:"instant,omitempty"`
	}{
		Label:      c.Label,
		ContextRef: c.ContextRef,
		Instant:    c.Instant,
	}
	// Layout-extracted columns have no reporting period; omit the date fields
	// rather than render a zero time.
	if !c.PeriodStart.IsZero() {
		out.PeriodStart = c.PeriodStart.Format(dateLayout)
	}
	if !c.PeriodEnd.IsZero() {
		out.PeriodEnd = c.PeriodEnd.Format(dateLayout)
	}
	return json.Marshal(out)
}

// Row is one statement line: a concept, its filing label, and its value in each
// column. A nil entry in Values is a cell with no matching fact — null, never
// zero.
type Row struct {
	Label   string     `json:"label"`
	Concept string     `json:"concept,omitempty"`
	Type    RowType    `json:"type"`
	Depth   int        `json:"depth,omitempty"`
	Values  []*float64 `json:"values"`
}

// Confidence is the calibrated trust signal for a projected table, derived from
// how completely the fact stream filled the linkbase's rows and columns.
type Confidence struct {
	Level             string  `json:"level"`
	RowMatchRate      float64 `json:"row_match_rate"`
	CellResolvedRate  float64 `json:"cell_resolved_rate"`
	UntaggedCellCount int     `json:"untagged_cell_count"`
}

// Source records which extractor produced the table and the parser version.
type Source struct {
	Extractor     string `json:"extractor"`
	ParserVersion string `json:"parser_version"`
}

// factKey indexes a fact by its concept and reporting context.
type factKey struct{ concept, ctx string }

// instantKey indexes an instant fact by its concept and ISO date.
type instantKey struct{ concept, date string }

// ProjectTable projects the fact stream onto the rows and columns of one
// statement role. Rows are the role's non-structural concepts in presentation
// order; columns are the primary (consolidated, unsegmented) contexts that carry
// facts for those concepts, newest period first. Each cell is the fact matching
// (concept, contextRef), or null when none exists.
func ProjectTable(role LinkbaseRole, facts []Fact, contexts map[string]Context) *Table {
	// Index consolidated numeric facts by concept and context. Segment-scoped
	// facts (per-product, per-class) share concepts with the consolidated line
	// and are excluded so a row reads its consolidated value, not a breakdown.
	values := make(map[factKey]float64)
	// instants indexes primary instant facts by concept and date, so opening and
	// closing balances (periodStart/EndLabel rows) can be placed into a duration
	// column at its period boundary.
	instants := make(map[instantKey]float64)
	for _, f := range facts {
		if !f.Numeric || f.Unparsed {
			continue
		}
		ctx, ok := contexts[f.ContextRef]
		if !ok || !ctx.IsPrimary() {
			continue
		}
		values[factKey{f.Concept, f.ContextRef}] = f.Value
		if ctx.IsInstant() {
			instants[instantKey{f.Concept, ctx.End.Format(dateLayout)}] = f.Value
		}
	}

	rowConcepts := make([]RoleConcept, 0, len(role.Concepts))
	for _, c := range role.Concepts {
		if !c.Structural {
			rowConcepts = append(rowConcepts, c)
		}
	}

	columns := projectColumns(rowConcepts, values, contexts)

	rows := make([]Row, 0, len(rowConcepts))
	for _, c := range rowConcepts {
		vals := make([]*float64, len(columns))
		for i, col := range columns {
			vals[i] = cellValue(c, col, values, instants)
		}
		rows = append(rows, Row{
			Label:   c.Label,
			Concept: c.Name,
			Type:    classifyRow(c),
			Depth:   c.Depth,
			Values:  vals,
		})
	}

	return &Table{
		SchemaVersion: schemaVersion,
		Title:         role.Title,
		RoleURI:       role.RoleURI,
		Columns:       columns,
		Rows:          rows,
		Confidence:    computeConfidence(rows, len(columns)),
		Source:        Source{Extractor: extractorFacts, ParserVersion: parserVersion},
	}
}

// columnCoverageFloor is the share of the best-covered period a context must
// also reach to qualify as a column. It separates the periods a statement
// actually presents (covered by most rows) from incidental ones — e.g. a single
// balance-sheet line whose value is also tagged at extra period-ends by the cash
// flow reconciliation.
const columnCoverageFloor = 0.5

// cellValue resolves one row's value in one column. Most rows read the fact at
// the column's own context. A periodStart/End row instead reads an instant fact
// at the column's opening or closing date — the convention for cash flow opening
// and closing balances shown inside a duration column. The opening balance is
// the instant on the day before the period begins (the prior period's close).
func cellValue(c RoleConcept, col Column, values map[factKey]float64, instants map[instantKey]float64) *float64 {
	switch c.PreferredLabel {
	case rolePeriodEndLabel:
		return lookupInstant(instants, c.Name, col.PeriodEnd)
	case rolePeriodStartLabel:
		if v := lookupInstant(instants, c.Name, col.PeriodStart.AddDate(0, 0, -1)); v != nil {
			return v
		}
		return lookupInstant(instants, c.Name, col.PeriodStart)
	default:
		if v, ok := values[factKey{c.Name, col.ContextRef}]; ok {
			return &v
		}
		return nil
	}
}

func lookupInstant(instants map[instantKey]float64, concept string, date time.Time) *float64 {
	if v, ok := instants[instantKey{concept, date.Format(dateLayout)}]; ok {
		return &v
	}
	return nil
}

// projectColumns selects the periods the statement presents and orders them
// newest first (by end date, then start date). A primary context becomes a
// column only if it carries facts for at least columnCoverageFloor of the rows
// the best-covered context does, which drops periods that only a stray row tags.
func projectColumns(rowConcepts []RoleConcept, values map[factKey]float64, contexts map[string]Context) []Column {
	rowSet := make(map[string]bool, len(rowConcepts))
	for _, c := range rowConcepts {
		rowSet[c.Name] = true
	}

	coverage := make(map[string]int)
	for k := range values {
		if rowSet[k.concept] {
			coverage[k.ctx]++
		}
	}
	maxCov := 0
	for _, n := range coverage {
		if n > maxCov {
			maxCov = n
		}
	}

	var refs []string
	for ref, n := range coverage {
		if float64(n) >= float64(maxCov)*columnCoverageFloor {
			refs = append(refs, ref)
		}
	}

	cols := make([]Column, 0, len(refs))
	for _, ref := range refs {
		ctx := contexts[ref]
		cols = append(cols, Column{
			Label:       columnLabel(ctx),
			ContextRef:  ref,
			PeriodStart: ctx.Start,
			PeriodEnd:   ctx.End,
			Instant:     ctx.IsInstant(),
		})
	}
	sort.SliceStable(cols, func(i, j int) bool {
		if !cols[i].PeriodEnd.Equal(cols[j].PeriodEnd) {
			return cols[i].PeriodEnd.After(cols[j].PeriodEnd)
		}
		return cols[i].PeriodStart.After(cols[j].PeriodStart)
	})
	return cols
}

// columnLabel names a column by the calendar year of its period end — the
// convention filings use for comparative statement headers.
func columnLabel(ctx Context) string {
	return ctx.End.Format("2006")
}

// classifyRow assigns a row type. The presentation arc's totalLabel role is the
// authoritative total signal (totals such as Gross profit are leaf concepts, so
// "has children" alone misses them); a label beginning with "Total" is a
// secondary signal. A non-structural parent that is not a total is a subtotal;
// everything else is a leaf data row.
func classifyRow(c RoleConcept) RowType {
	if c.PreferredLabel == roleTotalLabel || strings.HasPrefix(strings.ToLower(c.Label), "total") {
		return RowTotal
	}
	if c.HasChildren {
		return RowSubtotal
	}
	return RowData
}

// computeConfidence buckets trust from two completeness rates: the fraction of
// cells filled by a fact, and the fraction of rows with every column filled.
func computeConfidence(rows []Row, nCols int) Confidence {
	totalCells := len(rows) * nCols
	resolved := 0
	fullRows := 0
	for _, r := range rows {
		rowFull := nCols > 0
		for _, v := range r.Values {
			if v != nil {
				resolved++
			} else {
				rowFull = false
			}
		}
		if rowFull {
			fullRows++
		}
	}

	var cellRate, rowRate float64
	if totalCells > 0 {
		cellRate = float64(resolved) / float64(totalCells)
	}
	if len(rows) > 0 {
		rowRate = float64(fullRows) / float64(len(rows))
	}

	return Confidence{
		Level:             confidenceLevel(rowRate, cellRate),
		RowMatchRate:      rowRate,
		CellResolvedRate:  cellRate,
		UntaggedCellCount: totalCells - resolved,
	}
}

// confidenceLevel applies the design's bucketing thresholds. For the fact-stream
// path every placed cell already has a resolved contextRef, so the high bucket's
// contextRef-coverage clause is satisfied by construction.
func confidenceLevel(rowRate, cellRate float64) string {
	switch {
	case rowRate >= 0.95 && cellRate >= 0.95:
		return "high"
	case rowRate >= 0.85 || (cellRate >= 0.80 && cellRate <= 0.95):
		return "medium"
	default:
		return "low"
	}
}
