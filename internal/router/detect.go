package router

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// ixNamespace is the inline-XBRL namespace URI declared on the <html> root of an
// iXBRL filing. Its presence is the layer-3 signal.
const ixNamespace = "http://www.xbrl.org/2013/inlineXBRL"

// Density thresholds (layer 4), per DESIGN.md. Tag density is the fraction of a
// document's financial-figure table cells that carry an inline-XBRL fact
// wrapper. These are starting points to be tuned against the held-out corpus.
const (
	ixbrlDensityThreshold   = 0.80
	partialDensityThreshold = 0.10
)

// headScanLimit bounds the cheap byte-level signal scans (layers 2–3) to the
// document head, where the <html> tag and namespace declaration live.
const headScanLimit = 64 * 1024

// smallIntegerCeiling is the magnitude below which a bare integer cell is
// treated as incidental (page number, footnote marker, small count) rather than
// a reported financial figure.
const smallIntegerCeiling = 100

// lineArt matches the runs of '-' or '=' that pre-2010 ASCII filings use as
// rules, a layer-2 PlainText signal.
var lineArt = regexp.MustCompile(`[-=]{5,}`)

// Fetcher resolves a filing-index document reference to the bytes it points at.
// The router calls it at most once, to follow an index page to the real primary
// document. It is a plain function (not an interface) so tests pass a fake.
type Fetcher func(ctx context.Context, ref string) ([]byte, error)

// Detect classifies raw filing bytes. When the bytes are a filing-index page it
// follows the index to the primary document (via fetch) and classifies that.
// Index resolution recurses at most once — a primary document should never
// itself be an index.
func Detect(ctx context.Context, raw []byte, fetch Fetcher) (*Decision, error) {
	return detect(ctx, raw, fetch, 0)
}

func detect(ctx context.Context, raw []byte, fetch Fetcher, depth int) (*Decision, error) {
	dec := &Decision{PrimaryDocument: raw}

	// Parse once if the bytes look like HTML; reused by index detection and the
	// density classifier.
	var root *html.Node
	if hasHTMLTag(raw) {
		var err error
		if root, err = html.Parse(bytes.NewReader(raw)); err != nil {
			return nil, fmt.Errorf("router: parse html: %w", err)
		}
	}

	// Layer 1: filing-index resolution (only at depth 0; bounded recursion).
	if depth == 0 && root != nil {
		if ref, ok := filingIndexRef(root); ok {
			return resolveIndex(ctx, dec, ref, fetch, depth)
		}
	}

	// Layer 2: not HTML → PlainText (pre-2010 ASCII) or Unknown.
	if root == nil {
		if looksLikePlainText(raw) {
			dec.Format = PlainText
		} else {
			dec.Format = Unknown
		}
		return dec, nil
	}

	// Layer 3: no inline-XBRL namespace → PlainHTML.
	if !hasIXNamespace(raw) {
		dec.Format = PlainHTML
		if depth > 0 {
			if _, ok := filingIndexRef(root); ok {
				dec.Warnings = append(dec.Warnings,
					"resolved document still resembles a filing index; not resolving further")
			}
		}
		return dec, nil
	}

	// Layer 4: tag density on financial-figure cells.
	classifyDensity(root, dec)
	return dec, nil
}

// resolveIndex follows a filing-index reference to the primary document and
// classifies that, marking the result as index-resolved.
func resolveIndex(ctx context.Context, dec *Decision, ref string, fetch Fetcher, depth int) (*Decision, error) {
	if fetch == nil {
		dec.Format = Unknown
		dec.Warnings = append(dec.Warnings, "filing index detected but no fetcher provided")
		return dec, nil
	}
	primary, err := fetch(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("router: resolve filing index %q: %w", ref, err)
	}
	inner, err := detect(ctx, primary, fetch, depth+1)
	if err != nil {
		return nil, err
	}
	inner.ResolvedFromIndex = true
	return inner, nil
}

// classifyDensity sets dec.Format from the fraction of financial-figure cells
// that carry an inline-XBRL fact wrapper. It assumes the iXBRL namespace is
// declared.
func classifyDensity(root *html.Node, dec *Decision) {
	numeric, tagged := numericCellStats(root)
	switch numeric {
	case 0:
		// Namespace declared but nothing tabular to measure. If any fact wrapper
		// exists it is a fact-tagged filing; otherwise the namespace is inert.
		if findNode(root, isIXElement) != nil {
			dec.Format = IXBRL
		} else {
			dec.Format = PlainHTML
			dec.Warnings = append(dec.Warnings,
				"inline-XBRL namespace declared but no facts found")
		}
	default:
		density := float64(tagged) / float64(numeric)
		switch {
		case density >= ixbrlDensityThreshold:
			dec.Format = IXBRL
		case density >= partialDensityThreshold:
			dec.Format = PartialIXBRL
		default:
			dec.Format = PlainHTML
		}
	}
}

// hasHTMLTag reports whether the document head contains an <html> tag.
func hasHTMLTag(raw []byte) bool {
	return bytes.Contains(bytes.ToLower(head(raw)), []byte("<html"))
}

// hasIXNamespace reports whether the inline-XBRL namespace is declared.
func hasIXNamespace(raw []byte) bool {
	return bytes.Contains(head(raw), []byte(ixNamespace))
}

// looksLikePlainText reports whether non-HTML bytes are a pre-2010 EDGAR ASCII
// filing rather than something unrecognized. EDGAR full-submission text carries
// SGML document wrappers; standalone text filings carry form-feed page breaks
// and ASCII rules.
func looksLikePlainText(raw []byte) bool {
	h := head(raw)
	lower := bytes.ToLower(h)
	if bytes.Contains(lower, []byte("<sec-document>")) ||
		bytes.Contains(lower, []byte("<document>")) ||
		bytes.Contains(lower, []byte("<type>")) {
		return true
	}
	return bytes.ContainsRune(raw, '\f') && lineArt.Match(h)
}

// head returns the leading window of raw used by the cheap byte-level scans.
func head(raw []byte) []byte {
	if len(raw) > headScanLimit {
		return raw[:headScanLimit]
	}
	return raw
}

// numericCellStats counts a document's financial-figure table cells and how
// many of them carry an inline-XBRL fact wrapper. Density is measured over
// financial figures only — bare years and small incidental integers (page
// numbers, footnote markers, headcounts) are excluded so they don't dilute the
// signal. On AAPL's fully-tagged FY2024 10-K this lifts measured density from
// ~0.70 over all numeric cells to ~0.89 over financial figures.
func numericCellStats(root *html.Node) (numeric, tagged int) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "td" || n.Data == "th") {
			if isFinancialFigure(textContent(n)) {
				numeric++
				if findNode(n, isIXElement) != nil {
					tagged++
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return numeric, tagged
}

// isIXElement reports whether a node is an inline-XBRL element (ix: prefix).
func isIXElement(n *html.Node) bool {
	return n.Type == html.ElementNode && strings.HasPrefix(n.Data, "ix:")
}

// isFinancialFigure reports whether cell text is a reported financial value:
// a number — once currency symbols, grouping commas, parentheses, percent signs
// and whitespace (incl. non-breaking spaces) are stripped — that is either
// fractional (per-share figures, ratios, rates) or a sizeable integer, and is
// not a bare calendar year.
func isFinancialFigure(s string) bool {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		switch r {
		case '$', ',', '(', ')', '%':
			return -1
		}
		return r
	}, s)
	if cleaned == "" {
		return false
	}
	v, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return false
	}
	if strings.ContainsRune(cleaned, '.') {
		return true // fractional: EPS, ratios, rates
	}
	abs := math.Abs(v)
	if abs >= 1900 && abs <= 2100 {
		return false // bare calendar year (column header, "as of 2024")
	}
	return abs >= smallIntegerCeiling
}

// textContent returns the concatenated text of a node's subtree.
func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return sb.String()
}
