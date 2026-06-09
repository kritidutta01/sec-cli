package pipeline

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/kritidutta01/sec-cli/internal/cache"
	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/kritidutta01/sec-cli/internal/ixbrl"
	"github.com/kritidutta01/sec-cli/internal/router"
)

// StatementKeywords maps a statement selector to the role-URI fragments that
// identify that financial statement. A role matches if its URI (case-folded,
// punctuation removed) contains any fragment. Moved here from cmd/sec-cli so the
// command and the pipeline share one copy.
var StatementKeywords = map[string][]string{
	"income":        {"statementsofoperations", "statementsofincome"},
	"balance":       {"balancesheets", "financialposition"},
	"cashflow":      {"statementsofcashflows", "cashflow"},
	"equity":        {"stockholdersequity", "shareholdersequity"},
	"comprehensive": {"comprehensiveincome"},
}

// LoadRoles fetches a filing's presentation and label linkbases plus its schema,
// parses them, and returns every statement role. The directory listing comes
// from the EDGAR client; the linkbase and schema bytes flow through the caching
// fetch so a re-run serves them from the raw layer.
func LoadRoles(ctx context.Context, client *edgar.Client, fetch cache.FetchFunc, cik int64, filing edgar.Filing) ([]ixbrl.LinkbaseRole, error) {
	files, err := client.FilingFiles(ctx, cik, filing)
	if err != nil {
		return nil, err
	}
	preName := firstWithSuffix(files, "_pre.xml")
	labName := firstWithSuffix(files, "_lab.xml")
	if preName == "" || labName == "" {
		return nil, fmt.Errorf("pipeline: filing has no presentation/label linkbase (pre=%q lab=%q)", preName, labName)
	}

	preXML, err := fetch(ctx, edgar.CompanionURL(cik, filing, preName))
	if err != nil {
		return nil, fmt.Errorf("pipeline: fetch presentation linkbase: %w", err)
	}
	labXML, err := fetch(ctx, edgar.CompanionURL(cik, filing, labName))
	if err != nil {
		return nil, fmt.Errorf("pipeline: fetch label linkbase: %w", err)
	}

	var titles map[string]string
	if xsdName := firstSchema(files); xsdName != "" {
		if xsd, err := fetch(ctx, edgar.CompanionURL(cik, filing, xsdName)); err == nil {
			titles, _ = ixbrl.ParseRoleTitles(xsd)
		}
	}

	roles, err := ixbrl.ParseLinkbase(preXML, labXML, titles)
	if err != nil {
		return nil, err
	}
	return roles, nil
}

// SelectRole returns the first non-disclosure statement role whose URI matches a
// keyword. Roles for disclosure details, tables, and parentheticals are skipped
// so a keyword resolves to the primary statement.
func SelectRole(roles []ixbrl.LinkbaseRole, keywords []string) (ixbrl.LinkbaseRole, bool) {
	for _, r := range roles {
		u := foldRoleURI(r.RoleURI)
		if strings.Contains(u, "details") || strings.Contains(u, "tables") ||
			strings.Contains(u, "parenthetical") || strings.Contains(u, "disclosure") {
			continue
		}
		for _, k := range keywords {
			if strings.Contains(u, k) {
				return r, true
			}
		}
	}
	return ixbrl.LinkbaseRole{}, false
}

// IndexFetcher returns a router.Fetcher that resolves a filing-index document
// reference against EDGAR through the caching fetch. References may be absolute
// URLs, site-absolute paths, or filenames relative to the filing's Archives
// directory.
func IndexFetcher(fetch cache.FetchFunc, cik int64, filing edgar.Filing) router.Fetcher {
	base := strings.TrimSuffix(edgar.DocumentURL(cik, filing), filing.PrimaryDocument)
	return func(ctx context.Context, ref string) ([]byte, error) {
		var url string
		switch {
		case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
			url = ref
		case strings.HasPrefix(ref, "/"):
			url = "https://www.sec.gov" + ref
		default:
			url = base + ref
		}
		return fetch(ctx, url)
	}
}

// CollectTables returns every <table> element in the document, in document order.
// It backs the `table --layout` debug surface.
func CollectTables(doc *html.Node) []*html.Node {
	var tables []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			tables = append(tables, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return tables
}

// foldRoleURI lowercases a role URI's last path segment and drops everything but
// letters and digits, so "CONSOLIDATEDSTATEMENTSOFOPERATIONS" matches the
// keyword "statementsofoperations".
func foldRoleURI(uri string) string {
	tail := uri
	if i := strings.LastIndex(uri, "/"); i >= 0 {
		tail = uri[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(tail) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstWithSuffix(names []string, suffix string) string {
	for _, n := range names {
		if strings.HasSuffix(n, suffix) {
			return n
		}
	}
	return ""
}

// firstSchema returns the filing's own taxonomy schema (the top-level
// "<ticker>-<date>.xsd"), distinguished from linkbase files by its .xsd suffix.
func firstSchema(names []string) string {
	for _, n := range names {
		if strings.HasSuffix(n, ".xsd") {
			return n
		}
	}
	return ""
}
