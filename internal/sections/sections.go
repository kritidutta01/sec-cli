package sections

import (
	"errors"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/kritidutta01/sec-cli/internal/model"
	"github.com/kritidutta01/sec-cli/internal/router"
)

// ErrNoSections is returned when neither strategy can recover a section
// boundary — the document has no usable table of contents and no item-shaped
// headings.
var ErrNoSections = errors.New("sections: no sections detected")

// Section is one named part of a filing in document order. It is the thin
// working type the pipeline maps to a model.Section: it carries the same data
// (Item, Title, Kind, Text) plus the html node range and tables the pipeline
// needs to project statements into the section.
type Section struct {
	// Item is the 10-K item identifier, e.g. "1A".
	Item string `json:"item"`
	// Title is the canonical item title, e.g. "Risk Factors".
	Title string `json:"title"`
	// Heading is the heading text as it appeared at the section's start.
	Heading string `json:"heading"`
	// Kind separates narrative sections from the financial-statements section.
	Kind model.Kind `json:"kind"`
	// Text is the rendered free text of the section, iXBRL scaffolding removed.
	Text string `json:"text"`

	// StartNode is the section's first node; EndNode is the next section's start
	// (exclusive, nil for the last section). They bound the section in document
	// order so the pipeline can later bind statements to it. Tables holds the
	// <table> elements that fall inside the section. None of these serialize.
	StartNode *html.Node   `json:"-"`
	EndNode   *html.Node   `json:"-"`
	Tables    []*html.Node `json:"-"`
}

// Model projects the section to its serializable model.Section shape, dropping
// the working html node range. The financial tables inside the section are left
// for the pipeline to fill with projected/extracted model.Tables.
func (s Section) Model() model.Section {
	return model.Section{
		Item:  s.Item,
		Title: s.Title,
		Kind:  s.Kind,
		Text:  s.Text,
	}
}

// Result is the ordered section list, the partition confidence (migrated to
// model.Confidence in Phase 9), and the strategy that produced the boundaries
// ("toc", "heading", or "none").
type Result struct {
	Sections   []Section        `json:"sections"`
	Confidence model.Confidence `json:"confidence"`
	Strategy   string           `json:"strategy"`
}

// Partition splits a parsed filing into its named item sections. It tries the
// table-of-contents anchors first (high confidence: the filing names its own
// structure) and falls back to heading-text patterns (medium confidence). A
// section runs from its start to the next section's start in document order.
func Partition(doc *html.Node) (Result, error) {
	if doc == nil {
		return Result{}, ErrNoSections
	}

	pos, total := indexNodes(doc)

	starts, strategy, allResolved := tocStarts(doc)
	if len(starts) < 2 {
		if hs := headingStarts(doc); len(hs) >= len(starts) && len(hs) >= 1 {
			starts, strategy, allResolved = hs, "heading", false
		}
	}
	if len(starts) == 0 {
		return Result{}, ErrNoSections
	}

	sort.SliceStable(starts, func(i, j int) bool {
		return pos[starts[i].node] < pos[starts[j].node]
	})

	sections := make([]Section, 0, len(starts))
	for i, s := range starts {
		lo := pos[s.node]
		hi := total
		var end *html.Node
		if i+1 < len(starts) {
			hi = pos[starts[i+1].node]
			end = starts[i+1].node
		}
		inRange := func(x *html.Node) bool {
			p, ok := pos[x]
			return ok && p >= lo && p < hi
		}
		kind := model.KindNarrative
		if s.item == "8" {
			kind = model.KindFinancial
		}
		sections = append(sections, Section{
			Item:      s.item,
			Title:     s.title,
			Heading:   s.heading,
			Kind:      kind,
			Text:      renderNodes(doc, inRange),
			StartNode: s.node,
			EndNode:   end,
			Tables:    tablesInRange(doc, lo, hi, pos),
		})
	}

	return Result{
		Sections:   sections,
		Confidence: model.Confidence{Level: confidenceLevelFor(strategy, allResolved, len(sections))},
		Strategy:   strategy,
	}, nil
}

// SectionFormats classifies each section's tables as fact-tagged (router.IXBRL,
// where statement projection applies) or plain HTML (router.PlainHTML, where the
// layout fallback is the only option), for populating
// router.Decision.SectionFormats on a PartialIXBRL filing. The map is keyed by
// item id; sections with no tables are omitted. It returns nil when no section
// carries a table.
func SectionFormats(p Result) map[string]router.FilingFormat {
	out := make(map[string]router.FilingFormat)
	for _, s := range p.Sections {
		if len(s.Tables) == 0 {
			continue
		}
		format := router.PlainHTML
		for _, t := range s.Tables {
			if hasNumericFact(t) {
				format = router.IXBRL
				break
			}
		}
		out[s.Item] = format
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sectionStart is a detected section boundary: the matched item and the node
// where the section begins.
type sectionStart struct {
	item    string
	title   string
	heading string
	node    *html.Node
}

// confidenceLevelFor buckets the partition's confidence from the strategy used
// and how completely it resolved. The TOC path earns high only when every
// matched anchor resolved; the heading path is capped at medium; anything
// thinner is low.
func confidenceLevelFor(strategy string, allResolved bool, count int) string {
	level := "low"
	switch strategy {
	case "toc":
		if allResolved && count >= 2 {
			level = "high"
		} else {
			level = "medium"
		}
	case "heading":
		if count >= 2 {
			level = "medium"
		}
	}
	return level
}

// tocCandidate is one table-of-contents entry: the item its row names and the
// in-document node its link targets (nil when the target did not resolve).
type tocCandidate struct {
	item, title, row string
	target           *html.Node
}

// tocStarts recovers section boundaries from the table of contents: it picks the
// table carrying the most distinct item-naming anchors and turns each resolvable
// entry into a section start. allResolved is false when any matched anchor failed
// to resolve. It yields no starts when no table holds at least two resolvable
// items.
func tocStarts(doc *html.Node) (starts []sectionStart, strategy string, allResolved bool) {
	cands := pickTOC(collectTOCAnchors(doc))
	if cands == nil {
		return nil, "none", false
	}

	allResolved = true
	seen := make(map[string]bool)
	for _, c := range cands {
		if seen[c.item] {
			continue
		}
		if c.target == nil {
			allResolved = false
			continue
		}
		seen[c.item] = true
		heading := collapseSpaces(elementText(c.target))
		if heading == "" {
			heading = c.row
		}
		starts = append(starts, sectionStart{item: c.item, title: c.title, heading: heading, node: c.target})
	}
	if len(starts) < 2 {
		return nil, "none", false
	}
	return starts, "toc", allResolved
}

// collectTOCAnchors groups every in-document anchor that names an item by the
// table it lives in. Item naming is read from the whole table row, so an entry
// whose item number and title sit in separate cells is matched as a unit.
func collectTOCAnchors(doc *html.Node) map[*html.Node][]tocCandidate {
	byTable := make(map[*html.Node][]tocCandidate)
	eachElement(doc, func(a *html.Node) {
		if a.Data != "a" {
			return
		}
		href := attrVal(a, "href")
		if !strings.HasPrefix(href, "#") || len(href) <= 1 {
			return
		}
		tbl := tableAncestor(a)
		if tbl == nil {
			return
		}
		row := rowText(a)
		item, title, ok := classifyItem(row, false)
		if !ok {
			return
		}
		byTable[tbl] = append(byTable[tbl], tocCandidate{
			item: item, title: title, row: row, target: findTarget(doc, href[1:]),
		})
	})
	return byTable
}

// pickTOC returns the candidates of the table naming the most distinct items —
// the table of contents — or nil when no table names at least two.
func pickTOC(byTable map[*html.Node][]tocCandidate) []tocCandidate {
	var best []tocCandidate
	bestCount := 0
	for _, cs := range byTable {
		items := make(map[string]bool)
		for _, c := range cs {
			items[c.item] = true
		}
		if len(items) > bestCount {
			bestCount, best = len(items), cs
		}
	}
	if bestCount < 2 {
		return nil
	}
	return best
}

// headingStarts recovers section boundaries by scanning for heading-like nodes
// (bold text, <h1>–<h6>, or font-weight styling) whose text matches the item
// grammar. The first heading for each item, in document order, wins.
func headingStarts(doc *html.Node) []sectionStart {
	seen := make(map[string]bool)
	var starts []sectionStart
	eachElement(doc, func(n *html.Node) {
		if !isHeadingNode(n) {
			return
		}
		text := collapseSpaces(elementText(n))
		item, title, ok := classifyItem(text, true)
		if !ok || seen[item] {
			return
		}
		seen[item] = true
		starts = append(starts, sectionStart{item: item, title: title, heading: text, node: n})
	})
	return starts
}

// isHeadingNode reports whether a node renders as a heading: an <h1>–<h6>, a
// <b>/<strong>, or any element a bold font-weight style.
func isHeadingNode(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6", "b", "strong":
		return true
	}
	style := strings.ReplaceAll(strings.ToLower(attrVal(n, "style")), " ", "")
	return strings.Contains(style, "font-weight:bold") || strings.Contains(style, "font-weight:700")
}

// tablesInRange returns the <table> elements whose position falls in the section
// span [lo, hi).
func tablesInRange(doc *html.Node, lo, hi int, pos map[*html.Node]int) []*html.Node {
	var tables []*html.Node
	eachElement(doc, func(n *html.Node) {
		if n.Data != "table" {
			return
		}
		if p, ok := pos[n]; ok && p >= lo && p < hi {
			tables = append(tables, n)
		}
	})
	if len(tables) == 0 {
		return nil
	}
	return tables
}

// hasNumericFact reports whether a subtree contains an ix:nonFraction — a tagged
// numeric value, the signal that statement projection can apply to the table.
func hasNumericFact(n *html.Node) bool {
	found := false
	eachElement(n, func(e *html.Node) {
		if e.Data == "ix:nonfraction" {
			found = true
		}
	})
	return found
}

// tableAncestor returns the nearest enclosing <table> of n, or nil.
func tableAncestor(n *html.Node) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "table" {
			return p
		}
	}
	return nil
}

// rowText returns the text of an anchor's enclosing table row (so a TOC entry
// whose item number and title sit in separate cells is matched as a whole), or
// the anchor's own text when it is not in a row.
func rowText(a *html.Node) string {
	for p := a.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "tr" {
			return collapseSpaces(elementText(p))
		}
	}
	return collapseSpaces(elementText(a))
}

// findTarget resolves an in-document anchor target by id or by an <a name=>.
func findTarget(doc *html.Node, id string) *html.Node {
	var res *html.Node
	eachElement(doc, func(n *html.Node) {
		if res != nil {
			return
		}
		if attrVal(n, "id") == id {
			res = n
			return
		}
		if n.Data == "a" && attrVal(n, "name") == id {
			res = n
		}
	})
	return res
}

// itemSpec is one canonical 10-K item: its identifier, canonical title, and the
// lowercase title fragments used to match a TOC link or heading that names the
// item without an "Item N" prefix.
type itemSpec struct {
	id    string
	title string
	keys  []string
}

// itemSpecs is the canonical 10-K item map, in document order. Title fragments
// are deliberately specific to avoid cross-matching (e.g. Item 8 vs Item 15).
var itemSpecs = []itemSpec{
	{"1", "Business", []string{"business"}},
	{"1A", "Risk Factors", []string{"risk factors"}},
	{"1B", "Unresolved Staff Comments", []string{"unresolved staff comments"}},
	{"1C", "Cybersecurity", []string{"cybersecurity"}},
	{"2", "Properties", []string{"properties"}},
	{"3", "Legal Proceedings", []string{"legal proceedings"}},
	{"4", "Mine Safety Disclosures", []string{"mine safety"}},
	{"5", "Market for Registrant's Common Equity", []string{"market for registrant", "market for the registrant"}},
	{"6", "Selected Financial Data", []string{"[reserved]", "selected financial data"}},
	{"7", "Management's Discussion and Analysis", []string{"management's discussion", "md&a"}},
	{"7A", "Quantitative and Qualitative Disclosures About Market Risk", []string{"quantitative and qualitative"}},
	{"8", "Financial Statements and Supplementary Data", []string{"financial statements and supplementary", "financial statements"}},
	{"9", "Changes in and Disagreements with Accountants", []string{"changes in and disagreements"}},
	{"9A", "Controls and Procedures", []string{"controls and procedures"}},
	{"9B", "Other Information", []string{"other information"}},
	{"9C", "Disclosure Regarding Foreign Jurisdictions", []string{"foreign jurisdictions that prevent inspections"}},
	{"10", "Directors, Executive Officers and Corporate Governance", []string{"directors, executive officers"}},
	{"11", "Executive Compensation", []string{"executive compensation"}},
	{"12", "Security Ownership of Certain Beneficial Owners", []string{"security ownership"}},
	{"13", "Certain Relationships and Related Transactions", []string{"certain relationships"}},
	{"14", "Principal Accountant Fees and Services", []string{"principal accountant fees"}},
	{"15", "Exhibit and Financial Statement Schedules", []string{"exhibit and financial statement", "exhibits and financial statement"}},
	{"16", "Form 10-K Summary", []string{"form 10-k summary"}},
}

// itemByKey indexes itemSpecs by item identifier for the "Item N" regex path.
var itemByKey = func() map[string]itemSpec {
	m := make(map[string]itemSpec, len(itemSpecs))
	for _, s := range itemSpecs {
		m[s.id] = s
	}
	return m
}()

// eightKItemSpecs is the canonical 8-K item map. 8-K items use dotted
// sub-numbering (e.g. "Item 1.01") rather than the letter suffixes of 10-K.
var eightKItemSpecs = []itemSpec{
	{"1.01", "Entry into a Material Definitive Agreement", []string{"entry into a material definitive agreement"}},
	{"1.02", "Termination of a Material Definitive Agreement", []string{"termination of a material definitive agreement"}},
	{"1.03", "Bankruptcy or Receivership", []string{"bankruptcy or receivership"}},
	{"1.04", "Mine Safety - Reporting of Shutdowns and Patterns of Violations", []string{"mine safety"}},
	{"2.01", "Completion of Acquisition or Disposition of Assets", []string{"completion of acquisition or disposition"}},
	{"2.02", "Results of Operations and Financial Condition", []string{"results of operations and financial condition"}},
	{"2.03", "Creation of a Direct Financial Obligation", []string{"creation of a direct financial obligation"}},
	{"2.04", "Triggering Events That Accelerate or Increase a Direct Financial Obligation", []string{"triggering events that accelerate"}},
	{"2.05", "Cost Associated with Exit or Disposal Activities", []string{"cost associated with exit or disposal"}},
	{"2.06", "Material Impairments", []string{"material impairments"}},
	{"3.01", "Notice of Delisting or Failure to Satisfy a Continued Listing Rule", []string{"notice of delisting"}},
	{"3.02", "Unregistered Sales of Equity Securities", []string{"unregistered sales of equity"}},
	{"3.03", "Material Modification to Rights of Security Holders", []string{"material modification to rights"}},
	{"4.01", "Changes in Registrant's Certifying Accountant", []string{"changes in registrant's certifying accountant", "changes in registrant"}},
	{"4.02", "Non-Reliance on Previously Issued Financial Statements", []string{"non-reliance on previously issued"}},
	{"5.01", "Changes in Control of Registrant", []string{"changes in control of registrant"}},
	{"5.02", "Departure of Directors or Certain Officers", []string{"departure of directors or certain officers"}},
	{"5.03", "Amendments to Articles of Incorporation or Bylaws", []string{"amendments to articles of incorporation"}},
	{"5.04", "Temporary Suspension of Trading Under Registrant's Employee Benefit Plans", []string{"temporary suspension of trading"}},
	{"5.05", "Amendments to the Registrant's Code of Ethics", []string{"amendments to the registrant's code of ethics"}},
	{"5.06", "Change in Shell Company Status", []string{"change in shell company status"}},
	{"5.07", "Submission of Matters to a Vote of Security Holders", []string{"submission of matters to a vote"}},
	{"5.08", "Shareholder Director Nominations", []string{"shareholder director nominations"}},
	{"6.01", "ABS Informational and Computational Material", []string{"abs informational"}},
	{"7.01", "Regulation FD Disclosure", []string{"regulation fd disclosure"}},
	{"8.01", "Other Events", []string{"other events"}},
	{"9.01", "Financial Statements and Exhibits", []string{"financial statements and exhibits"}},
}

// eightKItemByKey indexes eightKItemSpecs by dotted id.
var eightKItemByKey = func() map[string]itemSpec {
	m := make(map[string]itemSpec, len(eightKItemSpecs))
	for _, s := range eightKItemSpecs {
		m[s.id] = s
	}
	return m
}()

var (
	itemPrefixRe    = regexp.MustCompile(`(?i)^item\s+(\d{1,2})\s*([a-c])?\b`)
	itemAnyRe       = regexp.MustCompile(`(?i)\bitem\s+(\d{1,2})\s*([a-c])?\b`)
	eightKPrefixRe  = regexp.MustCompile(`(?i)^item\s+(\d+\.\d+)\b`)
	eightKAnyRe     = regexp.MustCompile(`(?i)\bitem\s+(\d+\.\d+)\b`)
)

// classifyItem maps heading or TOC text to a canonical item. It first reads an
// "Item N" / "Item NA" form (10-K/10-Q) or "Item N.NN" form (8-K), then falls
// back to the canonical title fragments. When anchored is true the text must
// begin with the item (the heading path); otherwise the item may appear
// anywhere (the TOC path).
func classifyItem(text string, anchored bool) (id, title string, ok bool) {
	t := normalize(text)

	// Try 8-K dotted-item pattern first (non-overlapping with 10-K pattern).
	re8K := eightKAnyRe
	if anchored {
		re8K = eightKPrefixRe
	}
	if m := re8K.FindStringSubmatch(t); m != nil {
		if spec, found := eightKItemByKey[m[1]]; found {
			return spec.id, spec.title, true
		}
	}

	// 10-K / 10-Q item pattern.
	re := itemAnyRe
	if anchored {
		re = itemPrefixRe
	}
	if m := re.FindStringSubmatch(t); m != nil {
		key := m[1] + strings.ToUpper(m[2])
		if spec, found := itemByKey[key]; found {
			return spec.id, spec.title, true
		}
	}

	// Title-fragment fallback: try 8-K specs first, then 10-K specs.
	low := strings.ToLower(t)
	for _, specs := range [][]itemSpec{eightKItemSpecs, itemSpecs} {
		for _, s := range specs {
			for _, k := range s.keys {
				if anchored && strings.HasPrefix(low, k) {
					return s.id, s.title, true
				}
				if !anchored && strings.Contains(low, k) {
					return s.id, s.title, true
				}
			}
		}
	}
	return "", "", false
}

// normalize folds the curly apostrophes EDGAR emits to straight ones and
// collapses whitespace, so the item grammar matches regardless of typography.
func normalize(text string) string {
	text = strings.NewReplacer("’", "'", "‘", "'").Replace(text)
	return collapseSpaces(text)
}
