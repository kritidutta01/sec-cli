package ixbrl

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// extractorLayout marks tables read by spatial layout rather than the fact
// stream. Layout extraction reconstructs the number-to-label binding from cell
// position, so it has a lower accuracy ceiling and is capped at medium
// confidence.
const extractorLayout = "ixbrl.layout"

// yearPattern matches a four-digit year, the signal that a header row carries
// period columns.
var yearPattern = regexp.MustCompile(`\b(19|20)\d{2}\b`)

// cellInfo is one table cell's content and formatting, with footnote markers
// already stripped from the text.
type cellInfo struct {
	text   string
	bold   bool
	header bool // came from a <th>
	style  string
}

// meaningful reports whether a cell carries content — not a blank spacer and not
// a lone currency or percent decorator. EDGAR splits "$ 201,183 %" across three
// cells; only the number is meaningful, so values align row-to-row regardless of
// whether a given row shows the "$".
func (c cellInfo) meaningful() bool {
	t := strings.TrimSpace(c.text)
	return t != "" && t != "$" && t != "%"
}

// ExtractLayoutTable extracts a narrative HTML table — one not bound to the
// iXBRL fact stream — into a *Table using the five-phase layout pipeline: header
// resolution, footnote stripping, number normalisation, row classification, and
// structured emit. It is the fallback for tables in MD&A, Risk Factors, and
// exhibits inside an iXBRL filing.
//
// Values are read by sequence rather than by physical column: each data row's
// leftmost text cell is the label and its remaining meaningful cells are the
// values, which sidesteps the spacer and currency columns that pervade EDGAR
// markup. (Headers subdivided by colspan into distinct sub-columns are not
// expanded; that is a known limitation of the layout fallback.)
func ExtractLayoutTable(table *html.Node) *Table {
	footnotes := make(map[string]string)
	rows := dropEmptyRows(readRows(tableRows(table), footnotes))

	title, headers, dataStart := splitHeader(rows)
	dataRows := rows[dataStart:]

	nCols := 0
	for _, r := range dataRows {
		if n := len(valuesOf(r)); n > nCols {
			nCols = n
		}
	}

	columns := buildColumns(headers, nCols)
	outRows := buildRows(dataRows, nCols)

	conf := computeConfidence(outRows, nCols)
	// Layout extraction never claims high confidence; that is reserved for the
	// fact-stream path, where the number-to-concept binding is in the source.
	if conf.Level == "high" {
		conf.Level = "medium"
	}

	out := &Table{
		SchemaVersion: schemaVersion,
		Title:         title,
		Columns:       columns,
		Rows:          outRows,
		Confidence:    conf,
		Source:        Source{Extractor: extractorLayout, ParserVersion: parserVersion},
	}
	if len(footnotes) > 0 {
		out.Footnotes = footnotes
	}
	return out
}

// readRows reads each <tr> into its sequence of cells, stripping footnote
// markers from the text as it goes.
func readRows(trs []*html.Node, footnotes map[string]string) [][]cellInfo {
	rows := make([][]cellInfo, 0, len(trs))
	for _, tr := range trs {
		var row []cellInfo
		for _, cn := range cellElements(tr) {
			row = append(row, cellInfo{
				text:   cellText(cn, footnotes),
				bold:   isBold(cn),
				header: cn.Data == "th",
				style:  attrVal(cn, "style"),
			})
		}
		rows = append(rows, row)
	}
	return rows
}

// dropEmptyRows removes rows with no meaningful cells — the blank spacer rows
// EDGAR inserts for vertical rhythm — so header resolution starts at the first
// real row.
func dropEmptyRows(rows [][]cellInfo) [][]cellInfo {
	out := rows[:0]
	for _, r := range rows {
		if len(meaningfulCells(r)) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// meaningfulCells returns a row's content cells, dropping blanks and decorators.
func meaningfulCells(row []cellInfo) []cellInfo {
	var out []cellInfo
	for _, c := range row {
		if c.meaningful() {
			out = append(out, c)
		}
	}
	return out
}

// valuesOf returns a data row's value cells: its meaningful cells after the
// leading label cell.
func valuesOf(row []cellInfo) []cellInfo {
	cells := meaningfulCells(row)
	if len(cells) == 0 {
		return nil
	}
	return cells[1:]
}

// splitHeader separates an optional spanning title and the leading header rows
// from the data rows. A header row's meaningful cells are all bold/<th> or all
// carry a year; a lone bold non-numeric leading cell is the table title.
func splitHeader(rows [][]cellInfo) (title string, headers [][]cellInfo, dataStart int) {
	i := 0
	if i < len(rows) {
		if cells := meaningfulCells(rows[i]); len(cells) == 1 && cells[0].bold && !isNumberish(cells[0].text) {
			title = strings.TrimSpace(cells[0].text)
			i++
		}
	}
	for ; i < len(rows); i++ {
		if !isHeaderRow(rows[i]) {
			break
		}
		headers = append(headers, rows[i])
	}
	return title, headers, i
}

func isHeaderRow(row []cellInfo) bool {
	cells := meaningfulCells(row)
	if len(cells) == 0 {
		return false
	}
	allBold := true
	for _, c := range cells {
		if !c.bold && !c.header {
			allBold = false
			break
		}
	}
	if allBold {
		return true
	}
	for _, c := range cells {
		if !yearPattern.MatchString(c.text) {
			return false
		}
	}
	return true
}

// buildColumns labels each of the nCols value columns from the header rows. Each
// header row's meaningful cells are aligned to the value columns: a header row
// with one extra cell carries a label-column header that is dropped; a longer
// header row is right-aligned (financial tables are right-aligned). Labels from
// stacked header rows are joined per column.
func buildColumns(headers [][]cellInfo, nCols int) []Column {
	if nCols == 0 {
		return nil
	}
	labels := make([][]string, nCols)
	for _, hr := range headers {
		cells := meaningfulCells(hr)
		texts := make([]string, len(cells))
		for i, c := range cells {
			texts[i] = strings.TrimSpace(c.text)
		}
		switch {
		case len(texts) == nCols+1:
			texts = texts[1:] // drop the row-label column's header
		case len(texts) > nCols:
			texts = texts[len(texts)-nCols:] // right-align
		}
		for i, t := range texts {
			if i < nCols && t != "" && !containsString(labels[i], t) {
				labels[i] = append(labels[i], t)
			}
		}
	}
	cols := make([]Column, nCols)
	for i := range cols {
		cols[i] = Column{Label: strings.Join(labels[i], " ")}
	}
	return cols
}

// buildRows turns each data row into a Row: the leading meaningful cell is the
// label and the rest are normalised numeric values, padded to nCols (a missing
// trailing cell is null, never zero).
func buildRows(dataRows [][]cellInfo, nCols int) []Row {
	var rows []Row
	for _, r := range dataRows {
		cells := meaningfulCells(r)
		if len(cells) == 0 {
			continue
		}
		label := strings.TrimSpace(cells[0].text)
		vals := make([]*float64, nCols)
		for i, c := range cells[1:] {
			if i >= nCols {
				break
			}
			vals[i] = parseLayoutValue(c.text)
		}
		rows = append(rows, Row{
			Label:  label,
			Type:   classifyLayoutRow(label, r),
			Values: vals,
		})
	}
	return rows
}

// classifyLayoutRow types a row from its label and formatting: a "Total" line
// that is bold or has a top border is a total; an unformatted "Total" line is a
// subtotal; everything else is data.
func classifyLayoutRow(label string, row []cellInfo) RowType {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(label)), "total") {
		return RowData
	}
	for _, c := range row {
		if c.bold || strings.Contains(strings.ToLower(c.style), "border-top") {
			return RowTotal
		}
	}
	return RowSubtotal
}

// parseLayoutValue normalises a displayed figure into a signed float: strips
// currency, percent, and grouping separators; reads surrounding parentheses as
// negative; returns nil for an empty cell or an em-dash (not applicable), never
// zero.
func parseLayoutValue(text string) *float64 {
	t := strings.TrimSpace(text)
	if t == "" || isDashZero(t) {
		return nil
	}
	negative := false
	if strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")") {
		negative = true
		t = t[1 : len(t)-1]
	}
	var b strings.Builder
	for _, r := range t {
		if unicode.IsDigit(r) || r == '.' || r == '-' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" || s == "-" || s == "." {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	if negative {
		v = -v
	}
	return &v
}

func isNumberish(text string) bool {
	return parseLayoutValue(text) != nil
}

// cellText returns a cell's visible text with footnote markers removed:
// superscript elements and a trailing "(N)" tag (where N is one or two
// alphanumerics) are stripped and recorded in footnotes, so a figure like
// "$60,922(1)" parses cleanly.
func cellText(n *html.Node, footnotes map[string]string) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.ElementNode && c.Data == "sup" {
			recordFootnote(elementText(c), footnotes)
			return
		}
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)

	return stripTrailingFootnote(collapseSpaces(sb.String()), footnotes)
}

var trailingFootnote = regexp.MustCompile(`^(.+?)\(([0-9A-Za-z]{1,2})\)$`)

func stripTrailingFootnote(text string, footnotes map[string]string) string {
	text = strings.TrimSpace(text)
	m := trailingFootnote.FindStringSubmatch(text)
	if m == nil {
		return text
	}
	prefix := strings.TrimSpace(m[1])
	// A bare "(1,234)" is a negative number, not a footnote: only strip when
	// there is real content before the marker.
	if prefix == "" {
		return text
	}
	recordFootnote(m[2], footnotes)
	return prefix
}

func recordFootnote(marker string, footnotes map[string]string) {
	marker = strings.Trim(strings.TrimSpace(marker), "()")
	marker = strings.TrimSpace(marker)
	if marker == "" {
		return
	}
	if _, ok := footnotes[marker]; !ok {
		footnotes[marker] = ""
	}
}

// collapseSpaces trims the string and collapses every run of whitespace,
// including the non-breaking spaces EDGAR uses for indentation, to one space.
func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) || r == ' ' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// tableRows returns the table's logical rows in document order, descending
// through thead/tbody/tfoot but not into nested tables (a cell's inner table is
// not part of this table's grid).
func tableRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.Data {
			case "table":
				// stop: a nested table belongs to its own grid
			case "tr":
				rows = append(rows, c)
			default:
				walk(c)
			}
		}
	}
	walk(table)
	return rows
}

func cellElements(tr *html.Node) []*html.Node {
	var cells []*html.Node
	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
			cells = append(cells, c)
		}
	}
	return cells
}

// isBold reports whether a cell's text renders bold, via an inline font-weight
// style or a <b>/<strong> wrapper carrying text.
func isBold(n *html.Node) bool {
	if styleBold(attrVal(n, "style")) {
		return true
	}
	found := false
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if found {
			return
		}
		if c.Type == html.ElementNode {
			if (c.Data == "b" || c.Data == "strong") && strings.TrimSpace(elementText(c)) != "" {
				found = true
				return
			}
			if styleBold(attrVal(c, "style")) {
				found = true
				return
			}
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return found
}

func styleBold(style string) bool {
	s := strings.ToLower(strings.ReplaceAll(style, " ", ""))
	return strings.Contains(s, "font-weight:bold") || strings.Contains(s, "font-weight:700")
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
