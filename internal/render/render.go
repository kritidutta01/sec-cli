// Package render turns a model.Document into one of three serializations behind
// a single Renderer interface: canonical JSON (the schema-stable contract the
// cache and Python wrapper read), Markdown (the LLM- and human-facing view), and
// plain text. Renderers are total functions over the model — they never
// re-extract or re-decide anything, and a null cell renders as JSON null, an
// empty Markdown cell, or a blank text column, never as 0.
package render

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kritidutta01/sec-cli/internal/model"
)

// Renderer writes one serialization of a Document to w.
type Renderer interface {
	Render(doc *model.Document, w io.Writer) error
}

// ErrUnknownFormat is returned by For when the format name is not recognized.
var ErrUnknownFormat = errors.New("render: unknown format")

// For returns the renderer for a format name: "json" (the default), "md" /
// "markdown", or "text" / "txt".
func For(format string) (Renderer, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return JSON{}, nil
	case "md", "markdown":
		return Markdown{}, nil
	case "text", "txt":
		return Text{}, nil
	default:
		return nil, fmt.Errorf("%w: %q (choose json, md, text)", ErrUnknownFormat, format)
	}
}

// formatValue renders a cell value: a missing value (nil) is the empty string —
// never "0" — and a present value is its shortest exact decimal.
func formatValue(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// docHeading builds a one-line document heading from whatever metadata is
// present, or "" when there is nothing to show.
func docHeading(m model.Metadata) string {
	var parts []string
	if m.Company != "" {
		parts = append(parts, m.Company)
	}
	if m.Ticker != "" {
		parts = append(parts, "("+m.Ticker+")")
	}
	if m.Form != "" {
		parts = append(parts, m.Form)
	}
	head := strings.Join(parts, " ")
	if m.PeriodEnd != "" {
		if head != "" {
			head += " "
		}
		head += "— period ending " + m.PeriodEnd
	}
	return head
}

// sectionTitle renders a section's heading text ("Item 1A — Risk Factors", or
// just the title when there is no item id).
func sectionTitle(s model.Section) string {
	if s.Item == "" {
		return s.Title
	}
	return "Item " + s.Item + " — " + s.Title
}
