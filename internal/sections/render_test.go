package sections

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// RenderText strips every ix:* tag and the ix:hidden subtree while preserving
// the visible values, collapses non-breaking spaces, and renders blocks as
// paragraphs and list items.
func TestRenderText_StripsIXBRL(t *testing.T) {
	doc := parseFixture(t, "ixbrl-narrative.htm")
	text := RenderText(firstElement(t, doc, "div"))

	require.Contains(t, text, "Total net sales were 391,035 million")
	require.Contains(t, text, "Timothy Cook")
	require.Contains(t, text, "- The business is seasonal.")
	require.Contains(t, text, "- Results may vary.")

	require.NotContains(t, text, "HIDDEN-SHOULD-NOT-APPEAR")
	require.NotContains(t, strings.ToLower(text), "ix:")
	require.NotContains(t, strings.ToLower(text), "nonfraction")
	require.NotContains(t, text, " ", "non-breaking spaces must be collapsed")
}

func TestRenderText_Paragraphs(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(
		`<div><p>First paragraph.</p><p>Second paragraph.</p></div>`,
	))
	require.NoError(t, err)
	text := RenderText(firstElement(t, doc, "div"))
	require.Equal(t, "First paragraph.\n\nSecond paragraph.", text)
}
