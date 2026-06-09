// Package sections partitions a filing into its named 10-K item sections and
// renders clean free text for each, with all inline-XBRL scaffolding removed.
//
// Like internal/ixbrl, it walks the *html.Node tree produced by
// golang.org/x/net/html, whose HTML5 tokenizer lowercases every element and
// attribute name — so the namespaced source names ("ix:header", "ix:hidden",
// "ix:nonFraction") appear here lowercased, and the constants are written to
// match.
package sections

import (
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// attrVal returns the value of the named attribute (lowercased key) on a node,
// or "".
func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// elementText returns the concatenated text of a node's subtree.
func elementText(n *html.Node) string {
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

// eachElement calls fn for every element node in root's subtree (root included
// when it is an element), in document order.
func eachElement(root *html.Node, fn func(*html.Node)) {
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			fn(n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

// indexNodes assigns each node its preorder (document-order) position and
// returns the position map plus the node count. A section's span is a half-open
// range of these positions, which lets a section's text and tables be selected
// regardless of how they nest in the tree.
func indexNodes(root *html.Node) (map[*html.Node]int, int) {
	pos := make(map[*html.Node]int)
	i := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		pos[n] = i
		i++
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return pos, i
}

// collapseSpaces trims the string and collapses every run of whitespace,
// including the non-breaking spaces EDGAR uses for indentation, to one space. It
// mirrors the normalization internal/ixbrl applies to table cells.
func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
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
