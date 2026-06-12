package diff

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Renderer writes one serialization of a ChangeSet to w. It mirrors the shape of
// internal/render's Renderer (a total function over the artifact) so a diff
// renders the same way a document does — JSON is the canonical contract, Markdown
// the human- and LLM-facing "what changed" report.
type Renderer interface {
	Render(cs *ChangeSet, w io.Writer) error
}

// ErrUnknownFormat is returned by RendererFor for an unrecognized format name.
var ErrUnknownFormat = errors.New("diff: unknown format")

// RendererFor returns the renderer for a format name: "json" (the default) or
// "md"/"markdown". Text is intentionally not offered — a diff is a report, and
// its two useful forms are the canonical JSON and the Markdown summary.
func RendererFor(format string) (Renderer, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return JSON{}, nil
	case "md", "markdown":
		return Markdown{}, nil
	default:
		return nil, fmt.Errorf("%w: %q (choose json, md)", ErrUnknownFormat, format)
	}
}

// LexicalRenderer writes a lexical diff report — per-section word-level diffs
// — as plain Markdown. It is used when --layer lexical is requested and wraps
// both the structural ChangeSet header and the word-level paragraph diffs.
type LexicalRenderer struct{}

// Render writes the lexical diff as Markdown.
func (LexicalRenderer) Render(cs *ChangeSet, lexical []LexicalSectionDiff, w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Lexical diff — %s\n\n", diffHeading(cs))
	if len(cs.Statements) > 0 {
		b.WriteString("## Financial Statements\n\n")
		for _, s := range cs.Statements {
			writeStatementMarkdown(&b, s)
		}
	}
	if len(lexical) == 0 {
		b.WriteString("_No modified sections with paired paragraphs._\n")
	}
	for _, ls := range lexical {
		heading := ls.Title
		if ls.Item != "" {
			heading = "Item " + ls.Item
			if ls.Title != "" {
				heading += " — " + ls.Title
			}
		}
		fmt.Fprintf(&b, "## %s\n\n", heading)
		for i, p := range ls.Paragraphs {
			fmt.Fprintf(&b, "**Paragraph %d**\n\n%s\n\n", i+1, p.Text)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// JSON renders the canonical, schema-stable serialization of a change set:
// deterministic field order, HTML left unescaped so paragraph text stays
// readable, and a trailing newline.
type JSON struct{}

// Render writes cs as indented JSON.
func (JSON) Render(cs *ChangeSet, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cs); err != nil {
		return fmt.Errorf("diff: encode json: %w", err)
	}
	return nil
}

// Markdown renders the human- and LLM-facing "what changed" report: a heading
// naming the two filings, a deltas table per statement (changed/added/removed
// rows only — unchanged lines are omitted so the change stands out), and a
// bulleted list of added/removed paragraphs per section.
type Markdown struct{}

// Render writes cs as a Markdown report.
func (Markdown) Render(cs *ChangeSet, w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Changes — %s\n\n", diffHeading(cs))

	if len(cs.Statements) > 0 {
		b.WriteString("## Financial Statements\n\n")
		for _, s := range cs.Statements {
			writeStatementMarkdown(&b, s)
		}
	}
	if len(cs.Sections) > 0 {
		b.WriteString("## Sections\n\n")
		for _, s := range cs.Sections {
			writeSectionMarkdown(&b, s)
		}
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("diff: write markdown: %w", err)
	}
	return nil
}

// diffHeading builds a one-line description of the comparison from whatever
// metadata is present.
func diffHeading(cs *ChangeSet) string {
	name := cs.Curr.Company
	if name == "" {
		name = cs.Curr.Ticker
	}
	parts := make([]string, 0, 2)
	if name != "" {
		parts = append(parts, name)
	}
	if cs.Curr.Form != "" {
		parts = append(parts, cs.Curr.Form)
	}
	head := strings.Join(parts, " ")
	from, to := cs.Prev.PeriodEnd, cs.Curr.PeriodEnd
	if from == "" {
		from = cs.Prev.FilingDate
	}
	if to == "" {
		to = cs.Curr.FilingDate
	}
	if from != "" || to != "" {
		if head != "" {
			head += " "
		}
		head += from + " → " + to
	}
	if head == "" {
		return "(unlabeled filings)"
	}
	return head
}

// writeStatementMarkdown renders one statement's deltas as a table per
// overlapping period (a single period is the common case). Unchanged rows are
// omitted; the report shows only what moved.
func writeStatementMarkdown(b *strings.Builder, s StatementDiff) {
	title := s.Title
	if title == "" {
		title = s.RoleURI
	}
	if title == "" {
		title = "(statement)"
	}

	if len(s.Periods) == 0 {
		fmt.Fprintf(b, "### %s\n\n_no overlapping periods_\n\n", mdCell(title))
		return
	}

	for pi, period := range s.Periods {
		fmt.Fprintf(b, "### %s — %s\n\n", mdCell(title), mdCell(period))
		b.WriteString("| Line | Previous | Current | Δ | Δ% |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
		hasRows := false
		for _, r := range s.Rows {
			if r.Status == Unchanged {
				continue
			}
			if pi >= len(r.Cells) {
				continue
			}
			c := r.Cells[pi]
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
				mdCell(rowLabel(r)),
				formatValue(c.Prev),
				formatValue(c.Curr),
				formatSigned(c.Abs),
				formatPercent(c.Pct))
			hasRows = true
		}
		if !hasRows {
			b.WriteString("| _no changes_ |  |  |  |  |\n")
		}
		b.WriteByte('\n')
	}
}

// rowLabel annotates a row's label with a marker when it was added or removed,
// so the table reads on its own.
func rowLabel(r RowDelta) string {
	switch r.Status {
	case Added:
		return r.Label + " (added)"
	case Removed:
		return r.Label + " (removed)"
	default:
		return r.Label
	}
}

// writeSectionMarkdown renders one section's narrative changes as bulleted
// added/removed paragraphs.
func writeSectionMarkdown(b *strings.Builder, s SectionDiff) {
	heading := s.Title
	if s.Item != "" {
		heading = "Item " + s.Item
		if s.Title != "" {
			heading += " — " + s.Title
		}
	}
	fmt.Fprintf(b, "### %s\n\n", heading)
	for _, p := range s.Paragraphs {
		marker := "+"
		if p.Kind == Removed {
			marker = "−"
		}
		fmt.Fprintf(b, "- %s %s\n", marker, mdInline(p.Text))
	}
	b.WriteByte('\n')
}

// formatValue renders an optional value: a missing value is the empty string,
// never "0"; a present value is its shortest exact decimal.
func formatValue(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// formatSigned renders a delta with an explicit sign so a gain reads "+100" and
// a drop "-50"; a null delta is blank.
func formatSigned(v *float64) string {
	if v == nil {
		return ""
	}
	s := strconv.FormatFloat(*v, 'f', -1, 64)
	if *v >= 0 {
		return "+" + s
	}
	return s
}

// formatPercent renders a percentage to one decimal place with a sign; a null
// percent (zero/absent base) is blank.
func formatPercent(v *float64) string {
	if v == nil {
		return ""
	}
	s := strconv.FormatFloat(*v, 'f', 1, 64)
	if *v >= 0 {
		s = "+" + s
	}
	return s + "%"
}

// mdCell makes a string safe for a Markdown table cell: newlines collapse to
// spaces and pipes are escaped so a cell never breaks the row.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}

// mdInline makes paragraph text safe for a single-line bullet: newlines collapse
// to spaces.
func mdInline(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}
