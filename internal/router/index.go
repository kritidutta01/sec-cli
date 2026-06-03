package router

import (
	"strings"

	"golang.org/x/net/html"
)

// filingIndexRef inspects an HTML document for EDGAR's filing-index structure —
// a table summarized "Document Format Files" listing the filing's documents —
// and returns a reference to the primary document. The second return is false
// when the bytes are not a filing-index page.
func filingIndexRef(root *html.Node) (string, bool) {
	table := findNode(root, func(n *html.Node) bool {
		return n.Data == "table" &&
			strings.Contains(strings.ToLower(attr(n, "summary")), "document format files")
	})
	if table == nil {
		return "", false
	}

	// The first document link in that table is the primary document. Skip the
	// index page's own self-link (hrefs containing "-index").
	a := findNode(table, func(n *html.Node) bool {
		if n.Data != "a" {
			return false
		}
		href := strings.ToLower(attr(n, "href"))
		if strings.Contains(href, "-index") {
			return false
		}
		return strings.HasSuffix(href, ".htm") || strings.HasSuffix(href, ".html") || strings.HasSuffix(href, ".txt")
	})
	if a == nil {
		return "", false
	}
	return attr(a, "href"), true
}

// attr returns the value of the named attribute on an element node, or "".
func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// findNode returns the first element node in the tree (depth-first) matching
// pred, or nil.
func findNode(root *html.Node, pred func(*html.Node) bool) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && pred(n) {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}
