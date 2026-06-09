package sections

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"

	"github.com/kritidutta01/sec-cli/internal/model"
	"github.com/kritidutta01/sec-cli/internal/router"
)

// parseFixture reads and parses an HTML fixture from testdata.
func parseFixture(t *testing.T, name string) *html.Node {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	doc, err := html.Parse(bytes.NewReader(raw))
	require.NoError(t, err)
	return doc
}

// firstElement returns the first element with the given tag in doc.
func firstElement(t *testing.T, doc *html.Node, tag string) *html.Node {
	t.Helper()
	var found *html.Node
	eachElement(doc, func(n *html.Node) {
		if found == nil && n.Data == tag {
			found = n
		}
	})
	require.NotNil(t, found, "fixture must contain a <%s>", tag)
	return found
}

// items returns the partition's item ids in order.
func items(p Result) []string {
	ids := make([]string, len(p.Sections))
	for i, s := range p.Sections {
		ids[i] = s.Item
	}
	return ids
}

func sectionByItem(p Result, id string) (Section, bool) {
	for _, s := range p.Sections {
		if s.Item == id {
			return s, true
		}
	}
	return Section{}, false
}

// The TOC-anchor path recovers the full ordered item list at high confidence.
func TestPartition_TOC(t *testing.T) {
	p, err := Partition(parseFixture(t, "toc-10k.htm"))
	require.NoError(t, err)

	require.Equal(t, []string{"1", "1A", "7", "8"}, items(p))
	require.Equal(t, "high", p.Confidence.Level)
	require.Equal(t, "toc", p.Strategy)

	risk, ok := sectionByItem(p, "1A")
	require.True(t, ok)
	require.Equal(t, "Risk Factors", risk.Title)
	require.Equal(t, model.KindNarrative, risk.Kind)
	require.Contains(t, risk.Text, "Item 1A. Risk Factors")
	require.Contains(t, risk.Text, "subject to numerous risks")
	// The next section's text must not leak into this one.
	require.NotContains(t, risk.Text, "Net sales increased")

	fin, ok := sectionByItem(p, "8")
	require.True(t, ok)
	require.Equal(t, model.KindFinancial, fin.Kind)
}

// The heading-pattern fallback recovers items when no TOC exists, at medium
// confidence.
func TestPartition_HeadingFallback(t *testing.T) {
	p, err := Partition(parseFixture(t, "notoc-10k.htm"))
	require.NoError(t, err)

	require.Equal(t, []string{"1", "1A", "7", "8"}, items(p))
	require.Equal(t, "medium", p.Confidence.Level)
	require.Equal(t, "heading", p.Strategy)

	biz, ok := sectionByItem(p, "1")
	require.True(t, ok)
	require.Contains(t, biz.Text, "consumer electronics")
	require.NotContains(t, biz.Text, "supply concentration")
}

// SectionFormats marks a section with tagged numeric facts as IXBRL and a
// section with only a plain table as PlainHTML.
func TestSectionFormats(t *testing.T) {
	p, err := Partition(parseFixture(t, "toc-10k.htm"))
	require.NoError(t, err)

	formats := SectionFormats(p)
	require.NotNil(t, formats)
	require.Equal(t, router.IXBRL, formats["8"], "Item 8 has an ix:nonFraction table")
	require.Equal(t, router.PlainHTML, formats["7"], "Item 7 has a plain table")
	// Sections without tables are omitted.
	_, hasBusiness := formats["1"]
	require.False(t, hasBusiness)
}

// Section.Model projects the working section to its serializable model.Section,
// carrying the data and dropping the html node range.
func TestSectionModel(t *testing.T) {
	p, err := Partition(parseFixture(t, "toc-10k.htm"))
	require.NoError(t, err)

	risk, ok := sectionByItem(p, "1A")
	require.True(t, ok)

	m := risk.Model()
	require.Equal(t, "1A", m.Item)
	require.Equal(t, "Risk Factors", m.Title)
	require.Equal(t, model.KindNarrative, m.Kind)
	require.Equal(t, risk.Text, m.Text)
	require.Nil(t, m.Tables, "projected tables are filled by the pipeline, not the partitioner")
}

func TestPartition_Empty(t *testing.T) {
	doc, err := html.Parse(bytes.NewReader([]byte("<html><body><p>nothing here</p></body></html>")))
	require.NoError(t, err)
	_, err = Partition(doc)
	require.ErrorIs(t, err, ErrNoSections)
}

func TestClassifyItem(t *testing.T) {
	cases := []struct {
		text     string
		anchored bool
		id       string
		ok       bool
	}{
		{"Item 1. Business", true, "1", true},
		{"Item 1A. Risk Factors", true, "1A", true},
		{"ITEM 7A. Quantitative and Qualitative Disclosures", true, "7A", true},
		{"Risk Factors", true, "1A", true},
		{"see Item 1A above for details", true, "", false}, // not a heading
		{"see Item 1A above for details", false, "1A", true},
		{"Random heading", true, "", false},
	}
	for _, c := range cases {
		id, _, ok := classifyItem(c.text, c.anchored)
		require.Equal(t, c.ok, ok, c.text)
		if c.ok {
			require.Equal(t, c.id, id, c.text)
		}
	}
}
