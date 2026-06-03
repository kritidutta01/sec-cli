// Package router classifies a filing's raw bytes into one of the EDGAR format
// eras. It is the single seam where "what is this byte stream?" is decided;
// parsers downstream receive a Decision and trust it. In v1.0 only IXBRL
// and PartialIXBRL are parsed — the other formats are classified so the CLI can
// refuse them cleanly rather than crash a parser on unexpected bytes.
package router

// FilingFormat is the era a filing's bytes belong to.
type FilingFormat int

const (
	// IXBRL is a fully inline-XBRL filing (≥ 80% of numeric cells fact-tagged).
	// Supported in v1.0.
	IXBRL FilingFormat = iota
	// PartialIXBRL tags only part of the document (10–80% of numeric cells).
	// Supported in v1.0 via a mixed parser path.
	PartialIXBRL
	// PlainHTML is a pre-iXBRL HTML filing (~2010–2018, small filers later).
	// Refused in v1.0; targeted for v1.1.
	PlainHTML
	// PlainText is a pre-2010 ASCII filing. Refused in v1.0; deferred to v1.2.
	PlainText
	// Unknown does not fit any recognized pattern. Always refused.
	Unknown
)

func (f FilingFormat) String() string {
	switch f {
	case IXBRL:
		return "IXBRL"
	case PartialIXBRL:
		return "PartialIXBRL"
	case PlainHTML:
		return "PlainHTML"
	case PlainText:
		return "PlainText"
	default:
		return "Unknown"
	}
}

// Supported reports whether the format is parseable in v1.0.
func (f FilingFormat) Supported() bool {
	return f == IXBRL || f == PartialIXBRL
}

// Decision is the router's verdict on a filing's bytes.
type Decision struct {
	// Format is the classified era.
	Format FilingFormat
	// PrimaryDocument is the classified bytes — post-index-resolution when the
	// input was a filing-index page.
	PrimaryDocument []byte
	// SectionFormats holds per-section formats for PartialIXBRL filings. It is
	// nil until section detection (Phase 8) is wired in.
	SectionFormats map[string]FilingFormat
	// ResolvedFromIndex is true when the input was a filing-index page and the
	// router followed it to the real primary document.
	ResolvedFromIndex bool
	// Warnings are non-fatal observations a parser should know about.
	Warnings []string
}
