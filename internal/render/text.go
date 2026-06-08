package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/kritidutta01/sec-cli/internal/model"
)

// Text renders plain text for terminals and grep, with no Markdown decoration:
// section titles upper-cased and underlined, statements as fixed-width aligned
// columns, and a blank column for a missing value (never 0).
type Text struct{}

// Render writes doc as plain text.
func (Text) Render(doc *model.Document, w io.Writer) error {
	var b strings.Builder
	if h := docHeading(doc.Metadata); h != "" {
		writeHeading(&b, h, '=')
		b.WriteByte('\n')
	}
	for _, s := range doc.Sections {
		writeSectionText(&b, s)
	}
	if len(doc.Statements) > 0 {
		writeHeading(&b, "FINANCIAL STATEMENTS", '=')
		b.WriteByte('\n')
		for _, t := range doc.Statements {
			writeTableText(&b, t)
		}
	}
	writeDocFooterText(&b, doc.Metadata)
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("render: write text: %w", err)
	}
	return nil
}

func writeSectionText(b *strings.Builder, s model.Section) {
	writeHeading(b, strings.ToUpper(sectionTitle(s)), '-')
	if s.Text != "" {
		b.WriteString(s.Text)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	for _, t := range s.Tables {
		writeTableText(b, t)
	}
}

func writeTableText(b *strings.Builder, t model.Table) {
	if t.Title != "" {
		b.WriteString(t.Title)
		b.WriteByte('\n')
	}

	rows := tableTextRows(t)
	widths := columnWidths(rows)
	for ri, cells := range rows {
		writeTextRow(b, cells, widths)
		if ri == 0 { // rule under the header
			b.WriteString(textRule(widths))
		}
	}
	fmt.Fprintf(b, "(source: %s; confidence: %s)\n\n", t.Source.Extractor, t.Confidence.Level)
}

// tableTextRows lays out a table as a header row followed by data rows, each a
// slice of cells: the label column then one cell per period (blank for a missing
// value).
func tableTextRows(t model.Table) [][]string {
	nCols := len(t.Columns)
	header := make([]string, nCols+1)
	for i, c := range t.Columns {
		header[i+1] = c.Label
	}
	rows := [][]string{header}
	for _, r := range t.Rows {
		cells := make([]string, nCols+1)
		cells[0] = r.Label
		for j := 0; j < nCols; j++ {
			if j < len(r.Values) {
				cells[j+1] = formatValue(r.Values[j])
			}
		}
		rows = append(rows, cells)
	}
	return rows
}

func columnWidths(rows [][]string) []int {
	if len(rows) == 0 {
		return nil
	}
	widths := make([]int, len(rows[0]))
	for _, cells := range rows {
		for i, c := range cells {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	return widths
}

// writeTextRow writes one row: the label column left-aligned, every period
// column right-aligned, separated by two spaces.
func writeTextRow(b *strings.Builder, cells []string, widths []int) {
	for i, c := range cells {
		if i == 0 {
			fmt.Fprintf(b, "%-*s", widths[i], c)
			continue
		}
		fmt.Fprintf(b, "  %*s", widths[i], c)
	}
	b.WriteByte('\n')
}

func textRule(widths []int) string {
	total := 0
	for i, w := range widths {
		total += w
		if i > 0 {
			total += 2 // the two-space gap before each period column
		}
	}
	return strings.Repeat("-", total) + "\n"
}

func writeDocFooterText(b *strings.Builder, m model.Metadata) {
	if m.SchemaVersion == "" && m.ParserVersion == "" && m.Confidence.Level == "" {
		return
	}
	fmt.Fprintf(b, "(schema %s; parser %s; confidence: %s)\n",
		m.SchemaVersion, m.ParserVersion, m.Confidence.Level)
}

// writeHeading writes a title followed by a rule of the given character.
func writeHeading(b *strings.Builder, title string, rule byte) {
	b.WriteString(title)
	b.WriteByte('\n')
	b.WriteString(strings.Repeat(string(rule), len(title)))
	b.WriteString("\n")
}
