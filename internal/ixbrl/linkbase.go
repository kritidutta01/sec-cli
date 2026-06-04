package ixbrl

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// XBRL standard label-role URIs. The presentation arc's preferredLabel points at
// one of these (or a custom role); when it does not resolve, parsing falls back
// to the standard label, then to the concept's humanised name.
const (
	roleStandardLabel = "http://www.xbrl.org/2003/role/label"
	roleTerseLabel    = "http://www.xbrl.org/2003/role/terseLabel"
	roleVerboseLabel  = "http://www.xbrl.org/2003/role/verboseLabel"
	roleTotalLabel    = "http://www.xbrl.org/2003/role/totalLabel"

	// A concept shown as an opening/closing balance inside a duration column
	// (cash flow beginning/ending balances) carries one of these preferred-label
	// roles. The value is an instant fact at the column's period boundary.
	rolePeriodStartLabel = "http://www.xbrl.org/2003/role/periodStartLabel"
	rolePeriodEndLabel   = "http://www.xbrl.org/2003/role/periodEndLabel"
)

// LinkbaseRole is one statement's presentation tree: an ordered, depth-tagged
// list of concepts with display labels already resolved from the label linkbase.
// Concepts appear in presentation (depth-first, arc-order) order, which is the
// row order of the projected table.
type LinkbaseRole struct {
	// RoleURI identifies the statement, e.g.
	// "http://www.apple.com/role/CONSOLIDATEDSTATEMENTSOFOPERATIONS".
	RoleURI string
	// Title is the human-readable statement name from the schema's role
	// definition (e.g. "CONSOLIDATED STATEMENTS OF OPERATIONS"); when the schema
	// is unavailable it is derived from the role URI's final path segment.
	Title string
	// Concepts is the ordered concept list for the statement.
	Concepts []RoleConcept
}

// RoleConcept is one node in a statement's presentation tree.
type RoleConcept struct {
	// Name is the taxonomy concept in prefix:Local form, e.g. "us-gaap:GrossProfit".
	Name string
	// Label is the resolved display text for this concept under its preferred
	// label role — the filing's own wording ("Net sales", "Total operating
	// expenses"), not the generic taxonomy label.
	Label string
	// PreferredLabel is the label-role URI the presentation arc requested, or ""
	// for a root (which has no incoming arc).
	PreferredLabel string
	// Depth is the nesting level in the presentation tree (root = 0).
	Depth int
	// HasChildren is true when the concept is a parent in the tree.
	HasChildren bool
	// Structural marks abstract and dimensional grouping nodes (Abstract, Table,
	// Axis, Domain, Member, LineItems) that organise the statement but never
	// carry a fact. Projection keeps them for structure but omits them as rows.
	Structural bool
}

// linkbaseXML is the subset of an XBRL linkbase document we read. Element and
// attribute names are matched by local name, so the link:/xlink: prefixes in the
// source resolve here regardless of the exact namespace declarations.
type linkbaseXML struct {
	PresLinks  []presLinkXML  `xml:"presentationLink"`
	LabelLinks []labelLinkXML `xml:"labelLink"`
}

type presLinkXML struct {
	Role string       `xml:"role,attr"`
	Locs []locXML     `xml:"loc"`
	Arcs []presArcXML `xml:"presentationArc"`
}

type locXML struct {
	Href  string `xml:"href,attr"`
	Label string `xml:"label,attr"`
}

type presArcXML struct {
	From           string  `xml:"from,attr"`
	To             string  `xml:"to,attr"`
	Order          float64 `xml:"order,attr"`
	PreferredLabel string  `xml:"preferredLabel,attr"`
}

type labelLinkXML struct {
	Locs   []locXML      `xml:"loc"`
	Arcs   []labelArcXML `xml:"labelArc"`
	Labels []labelXML    `xml:"label"`
}

type labelArcXML struct {
	From string `xml:"from,attr"`
	To   string `xml:"to,attr"`
}

type labelXML struct {
	Label string `xml:"label,attr"`
	Role  string `xml:"role,attr"`
	Text  string `xml:",chardata"`
}

// ParseLinkbase parses a filing's presentation and label linkbases into one
// LinkbaseRole per statement. The presentation linkbase supplies concept order
// and parent-child structure; the label linkbase supplies the display text,
// resolved against each arc's preferred label role. roleTitles maps a role URI
// to its schema-defined title and may be nil — a missing title is derived from
// the role URI.
func ParseLinkbase(presentationXML, labelXML []byte, roleTitles map[string]string) ([]LinkbaseRole, error) {
	var pres linkbaseXML
	if err := xml.Unmarshal(presentationXML, &pres); err != nil {
		return nil, fmt.Errorf("ixbrl: parse presentation linkbase: %w", err)
	}

	var lab linkbaseXML
	if err := xml.Unmarshal(labelXML, &lab); err != nil {
		return nil, fmt.Errorf("ixbrl: parse label linkbase: %w", err)
	}
	labels := resolveLabels(lab.LabelLinks)

	roles := make([]LinkbaseRole, 0, len(pres.PresLinks))
	for _, pl := range pres.PresLinks {
		role := buildRole(pl, labels, roleTitles)
		roles = append(roles, role)
	}
	return roles, nil
}

// labelMap maps a concept (prefix:Local) to its label text per label-role URI.
type labelMap map[string]map[string]string

// resolveLabels walks the label linkbases and links each concept (via its loc →
// labelArc → label chain) to the text of each label role it defines.
func resolveLabels(links []labelLinkXML) labelMap {
	out := make(labelMap)
	for _, ll := range links {
		locConcept := make(map[string]string, len(ll.Locs)) // xlink:label → concept
		for _, l := range ll.Locs {
			locConcept[l.Label] = conceptFromHref(l.Href)
		}
		// resource xlink:label → concept, via the labelArc from a loc.
		resConcept := make(map[string]string, len(ll.Arcs))
		for _, a := range ll.Arcs {
			if c, ok := locConcept[a.From]; ok {
				resConcept[a.To] = c
			}
		}
		for _, lbl := range ll.Labels {
			concept, ok := resConcept[lbl.Label]
			if !ok {
				continue
			}
			roles := out[concept]
			if roles == nil {
				roles = make(map[string]string)
				out[concept] = roles
			}
			roles[lbl.Role] = strings.TrimSpace(lbl.Text)
		}
	}
	return out
}

// label returns the display text for a concept under its preferred role,
// degrading deterministically: preferred → standard → terse → verbose → any
// defined label → a humanised concept name.
func (m labelMap) label(concept, preferred string) string {
	roles := m[concept]
	if len(roles) == 0 {
		return humanizeConcept(concept)
	}
	for _, r := range []string{preferred, roleStandardLabel, roleTerseLabel, roleVerboseLabel} {
		if r == "" {
			continue
		}
		if t := roles[r]; t != "" {
			return t
		}
	}
	// Any remaining role, chosen deterministically for stable output.
	keys := make([]string, 0, len(roles))
	for k := range roles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if roles[k] != "" {
			return roles[k]
		}
	}
	return humanizeConcept(concept)
}

// buildRole turns one presentationLink into a LinkbaseRole by walking the
// parent-child arcs depth-first in arc order.
func buildRole(pl presLinkXML, labels labelMap, roleTitles map[string]string) LinkbaseRole {
	locConcept := make(map[string]string, len(pl.Locs)) // xlink:label → concept
	for _, l := range pl.Locs {
		locConcept[l.Label] = conceptFromHref(l.Href)
	}

	children := make(map[string][]presArcXML) // parent loc label → child arcs
	incoming := make(map[string]presArcXML)   // child loc label → its arc (for preferredLabel)
	for _, a := range pl.Arcs {
		children[a.From] = append(children[a.From], a)
		incoming[a.To] = a
	}
	for from := range children {
		arcs := children[from]
		sort.SliceStable(arcs, func(i, j int) bool { return arcs[i].Order < arcs[j].Order })
		children[from] = arcs
	}

	role := LinkbaseRole{RoleURI: pl.Role, Title: roleTitle(pl.Role, roleTitles)}

	visited := make(map[string]bool)
	var visit func(locLabel string, depth int)
	visit = func(locLabel string, depth int) {
		if visited[locLabel] {
			return // guard against a malformed cyclic linkbase
		}
		visited[locLabel] = true

		concept := locConcept[locLabel]
		arc := incoming[locLabel]
		role.Concepts = append(role.Concepts, RoleConcept{
			Name:           concept,
			Label:          labels.label(concept, arc.PreferredLabel),
			PreferredLabel: arc.PreferredLabel,
			Depth:          depth,
			HasChildren:    len(children[locLabel]) > 0,
			Structural:     isStructural(concept),
		})
		for _, c := range children[locLabel] {
			visit(c.To, depth+1)
		}
	}

	// Roots are loc labels that no arc points at, visited in document order.
	for _, l := range pl.Locs {
		if _, isChild := incoming[l.Label]; !isChild {
			visit(l.Label, 0)
		}
	}
	return role
}

// conceptFromHref turns a locator href fragment into a prefix:Local concept name:
// "...us-gaap-2024.xsd#us-gaap_GrossProfit" → "us-gaap:GrossProfit". The element
// id replaces the namespace colon with an underscore, so only the first
// underscore (the prefix separator) is converted back.
func conceptFromHref(href string) string {
	frag := href
	if i := strings.LastIndex(href, "#"); i >= 0 {
		frag = href[i+1:]
	}
	if i := strings.Index(frag, "_"); i >= 0 {
		return frag[:i] + ":" + frag[i+1:]
	}
	return frag
}

// structuralSuffixes name the presentation nodes that organise a statement but
// never carry a fact: abstract groupings and the dimensional hypercube parts.
var structuralSuffixes = []string{"Abstract", "Table", "Axis", "Domain", "Member", "LineItems"}

func isStructural(concept string) bool {
	local := concept
	if i := strings.Index(concept, ":"); i >= 0 {
		local = concept[i+1:]
	}
	for _, s := range structuralSuffixes {
		if strings.HasSuffix(local, s) {
			return true
		}
	}
	return false
}

// roleTitle returns the schema-defined title for a role URI when known,
// otherwise a title derived from the URI's final path segment.
func roleTitle(roleURI string, titles map[string]string) string {
	if t := titles[roleURI]; t != "" {
		return t
	}
	tail := roleURI
	if i := strings.LastIndex(roleURI, "/"); i >= 0 {
		tail = roleURI[i+1:]
	}
	return tail
}

// humanizeConcept renders a concept name as a fallback label when the label
// linkbase defines none: "us-gaap:GrossProfit" → "Gross Profit".
func humanizeConcept(concept string) string {
	local := concept
	if i := strings.Index(concept, ":"); i >= 0 {
		local = concept[i+1:]
	}
	var b strings.Builder
	for i, r := range local {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// roleTypeXML and ParseRoleTitles read a filing schema's roleType definitions,
// mapping each role URI to its human-readable title.
type schemaXML struct {
	RoleTypes []roleTypeXML `xml:"annotation>appinfo>roleType"`
}

type roleTypeXML struct {
	RoleURI    string `xml:"roleURI,attr"`
	Definition string `xml:"definition"`
}

// ParseRoleTitles extracts role-URI → title pairs from a filing schema (.xsd).
// A roleType definition looks like "9952151 - Statement - CONSOLIDATED
// STATEMENTS OF OPERATIONS"; the title is the segment after the last " - ".
func ParseRoleTitles(schemaXSD []byte) (map[string]string, error) {
	var s schemaXML
	if err := xml.Unmarshal(schemaXSD, &s); err != nil {
		return nil, fmt.Errorf("ixbrl: parse schema role types: %w", err)
	}
	out := make(map[string]string, len(s.RoleTypes))
	for _, rt := range s.RoleTypes {
		def := strings.TrimSpace(rt.Definition)
		if i := strings.LastIndex(def, " - "); i >= 0 {
			def = strings.TrimSpace(def[i+3:])
		}
		if def != "" {
			out[rt.RoleURI] = def
		}
	}
	return out, nil
}
