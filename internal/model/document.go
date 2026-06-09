package model

// Kind distinguishes the narrative item sections (Business, Risk Factors, MD&A,
// …) from the financial-statements section, where the fact-stream projection
// applies rather than free-text rendering.
type Kind string

// Section kinds.
const (
	KindNarrative Kind = "narrative"
	KindFinancial Kind = "financial"
)

// Document is the normalized, source-agnostic representation of one filing: the
// single value every package after Phase 8 produces, renders, caches, diffs, or
// scores. It is flat and JSON-first.
type Document struct {
	Metadata   Metadata  `json:"metadata"`
	Sections   []Section `json:"sections"`
	Statements []Table   `json:"statements"`
}

// Metadata identifies the filing and stamps the output: the company and filing
// coordinates, the period reported, the schema/parser versions, the extractor
// source, and the document-level confidence (combining statement and section
// confidences). Dates are plain YYYY-MM-DD strings.
type Metadata struct {
	Company       string     `json:"company,omitempty"`
	CIK           int64      `json:"cik,omitempty"`
	Ticker        string     `json:"ticker,omitempty"`
	Form          string     `json:"form,omitempty"`
	Accession     string     `json:"accession,omitempty"`
	FilingDate    string     `json:"filing_date,omitempty"`
	PeriodStart   string     `json:"period_start,omitempty"`
	PeriodEnd     string     `json:"period_end,omitempty"`
	SchemaVersion string     `json:"schema_version"`
	ParserVersion string     `json:"parser_version"`
	Source        Source     `json:"source"`
	Confidence    Confidence `json:"confidence"`
}

// Section is one named part of a filing: its item identifier and canonical
// title, whether it is narrative or the financial-statements section, the
// rendered free text, and the tables that fall inside it.
type Section struct {
	Item   string  `json:"item"`
	Title  string  `json:"title"`
	Kind   Kind    `json:"kind"`
	Text   string  `json:"text,omitempty"`
	Tables []Table `json:"tables,omitempty"`
}
