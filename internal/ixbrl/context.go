package ixbrl

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// xbrlDateLayout is the ISO date format used in xbrli:period elements.
const xbrlDateLayout = "2006-01-02"

// iXBRL context element/attribute names, lowercased by the HTML tokenizer.
const (
	tagContext        = "xbrli:context"
	tagIdentifier     = "xbrli:identifier"
	tagPeriod         = "xbrli:period"
	tagStartDate      = "xbrli:startdate"
	tagEndDate        = "xbrli:enddate"
	tagInstant        = "xbrli:instant"
	tagExplicitMember = "xbrldi:explicitmember"
)

// Segment is one explicit dimension on a context, e.g. a business segment or a
// class of stock. Primary (consolidated) contexts have no segments.
type Segment struct {
	Dimension string // e.g. us-gaap:StatementClassOfStockAxis
	Member    string // e.g. aapl:A1.625NotesDue2026Member
}

// Context is a resolved xbrli:context: the reporting period and entity a fact's
// contextRef points at. A duration context has distinct Start/End; an instant
// context has Start == End.
type Context struct {
	ID        string
	Start     time.Time
	End       time.Time
	EntityCIK string // raw identifier text, e.g. "0000320193"
	Segments  []Segment
}

// IsInstant reports whether the context is a point in time (balance-sheet date)
// rather than a duration (income-statement period).
func (c Context) IsInstant() bool {
	return c.Start.Equal(c.End)
}

// IsPrimary reports whether the context is unscoped by any segment dimension —
// the consolidated figures, as opposed to a per-segment or per-class breakdown.
func (c Context) IsPrimary() bool {
	return len(c.Segments) == 0
}

// ParseContexts builds the context map for an iXBRL document by reading every
// xbrli:context in the ix:header resources. The key is the context ID that
// facts reference via contextRef.
func ParseContexts(doc *html.Node) (map[string]Context, error) {
	out := make(map[string]Context)
	var firstErr error
	forEachElement(doc, tagContext, func(n *html.Node) {
		c, err := parseContext(n)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return
		}
		out[c.ID] = c
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func parseContext(n *html.Node) (Context, error) {
	id := attrVal(n, "id")
	if id == "" {
		return Context{}, fmt.Errorf("ixbrl: context element missing id")
	}
	c := Context{ID: id}

	if ident := findDescendant(n, tagIdentifier); ident != nil {
		c.EntityCIK = strings.TrimSpace(elementText(ident))
	}

	period := findDescendant(n, tagPeriod)
	if period == nil {
		return Context{}, fmt.Errorf("ixbrl: context %q missing period", id)
	}
	if inst := findDescendant(period, tagInstant); inst != nil {
		t, err := parseXBRLDate(inst)
		if err != nil {
			return Context{}, fmt.Errorf("ixbrl: context %q instant: %w", id, err)
		}
		c.Start, c.End = t, t
	} else {
		start := findDescendant(period, tagStartDate)
		end := findDescendant(period, tagEndDate)
		if start == nil || end == nil {
			return Context{}, fmt.Errorf("ixbrl: context %q has neither instant nor start/end dates", id)
		}
		s, err := parseXBRLDate(start)
		if err != nil {
			return Context{}, fmt.Errorf("ixbrl: context %q start date: %w", id, err)
		}
		e, err := parseXBRLDate(end)
		if err != nil {
			return Context{}, fmt.Errorf("ixbrl: context %q end date: %w", id, err)
		}
		c.Start, c.End = s, e
	}

	forEachElement(n, tagExplicitMember, func(m *html.Node) {
		c.Segments = append(c.Segments, Segment{
			Dimension: attrVal(m, "dimension"),
			Member:    strings.TrimSpace(elementText(m)),
		})
	})

	return c, nil
}

func parseXBRLDate(n *html.Node) (time.Time, error) {
	return time.Parse(xbrlDateLayout, strings.TrimSpace(elementText(n)))
}
