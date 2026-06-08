package ixbrl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kritidutta01/sec-cli/internal/model"
	"golang.org/x/net/html"
)

// parseTable parses an HTML fragment and returns its first <table> node.
func parseTable(t *testing.T, fragment string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(fragment))
	require.NoError(t, err)
	tbl := findDescendant(doc, "table")
	require.NotNil(t, tbl, "fixture must contain a <table>")
	return tbl
}

func layoutRowByLabel(tbl *model.Table, label string) (model.Row, bool) {
	for _, r := range tbl.Rows {
		if r.Label == label {
			return r, true
		}
	}
	return model.Row{}, false
}

// Phase 1: header resolution — a spanning title row is lifted out of the column
// headers, a two-row stacked header is merged per column, and the leading
// label-column header is dropped so the period labels align to the value
// columns. (Headers subdivided by a colspan into distinct sub-columns are a
// documented limitation of the layout fallback and are not expanded.)
func TestLayoutHeaderResolution(t *testing.T) {
	tbl := ExtractLayoutTable(parseTable(t, `
<table>
  <tr><td colspan="3"><b>Net sales by category</b></td></tr>
  <tr><th>Category</th><th>2024</th><th>2023</th></tr>
  <tr><th></th><th>Revenue</th><th>Revenue</th></tr>
  <tr><td>iPhone</td><td>$201,183</td><td>$200,583</td></tr>
</table>`))

	require.Equal(t, "Net sales by category", tbl.Title)
	require.Equal(t, []string{"2024 Revenue", "2023 Revenue"}, columnLabels(tbl))

	require.Len(t, tbl.Rows, 1)
	iphone := tbl.Rows[0]
	require.Equal(t, "iPhone", iphone.Label)
	require.Equal(t, []float64{201183, 200583}, values(iphone))
}

// Phase 2: footnote stripping — superscripts and a trailing "(N)" tag are
// removed from cells and recorded, so the figures parse.
func TestLayoutFootnoteStripping(t *testing.T) {
	tbl := ExtractLayoutTable(parseTable(t, `
<table>
  <tr><th>Item</th><th>2024</th></tr>
  <tr><td>Restructuring<sup>(1)</sup></td><td>$60,922<sup>1</sup></td></tr>
  <tr><td>Other</td><td>1,234(2)</td></tr>
</table>`))

	r, ok := layoutRowByLabel(tbl, "Restructuring")
	require.True(t, ok, "footnote marker must be stripped from the label")
	require.Equal(t, []float64{60922}, values(r))

	other, _ := layoutRowByLabel(tbl, "Other")
	require.Equal(t, []float64{1234}, values(other), "trailing (2) must not break the number")

	require.Contains(t, tbl.Footnotes, "1")
	require.Contains(t, tbl.Footnotes, "2")
}

// Phase 3: number normalization (plus spacer-column compaction) — currency and
// grouping symbols are stripped, parentheses negate, an em-dash is null, and a
// "$"-only spacer column is dropped.
func TestLayoutNumberNormalization(t *testing.T) {
	tbl := ExtractLayoutTable(parseTable(t, `
<table>
  <tr><th>Item</th><th></th><th>Amount</th></tr>
  <tr><td>Positive</td><td>$</td><td>1,234.5</td></tr>
  <tr><td>Negative</td><td>$</td><td>(567)</td></tr>
  <tr><td>NotApplicable</td><td></td><td>&#8212;</td></tr>
</table>`))

	// The "$" spacer column is compacted away, leaving one value column.
	require.Len(t, tbl.Columns, 1)

	pos, _ := layoutRowByLabel(tbl, "Positive")
	require.Equal(t, []float64{1234.5}, values(pos))

	neg, _ := layoutRowByLabel(tbl, "Negative")
	require.Equal(t, []float64{-567}, values(neg), "parentheses negate")

	na, _ := layoutRowByLabel(tbl, "NotApplicable")
	require.Nil(t, na.Values[0], "em-dash is null, never 0")
}

// Phase 4: row classification — a "Total" line that is bold is a total, an
// unformatted "Total" line is a subtotal, and the rest are data.
func TestLayoutRowClassification(t *testing.T) {
	tbl := ExtractLayoutTable(parseTable(t, `
<table>
  <tr><th>Item</th><th>2024</th></tr>
  <tr><td>Cost of sales</td><td>100</td></tr>
  <tr><td>Total segment costs</td><td>300</td></tr>
  <tr><td style="border-top:1px solid"><b>Total costs and expenses</b></td><td><b>400</b></td></tr>
</table>`))

	cost, _ := layoutRowByLabel(tbl, "Cost of sales")
	require.Equal(t, model.RowData, cost.Type)

	sub, _ := layoutRowByLabel(tbl, "Total segment costs")
	require.Equal(t, model.RowSubtotal, sub.Type, "unformatted Total is a subtotal")

	tot, _ := layoutRowByLabel(tbl, "Total costs and expenses")
	require.Equal(t, model.RowTotal, tot.Type, "bold/border-top Total is a total")
}

// Phase 5: structured emit — leftmost cell is the label, the rest are values,
// the extractor is tagged ixbrl.layout, and confidence is capped at medium even
// when every cell resolves.
func TestLayoutStructuredEmit(t *testing.T) {
	tbl := ExtractLayoutTable(parseTable(t, `
<table>
  <tr><th>Region</th><th>2024</th><th>2023</th></tr>
  <tr><td>Americas</td><td>167,045</td><td>162,560</td></tr>
  <tr><td>Europe</td><td>101,328</td><td>94,294</td></tr>
</table>`))

	require.Equal(t, extractorLayout, tbl.Source.Extractor)
	require.Equal(t, []string{"2024", "2023"}, columnLabels(tbl))
	require.Len(t, tbl.Rows, 2)
	require.Equal(t, []float64{167045, 162560}, values(tbl.Rows[0]))

	// Every cell resolved, but layout extraction never claims high confidence.
	require.Equal(t, "medium", tbl.Confidence.Level)
	require.Equal(t, 0, tbl.Confidence.UntaggedCellCount)
}

// TestLayoutAAPLNarrative runs the extractor against a real, untagged MD&A table
// from Apple's FY2024 10-K — the Products and Services Performance net-sales
// table — to confirm it survives genuine EDGAR markup: spacer $/% columns, an
// em-dash, a footnote marker, and parenthesised negatives.
func TestLayoutAAPLNarrative(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "aapl-mdna-products.htm"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	doc, err := html.Parse(f)
	require.NoError(t, err)

	tbl := ExtractLayoutTable(findDescendant(doc, "table"))

	require.Equal(t, extractorLayout, tbl.Source.Extractor)
	require.NotEqual(t, "high", tbl.Confidence.Level, "layout extraction never claims high")
	require.Equal(t, []string{"2024", "Change", "2023", "Change", "2022"}, columnLabels(tbl))
	require.Len(t, tbl.Rows, 6)

	// iPhone: the 2024 change is an em-dash → null; the 2023 change is (2)% → -2.
	iphone, ok := layoutRowByLabel(tbl, "iPhone")
	require.True(t, ok)
	require.Equal(t, 201183.0, *iphone.Values[0])
	require.Nil(t, iphone.Values[1], "em-dash change is null")
	require.Equal(t, 200583.0, *iphone.Values[2])
	require.Equal(t, -2.0, *iphone.Values[3])
	require.Equal(t, 205489.0, *iphone.Values[4])

	// The "(1)" marker on "Services" is moved to the footnote map, not the label.
	services, ok := layoutRowByLabel(tbl, "Services")
	require.True(t, ok, "footnote marker must be stripped from the Services label")
	require.Equal(t, 96169.0, *services.Values[0])
	require.Contains(t, tbl.Footnotes, "1")

	// The total row is typed total and its 2024 value equals the sum of the
	// category rows — proof the columns are aligned, not merely populated.
	total, ok := layoutRowByLabel(tbl, "Total net sales")
	require.True(t, ok)
	require.Equal(t, model.RowTotal, total.Type)
	require.Equal(t, 391035.0, *total.Values[0])

	var sum float64
	for _, label := range []string{"iPhone", "Mac", "iPad", "Wearables, Home and Accessories", "Services"} {
		r, ok := layoutRowByLabel(tbl, label)
		require.True(t, ok, "missing category row %q", label)
		sum += *r.Values[0]
	}
	require.Equal(t, *total.Values[0], sum, "category net sales must sum to the total")
}

// columnLabels and values are small assertion helpers.
func columnLabels(tbl *model.Table) []string {
	out := make([]string, len(tbl.Columns))
	for i, c := range tbl.Columns {
		out[i] = c.Label
	}
	return out
}

func values(r model.Row) []float64 {
	out := make([]float64, len(r.Values))
	for i, v := range r.Values {
		if v != nil {
			out[i] = *v
		}
	}
	return out
}
