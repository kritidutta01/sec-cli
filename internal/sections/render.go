package sections

import (
	"strings"

	"golang.org/x/net/html"
)

// RenderText renders a node's subtree to clean free text: block elements become
// blank-line-separated paragraphs, list items are prefixed with "- ", and all
// inline-XBRL scaffolding is removed — ix:header and ix:hidden subtrees are
// dropped entirely while ix:nonNumeric/ix:nonFraction wrappers are unwrapped to
// their visible text (the value stays, the tag vanishes). Non-breaking spaces
// and whitespace runs collapse to single spaces.
func RenderText(n *html.Node) string {
	return renderNodes(n, func(*html.Node) bool { return true })
}

// renderNodes renders the text of every node in root for which inRange reports
// true. The predicate selects a document-order span (see indexNodes), so a
// single walk over the whole document can render one section's text without the
// span needing to be a single subtree.
func renderNodes(root *html.Node, inRange func(*html.Node) bool) string {
	r := &textRenderer{inRange: inRange}
	r.walk(root)
	r.flush("")
	return strings.Join(r.blocks, "\n\n")
}

// textRenderer accumulates inline text into the current block and emits a block
// at each block-level boundary.
type textRenderer struct {
	inRange func(*html.Node) bool
	blocks  []string
	cur     strings.Builder
}

// flush emits the accumulated inline text as one block, prefixed (for list
// items) and whitespace-collapsed, dropping it when empty.
func (r *textRenderer) flush(prefix string) {
	text := collapseSpaces(r.cur.String())
	r.cur.Reset()
	if text == "" {
		return
	}
	r.blocks = append(r.blocks, prefix+text)
}

func (r *textRenderer) walk(n *html.Node) {
	if n.Type == html.TextNode {
		if r.inRange(n) {
			r.cur.WriteString(n.Data)
		}
		return
	}
	if n.Type == html.ElementNode {
		switch n.Data {
		case "ix:header", "ix:hidden", "script", "style":
			return // iXBRL machinery and non-content: skip the whole subtree
		}
		if isBlock(n.Data) {
			r.flush("")
			r.walkChildren(n)
			if n.Data == "li" {
				r.flush("- ")
			} else {
				r.flush("")
			}
			return
		}
		// A table cell is inline within its row's block, but adjacent cells must
		// not run their text together.
		if n.Data == "td" || n.Data == "th" {
			r.cur.WriteByte(' ')
		}
	}
	r.walkChildren(n)
}

func (r *textRenderer) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walk(c)
	}
}

// isBlock reports whether an element introduces a free-text block boundary.
func isBlock(tag string) bool {
	switch tag {
	case "p", "div", "li", "ul", "ol", "tr", "table",
		"section", "article", "header", "footer", "blockquote",
		"h1", "h2", "h3", "h4", "h5", "h6", "br", "hr":
		return true
	}
	return false
}
