package ixbrl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// parseFixture parses a testdata HTML file into a node tree.
func parseFixture(t *testing.T, name string) *html.Node {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	doc, err := html.Parse(f)
	require.NoError(t, err)
	return doc
}

func TestParseContexts_Synthetic(t *testing.T) {
	contexts, err := ParseContexts(parseFixture(t, "synthetic.htm"))
	require.NoError(t, err)
	require.Len(t, contexts, 3)

	dur := contexts["dur-2024"]
	require.False(t, dur.IsInstant())
	require.True(t, dur.IsPrimary())
	require.Equal(t, "0001234567", dur.EntityCIK)
	require.Equal(t, "2024-01-01", dur.Start.Format(xbrlDateLayout))
	require.Equal(t, "2024-12-31", dur.End.Format(xbrlDateLayout))

	inst := contexts["inst-2024"]
	require.True(t, inst.IsInstant())
	require.Equal(t, "2024-12-31", inst.Start.Format(xbrlDateLayout))
	require.Equal(t, inst.Start, inst.End)

	seg := contexts["seg-product"]
	require.False(t, seg.IsPrimary())
	require.Len(t, seg.Segments, 1)
	require.Equal(t, "srt:ProductOrServiceAxis", seg.Segments[0].Dimension)
	require.Equal(t, "acme:WidgetsMember", seg.Segments[0].Member)
}

func TestParseContexts_AAPLExcerpt(t *testing.T) {
	contexts, err := ParseContexts(parseFixture(t, "aapl-fy2024-excerpt.htm"))
	require.NoError(t, err)

	c1, ok := contexts["c-1"]
	require.True(t, ok, "primary full-year context c-1 must be present")
	require.False(t, c1.IsInstant())
	require.True(t, c1.IsPrimary())
	require.Equal(t, "0000320193", c1.EntityCIK)
	require.Equal(t, "2023-10-01", c1.Start.Format(xbrlDateLayout))
	require.Equal(t, "2024-09-28", c1.End.Format(xbrlDateLayout))
}
