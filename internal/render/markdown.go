package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/kritidutta01/sec-cli/internal/model"
)

// Markdown renders the LLM- and human-facing view: a document heading, each
// section as a `##` heading with its free text and tables, the financial
// statements as right-aligned Markdown tables, and a compact provenance footer.
type Markdown struct{}

// Render writes doc as Markdown.
func (Markdown) Render(doc *model.Document, w io.Writer) error {
	var b strings.Builder
	// Schema/version stamp so downstream consumers know the contract they are reading.
	fmt.Fprintf(&b, "<!-- sec-cli v%s schema %s -->\n\n", doc.Metadata.ParserVersion, doc.Metadata.SchemaVersion)
	if h := docHeading(doc.Metadata); h != "" {
		fmt.Fprintf(&b, "# %s\n\n", h)
	}
	for _, s := range doc.Sections {
		writeSectionMarkdown(&b, s)
	}
	if len(doc.Statements) > 0 {
		b.WriteString("## Financial Statements\n\n")
		for _, t := range doc.Statements {
			writeTableMarkdown(&b, t)
		}
	}
	writeDocFooterMarkdown(&b, doc.Metadata)
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("render: write markdown: %w", err)
	}
	return nil
}

func writeSectionMarkdown(b *strings.Builder, s model.Section) {
	fmt.Fprintf(b, "## %s\n\n", sectionTitle(s))
	if s.Text != "" {
		b.WriteString(s.Text)
		b.WriteString("\n\n")
	}
	for _, t := range s.Tables {
		writeTableMarkdown(b, t)
	}
}

func writeTableMarkdown(b *strings.Builder, t model.Table) {
	if t.Title != "" {
		fmt.Fprintf(b, "### %s\n\n", t.Title)
	}

	header := make([]string, 0, len(t.Columns)+1)
	header = append(header, "") // the row-label column has no header
	for _, c := range t.Columns {
		header = append(header, mdCell(c.Label))
	}
	writeMarkdownRow(b, header)

	sep := make([]string, 0, len(t.Columns)+1)
	sep = append(sep, "---")
	for range t.Columns {
		sep = append(sep, "---:") // right-align numeric columns
	}
	writeMarkdownRow(b, sep)

	for _, r := range t.Rows {
		cells := make([]string, 0, len(t.Columns)+1)
		cells = append(cells, mdCell(r.Label))
		for _, v := range r.Values {
			cells = append(cells, formatValue(v))
		}
		writeMarkdownRow(b, cells)
	}

	fmt.Fprintf(b, "\n_source: %s · confidence: %s_\n\n", t.Source.Extractor, t.Confidence.Level)
}

func writeMarkdownRow(b *strings.Builder, cells []string) {
	b.WriteByte('|')
	for _, c := range cells {
		b.WriteByte(' ')
		b.WriteString(c)
		b.WriteString(" |")
	}
	b.WriteByte('\n')
}

func writeDocFooterMarkdown(b *strings.Builder, m model.Metadata) {
	if m.SchemaVersion == "" && m.ParserVersion == "" && m.Confidence.Level == "" {
		return
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(b, "_schema %s · parser %s · confidence: %s_\n",
		m.SchemaVersion, m.ParserVersion, m.Confidence.Level)
}

// mdCell makes a string safe for a Markdown table cell: pipes are escaped and
// newlines collapse to spaces so a cell never breaks the row.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}
