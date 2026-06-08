package diff

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kritidutta01/sec-cli/internal/model"
)

var update = flag.Bool("update", false, "update golden files")

func f(v float64) *float64 { return &v }

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// col builds a duration reporting period from its end-of-year date.
func col(label, end string) model.Column {
	e := date(end)
	return model.Column{Label: label, PeriodStart: date(end[:4] + "-01-01"), PeriodEnd: e}
}

// prevDoc and currDoc are two filings of the same company that share the FY2023
// period (prev reports FY2022/FY2023, curr reports FY2023/FY2024). The shared
// period is what the diff aligns on. The income statement exercises every case:
// a relabeled-but-same-concept row, an unchanged row, a zero-base row, an added
// row, and a removed row. The Risk Factors section has one paragraph swapped.
func prevDoc() *model.Document {
	return &model.Document{
		Metadata: model.Metadata{
			Company: "Acme Corp", Ticker: "ACME", Form: "10-K",
			Accession: "0000000000-23-000001", FilingDate: "2023-11-01", PeriodEnd: "2023-12-31",
		},
		Sections: []model.Section{{
			Item: "1A", Title: "Risk Factors", Kind: model.KindNarrative,
			Text: "Markets are competitive.\n\nSupply chains may be disrupted.\n\nWe depend on a legacy product line.",
		}},
		Statements: []model.Table{{
			Title:   "Income Statement",
			RoleURI: "http://acme.com/role/Income",
			Columns: []model.Column{col("FY2022", "2022-12-31"), col("FY2023", "2023-12-31")},
			Rows: []model.Row{
				{Label: "Revenue", Concept: "us-gaap:Revenues", Type: model.RowData, Values: []*float64{f(800), f(900)}},
				{Label: "Cost of sales", Concept: "us-gaap:CostOfRevenue", Type: model.RowData, Values: []*float64{f(350), f(400)}},
				{Label: "Marketing", Concept: "us-gaap:MarketingExpense", Type: model.RowData, Values: []*float64{f(0), f(0)}},
				{Label: "Legacy fee", Concept: "us-gaap:LegacyFee", Type: model.RowData, Values: []*float64{f(40), f(50)}},
			},
		}},
	}
}

func currDoc() *model.Document {
	return &model.Document{
		Metadata: model.Metadata{
			Company: "Acme Corp", Ticker: "ACME", Form: "10-K",
			Accession: "0000000000-24-000001", FilingDate: "2024-11-01", PeriodEnd: "2024-12-31",
		},
		Sections: []model.Section{{
			Item: "1A", Title: "Risk Factors", Kind: model.KindNarrative,
			Text: "Markets are competitive.\n\nSupply chains may be disrupted.\n\nNew regulation may raise our costs.",
		}},
		Statements: []model.Table{{
			Title:   "Income Statement",
			RoleURI: "http://acme.com/role/Income",
			Columns: []model.Column{col("FY2023", "2023-12-31"), col("FY2024", "2024-12-31")},
			Rows: []model.Row{
				// Relabeled but same concept — must still match the prev "Revenue" row.
				{Label: "Total revenue", Concept: "us-gaap:Revenues", Type: model.RowData, Values: []*float64{f(1000), f(1100)}},
				{Label: "Cost of sales", Concept: "us-gaap:CostOfRevenue", Type: model.RowData, Values: []*float64{f(400), f(430)}},
				{Label: "Marketing", Concept: "us-gaap:MarketingExpense", Type: model.RowData, Values: []*float64{f(25), f(30)}},
				// New concept, absent from prev → added.
				{Label: "New segment", Concept: "us-gaap:NewSegmentRevenue", Type: model.RowData, Values: []*float64{f(75), f(90)}},
			},
		}},
	}
}

// rowByConcept finds a diffed row by concept within the first statement.
func rowByConcept(t *testing.T, cs *ChangeSet, concept string) RowDelta {
	t.Helper()
	require.Len(t, cs.Statements, 1)
	for _, r := range cs.Statements[0].Rows {
		if r.Concept == concept {
			return r
		}
	}
	t.Fatalf("no row for concept %q", concept)
	return RowDelta{}
}

// TestDiff_OverlapAndMatching asserts the diff aligns on the shared FY2023
// period, matches rows by concept across a relabel, and computes the delta.
func TestDiff_OverlapAndMatching(t *testing.T) {
	cs, err := Diff(prevDoc(), currDoc())
	require.NoError(t, err)

	require.Len(t, cs.Statements, 1)
	require.Equal(t, []string{"FY2023"}, cs.Statements[0].Periods, "only the shared period is compared")

	// Revenue: prev FY2023 = 900 (column index 1), curr FY2023 = 1000 (column
	// index 0). Matched by concept despite the "Revenue" → "Total revenue" relabel.
	rev := rowByConcept(t, cs, "us-gaap:Revenues")
	require.Equal(t, Changed, rev.Status)
	require.Equal(t, "Total revenue", rev.Label, "the current label is reported")
	require.Len(t, rev.Cells, 1)
	c := rev.Cells[0]
	require.Equal(t, 900.0, *c.Prev)
	require.Equal(t, 1000.0, *c.Curr)
	require.Equal(t, 100.0, *c.Abs)
	require.InDelta(t, 11.111, *c.Pct, 0.001)
}

// TestDiff_AddedAndRemoved asserts a concept only in one filing is reported as
// added or removed.
func TestDiff_AddedAndRemoved(t *testing.T) {
	cs, err := Diff(prevDoc(), currDoc())
	require.NoError(t, err)

	added := rowByConcept(t, cs, "us-gaap:NewSegmentRevenue")
	require.Equal(t, Added, added.Status)
	require.Nil(t, added.Cells[0].Prev, "added row has no previous value")
	require.Equal(t, 75.0, *added.Cells[0].Curr)
	require.Nil(t, added.Cells[0].Abs, "no delta without a previous value")

	removed := rowByConcept(t, cs, "us-gaap:LegacyFee")
	require.Equal(t, Removed, removed.Status)
	require.Equal(t, 50.0, *removed.Cells[0].Prev)
	require.Nil(t, removed.Cells[0].Curr, "removed row has no current value")
}

// TestDiff_UnchangedRow asserts an identical row is labeled unchanged.
func TestDiff_UnchangedRow(t *testing.T) {
	cs, err := Diff(prevDoc(), currDoc())
	require.NoError(t, err)
	cos := rowByConcept(t, cs, "us-gaap:CostOfRevenue")
	require.Equal(t, Unchanged, cos.Status, "FY2023 cost of sales is 400 in both filings")
	require.Equal(t, 0.0, *cos.Cells[0].Abs)
}

// TestDiff_PercentNullOnZeroBase asserts the percent delta is null when the
// previous value is zero (the absolute delta is still reported).
func TestDiff_PercentNullOnZeroBase(t *testing.T) {
	cs, err := Diff(prevDoc(), currDoc())
	require.NoError(t, err)
	mkt := rowByConcept(t, cs, "us-gaap:MarketingExpense")
	require.Equal(t, Changed, mkt.Status)
	require.Equal(t, 0.0, *mkt.Cells[0].Prev)
	require.Equal(t, 25.0, *mkt.Cells[0].Curr)
	require.Equal(t, 25.0, *mkt.Cells[0].Abs)
	require.Nil(t, mkt.Cells[0].Pct, "percent change against a zero base must be null")
}

// TestDiff_ParagraphGranularity asserts section text is diffed by paragraph,
// matched on item id, reporting exactly the one removed and one added paragraph.
func TestDiff_ParagraphGranularity(t *testing.T) {
	cs, err := Diff(prevDoc(), currDoc())
	require.NoError(t, err)

	require.Len(t, cs.Sections, 1)
	sec := cs.Sections[0]
	require.Equal(t, "1A", sec.Item)
	require.Equal(t, []ParagraphChange{
		{Kind: Removed, Text: "We depend on a legacy product line."},
		{Kind: Added, Text: "New regulation may raise our costs."},
	}, sec.Paragraphs)
}

// TestDiff_NilDocument asserts a nil side is a typed error, not a panic.
func TestDiff_NilDocument(t *testing.T) {
	_, err := Diff(nil, currDoc())
	require.ErrorIs(t, err, ErrNilDocument)
	_, err = Diff(prevDoc(), nil)
	require.ErrorIs(t, err, ErrNilDocument)
}

// TestJSONRoundTrip asserts the change set survives a JSON round-trip with its
// null cells intact (a missing value stays null, never 0).
func TestJSONRoundTrip(t *testing.T) {
	cs, err := Diff(prevDoc(), currDoc())
	require.NoError(t, err)

	var b bytes.Buffer
	require.NoError(t, JSON{}.Render(cs, &b))
	require.Contains(t, b.String(), "null")

	var got ChangeSet
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))
	require.Equal(t, *cs, got)
}

// TestMarkdownGolden golden-files the Markdown "what changed" report.
func TestMarkdownGolden(t *testing.T) {
	cs, err := Diff(prevDoc(), currDoc())
	require.NoError(t, err)

	var b bytes.Buffer
	require.NoError(t, Markdown{}.Render(cs, &b))

	path := filepath.Join("testdata", "diff.md")
	if *update {
		require.NoError(t, os.WriteFile(path, b.Bytes(), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden diff.md (run: go test ./internal/diff -update)")
	require.Equal(t, string(want), b.String())
}

func TestRendererFor(t *testing.T) {
	for _, f := range []string{"", "json", "md", "markdown", "JSON", "MD"} {
		r, err := RendererFor(f)
		require.NoError(t, err, f)
		require.NotNil(t, r)
	}
	_, err := RendererFor("text")
	require.ErrorIs(t, err, ErrUnknownFormat)
}
