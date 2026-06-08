package render

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kritidutta01/sec-cli/internal/model"
)

var update = flag.Bool("update", false, "update golden files")

// sampleDocument is a fixed Document exercising every renderer: a narrative
// section, a statement with a total row, and — deliberately — a null cell (R&D
// in the prior year) that must never render as 0.
func sampleDocument() *model.Document {
	f := func(v float64) *float64 { return &v }
	return &model.Document{
		Metadata: model.Metadata{
			Company:       "Acme Corp",
			Ticker:        "ACME",
			Form:          "10-K",
			Accession:     "0000000000-24-000001",
			FilingDate:    "2024-11-01",
			PeriodEnd:     "2024-12-31",
			SchemaVersion: model.SchemaVersion,
			ParserVersion: model.ParserVersion,
			Source:        model.Source{Extractor: "ixbrl.facts", ParserVersion: model.ParserVersion},
			Confidence:    model.Confidence{Level: "high", RowMatchRate: 1, CellResolvedRate: 1},
		},
		Sections: []model.Section{
			{
				Item:  "1A",
				Title: "Risk Factors",
				Kind:  model.KindNarrative,
				Text:  "Our results are subject to risks.\n\nMarkets fluctuate.",
			},
		},
		Statements: []model.Table{
			{
				SchemaVersion: model.SchemaVersion,
				Title:         "Income Statement",
				Columns:       []model.Column{{Label: "2024"}, {Label: "2023"}},
				Rows: []model.Row{
					{Label: "Revenue", Concept: "us-gaap:Revenues", Type: model.RowData, Values: []*float64{f(1000), f(900)}},
					{Label: "Research & development", Type: model.RowData, Values: []*float64{f(200), nil}},
					{Label: "Net income", Type: model.RowTotal, Values: []*float64{f(300), f(250)}},
				},
				Confidence: model.Confidence{Level: "medium", RowMatchRate: 0.67, CellResolvedRate: 0.83, UntaggedCellCount: 1},
				Source:     model.Source{Extractor: "ixbrl.facts", ParserVersion: model.ParserVersion},
			},
		},
	}
}

func render(t *testing.T, r Renderer) string {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, r.Render(sampleDocument(), &b))
	return b.String()
}

// goldenCheck compares got against testdata/<name>, writing the golden when
// -update is set.
func goldenCheck(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden %s (run: go test ./internal/render -update)", name)
	require.Equal(t, string(want), got)
}

func TestRenderJSON(t *testing.T)     { goldenCheck(t, "document.json", render(t, JSON{})) }
func TestRenderMarkdown(t *testing.T) { goldenCheck(t, "document.md", render(t, Markdown{})) }
func TestRenderText(t *testing.T)     { goldenCheck(t, "document.txt", render(t, Text{})) }

// TestJSONRoundTrip renders to JSON, unmarshals, and asserts the meaningful
// fields survive — including the null cell staying null.
func TestJSONRoundTrip(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, JSON{}.Render(sampleDocument(), &b))

	var got model.Document
	require.NoError(t, json.Unmarshal(b.Bytes(), &got))

	require.Equal(t, "Acme Corp", got.Metadata.Company)
	require.Equal(t, model.SchemaVersion, got.Metadata.SchemaVersion)
	require.Len(t, got.Sections, 1)
	require.Equal(t, "1A", got.Sections[0].Item)
	require.Len(t, got.Statements, 1)

	rnd := got.Statements[0].Rows[1]
	require.Equal(t, "Research & development", rnd.Label)
	require.NotNil(t, rnd.Values[0])
	require.Equal(t, 200.0, *rnd.Values[0])
	require.Nil(t, rnd.Values[1], "absent cell must round-trip as null, never 0")
}

// TestNullNeverZero asserts a missing cell renders as JSON null / empty Markdown
// cell / blank text column — never as "0".
func TestNullNeverZero(t *testing.T) {
	jsonOut := render(t, JSON{})
	require.Contains(t, jsonOut, "null")
	// The R&D row's values are [200, null]; the 2023 cell must not be 0.
	require.NotContains(t, jsonOut, "200,\n          0")

	md := render(t, Markdown{})
	// The R&D row ends with an empty trailing cell: "200 |  |".
	require.Contains(t, md, "| Research & development | 200 |  |")

	txt := render(t, Text{})
	require.Regexp(t, `Research & development\s+200\s*\n`, txt)
}

func TestFor(t *testing.T) {
	for _, f := range []string{"", "json", "md", "markdown", "text", "txt", "JSON", "MD"} {
		r, err := For(f)
		require.NoError(t, err, f)
		require.NotNil(t, r)
	}
	_, err := For("yaml")
	require.ErrorIs(t, err, ErrUnknownFormat)
}
