// Package ixbrl parses inline-XBRL filings: the fact stream and its contexts
// (this file's siblings), and — in later phases — the presentation-linkbase
// projection, layout tables, and free-text sections.
//
// All parsing walks the *html.Node tree produced by golang.org/x/net/html. The
// HTML5 tokenizer lowercases every element and attribute name, so the XML
// namespaced names from the source ("xbrli:context", "ix:nonFraction",
// "contextRef") appear here lowercased ("xbrli:context", "ix:nonfraction",
// "contextref"). The name constants in this package are written accordingly.
package ixbrl

import (
	"strings"

	"golang.org/x/net/html"
)

// attrVal returns the value of the named attribute (lowercased key) on an
// element node, or "".
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

// findDescendant returns the first element node named name in n's subtree
// (depth-first, n itself excluded), or nil.
func findDescendant(n *html.Node, name string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if found != nil {
			return
		}
		if c != n && c.Type == html.ElementNode && c.Data == name {
			found = c
			return
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return found
}

// forEachElement calls fn for every element node named name in n's subtree, in
// document order.
func forEachElement(n *html.Node, name string, fn func(*html.Node)) {
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.ElementNode && c.Data == name {
			fn(c)
		}
		for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
}
