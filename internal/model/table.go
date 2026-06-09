// Package model is the normalized, source-agnostic output contract for a filing:
// the Document and the value types every other package produces, renders,
// caches, diffs, or scores. It is deliberately flat and JSON-first — pure data
// plus the custom marshaling the schema requires — and depends only on the
// standard library, so every other package can import it without a cycle. The
// parser (internal/ixbrl) produces these types; it does not define them.
package model

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the consumer-facing JSON output contract (semver: a breaking
// change to the JSON shape bumps the major). ParserVersion is the extraction
// code's identity; the cache keys parsed output on it so a parser fix
// invalidates cached results without re-fetching bytes. They are distinct on
// purpose — see DESIGN.md §2.4.
const (
	SchemaVersion = "1.0.0"
	ParserVersion = "0.1.0"
)

// dateLayout renders period dates as plain YYYY-MM-DD, the schema's date
// convention, rather than Go's default RFC3339.
const dateLayout = "2006-01-02"

// RowType classifies a statement row. A total caps a section, a subtotal is a
// parent line that also carries a value, and data is a leaf line item.
type RowType string

// Row types.
const (
	RowData     RowType = "data"
	RowSubtotal RowType = "subtotal"
	RowTotal    RowType = "total"
)

// Table is a statement or narrative table projected onto rows × columns, either
// from the iXBRL fact stream or by spatial layout. The JSON tags are the output
// schema.
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

// MarshalJSON renders period dates as plain YYYY-MM-DD, matching the output
// schema rather than Go's default RFC3339 time encoding.
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

// UnmarshalJSON is the inverse of MarshalJSON: it parses the YYYY-MM-DD period
// dates the schema uses (not RFC3339), so a Document round-trips through JSON.
// The parsed cache (Phase 10) depends on this — it stores canonical JSON and
// reloads it into a *Document.
func (c *Column) UnmarshalJSON(data []byte) error {
	var in struct {
		Label       string `json:"label"`
		ContextRef  string `json:"context_ref"`
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
		Instant     bool   `json:"instant"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	c.Label = in.Label
	c.ContextRef = in.ContextRef
	c.Instant = in.Instant
	if in.PeriodStart != "" {
		t, err := time.Parse(dateLayout, in.PeriodStart)
		if err != nil {
			return err
		}
		c.PeriodStart = t
	}
	if in.PeriodEnd != "" {
		t, err := time.Parse(dateLayout, in.PeriodEnd)
		if err != nil {
			return err
		}
		c.PeriodEnd = t
	}
	return nil
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

// Confidence is the calibrated trust signal for an extracted artifact. Level is
// the bucket every artifact carries; the rate fields describe how completely a
// projected table resolved and are left zero for non-table partitions.
type Confidence struct {
	Level             string  `json:"level"`
	RowMatchRate      float64 `json:"row_match_rate"`
	CellResolvedRate  float64 `json:"cell_resolved_rate"`
	UntaggedCellCount int     `json:"untagged_cell_count"`
}

// Source records which extractor produced an artifact and the parser version.
type Source struct {
	Extractor     string `json:"extractor"`
	ParserVersion string `json:"parser_version"`
}
