package ixbrl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// iXBRL fact element names, lowercased by the HTML tokenizer.
const (
	tagNonFraction = "ix:nonfraction"
	tagNonNumeric  = "ix:nonnumeric"
)

// Fact is one inline-XBRL fact: a numeric value (ix:nonFraction) or a text
// value (ix:nonNumeric) bound to a concept and a reporting context.
type Fact struct {
	// Concept is the taxonomy name, e.g. "us-gaap:RevenueFromContractWithCustomerExcludingAssessedTax".
	Concept string
	// ContextRef is the context ID the value is reported against.
	ContextRef string
	// UnitRef is the unit ID for numeric facts (e.g. "usd", "shares"); empty for text facts.
	UnitRef string
	// Numeric distinguishes ix:nonFraction (true) from ix:nonNumeric (false).
	Numeric bool
	// Raw is the fact's text content as it appeared in the document, before
	// decoration stripping or scaling.
	Raw string
	// Value is the parsed, scaled, signed numeric value. Meaningful only when Numeric is true.
	Value float64
	// Scale is the power of ten applied to the source digits (scale="6" → ×10^6).
	Scale int
	// Decimals is the source decimals attribute — informational precision, not magnitude.
	Decimals string
	// Negative records whether the value was negated (sign="-" or surrounding parentheses).
	Negative bool
	// Unparsed marks a numeric fact whose content could not be resolved to a
	// number — typically an exotic iXBRL transform (spelled-out words, dates)
	// on a narrative custom concept. Raw is preserved; Value is 0. Financial
	// statement values use the common dot/comma/dash transforms and parse cleanly.
	Unparsed bool
	// ID is the fact's id attribute (e.g. "f-66"). golang.org/x/net/html exposes
	// no line/column, so this is the fact's source locator for debugging.
	ID string
}

// ExtractFacts walks the document and returns every ix:nonFraction and
// ix:nonNumeric fact. Facts marked xsi:nil are skipped. Facts whose contextRef
// does not resolve against contexts are skipped — a dangling reference cannot be
// projected — so a valid filing (every fact's context defined) loses nothing.
// A numeric fact whose content can't be resolved to a number is kept with
// Unparsed set rather than aborting extraction of the whole filing.
func ExtractFacts(doc *html.Node, contexts map[string]Context) ([]Fact, error) {
	var facts []Fact

	collect := func(n *html.Node, numeric bool) {
		if strings.EqualFold(attrVal(n, "xsi:nil"), "true") {
			return
		}
		ctxRef := attrVal(n, "contextref")
		if _, ok := contexts[ctxRef]; !ok {
			return
		}
		f := Fact{
			Concept:    attrVal(n, "name"),
			ContextRef: ctxRef,
			UnitRef:    attrVal(n, "unitref"),
			Numeric:    numeric,
			Raw:        strings.TrimSpace(elementText(n)),
			Decimals:   attrVal(n, "decimals"),
			ID:         attrVal(n, "id"),
		}
		if numeric {
			v, scale, neg, err := parseNumericFact(n, f.Raw)
			if err != nil {
				f.Unparsed = true
			} else {
				f.Value, f.Scale, f.Negative = v, scale, neg
			}
		}
		facts = append(facts, f)
	}

	forEachElement(doc, tagNonFraction, func(n *html.Node) { collect(n, true) })
	forEachElement(doc, tagNonNumeric, func(n *html.Node) { collect(n, false) })

	return facts, nil
}

// parseNumericFact resolves an ix:nonFraction's raw content into a signed,
// scaled float. Sign comes from the sign="-" attribute or surrounding
// parentheses; magnitude from the digits times 10^scale. The decimals attribute
// is precision metadata and is deliberately not applied.
func parseNumericFact(n *html.Node, raw string) (value float64, scale int, negative bool, err error) {
	negative = attrVal(n, "sign") == "-"

	text := raw
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
		negative = true
		text = text[1 : len(text)-1]
	}

	format := attrVal(n, "format")
	if s := attrVal(n, "scale"); s != "" {
		scale, err = strconv.Atoi(s)
		if err != nil {
			return 0, 0, false, fmt.Errorf("parse scale %q: %w", s, err)
		}
	}

	// A dash (or a fixed-zero transform) is iXBRL's "nil/zero" for a numeric
	// cell — "—", "–", "-" all mean zero, not a parse error.
	if isDashZero(text) || strings.Contains(format, "fixed-zero") {
		return 0, scale, negative, nil
	}

	digits := normalizeNumber(text, format)
	if digits == "" {
		return 0, 0, false, fmt.Errorf("non-numeric content %q", raw)
	}
	mag, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("parse %q: %w", raw, err)
	}

	value = mag * math.Pow10(scale)
	if negative {
		value = -value
	}
	return value, scale, negative, nil
}

// isDashZero reports whether text is composed solely of dash characters (ASCII
// hyphen, en/em dash, or the Unicode minus sign), the iXBRL convention for a
// zero numeric value.
func isDashZero(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	for _, r := range text {
		switch r {
		case '-', '–', '—', '−':
		default:
			return false
		}
	}
	return true
}

// normalizeNumber strips currency symbols, whitespace and grouping separators
// from a displayed figure, returning a string ParseFloat can read. The iXBRL
// format transform decides which of comma/dot is the grouping separator;
// num-comma-decimal (European) is the only non-default handled, everything else
// is treated as num-dot-decimal (comma groups, dot decimal).
func normalizeNumber(text, format string) string {
	commaDecimal := strings.Contains(format, "comma-decimal")
	var b strings.Builder
	for _, r := range text {
		switch {
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '+':
			b.WriteRune(r)
		case r == '.':
			if commaDecimal {
				continue // grouping separator in European format
			}
			b.WriteRune(r)
		case r == ',':
			if commaDecimal {
				b.WriteRune('.') // decimal separator in European format
			}
			// else grouping separator — drop
		}
		// currency symbols, %, whitespace, nbsp: dropped
	}
	return b.String()
}
