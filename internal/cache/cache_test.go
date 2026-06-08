package cache

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kritidutta01/sec-cli/internal/model"
)

// openTemp opens a cache in a fresh temp dir and registers its cleanup.
func openTemp(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRawLayer_PutGetMiss(t *testing.T) {
	c := openTemp(t)

	_, ok := c.GetRaw("https://example.com/a.htm")
	require.False(t, ok, "empty cache must miss")

	body := []byte("<html>filing bytes</html>")
	require.NoError(t, c.PutRaw("https://example.com/a.htm", body))

	got, ok := c.GetRaw("https://example.com/a.htm")
	require.True(t, ok, "stored URL must hit")
	require.Equal(t, body, got)

	_, ok = c.GetRaw("https://example.com/other.htm")
	require.False(t, ok, "unstored URL must miss")
}

func TestRawLayer_Overwrite(t *testing.T) {
	c := openTemp(t)
	url := "https://example.com/a.htm"
	require.NoError(t, c.PutRaw(url, []byte("v1")))
	require.NoError(t, c.PutRaw(url, []byte("v2")))
	got, ok := c.GetRaw(url)
	require.True(t, ok)
	require.Equal(t, []byte("v2"), got)
}

// sampleDoc is a Document with dated period columns, to prove the parsed layer
// round-trips through JSON including the YYYY-MM-DD date marshaling.
func sampleDoc() *model.Document {
	f := func(v float64) *float64 { return &v }
	return &model.Document{
		Metadata: model.Metadata{
			Company:       "Acme Corp",
			Accession:     "0000000000-24-000001",
			PeriodEnd:     "2024-12-31",
			SchemaVersion: model.SchemaVersion,
			ParserVersion: model.ParserVersion,
		},
		Statements: []model.Table{{
			SchemaVersion: model.SchemaVersion,
			Title:         "Income Statement",
			Columns: []model.Column{{
				Label:       "FY2024",
				PeriodStart: mustDate("2024-01-01"),
				PeriodEnd:   mustDate("2024-12-31"),
			}},
			Rows: []model.Row{
				{Label: "Revenue", Concept: "us-gaap:Revenues", Type: model.RowData, Values: []*float64{f(1000)}},
				{Label: "R&D", Type: model.RowData, Values: []*float64{nil}},
			},
		}},
	}
}

func TestParsedLayer_PutGetRoundTrip(t *testing.T) {
	c := openTemp(t)
	acc := "0000000000-24-000001"

	_, ok := c.GetDocument(acc)
	require.False(t, ok, "empty cache must miss")

	require.NoError(t, c.PutDocument(acc, sampleDoc()))

	got, ok := c.GetDocument(acc)
	require.True(t, ok, "stored accession must hit")
	require.Equal(t, "Acme Corp", got.Metadata.Company)
	require.Len(t, got.Statements, 1)

	col := got.Statements[0].Columns[0]
	require.Equal(t, "FY2024", col.Label)
	require.Equal(t, "2024-12-31", col.PeriodEnd.Format("2006-01-02"), "period dates must round-trip")

	rnd := got.Statements[0].Rows[1]
	require.Equal(t, "R&D", rnd.Label)
	require.Nil(t, rnd.Values[0], "absent cell must round-trip as null, never 0")
}

// TestParsedLayer_VersionInvalidation proves a parserVersion change misses the
// parsed layer while the raw layer (which is version-agnostic) still hits.
func TestParsedLayer_VersionInvalidation(t *testing.T) {
	c := openTemp(t)
	acc := "0000000000-24-000001"

	// Write a parsed row under a stale parser version directly, and a raw row.
	_, err := c.db.Exec(
		"INSERT INTO parsed (accession, parser_version, doc, created_at) VALUES (?, ?, ?, 0)",
		acc, "0.0.1-old", []byte(`{"metadata":{}}`),
	)
	require.NoError(t, err)
	require.NoError(t, c.PutRaw("https://example.com/a.htm", []byte("bytes")))

	// The current ParserVersion does not match the stale row → miss.
	_, ok := c.GetDocument(acc)
	require.False(t, ok, "document under a different parser version must miss")

	// Raw bytes stay warm regardless of parser version.
	_, ok = c.GetRaw("https://example.com/a.htm")
	require.True(t, ok, "raw layer is version-agnostic and must still hit")

	// Writing under the current version then hits.
	require.NoError(t, c.PutDocument(acc, sampleDoc()))
	_, ok = c.GetDocument(acc)
	require.True(t, ok)
}

// TestFetching_FetchesOnce asserts the caching fetch wrapper calls the
// underlying fetch exactly once for two reads of the same URL.
func TestFetching_FetchesOnce(t *testing.T) {
	c := openTemp(t)

	var calls atomic.Int64
	underlying := func(_ context.Context, _ string) ([]byte, error) {
		calls.Add(1)
		return []byte("payload"), nil
	}
	fetch := c.Fetching(underlying)

	b1, err := fetch(context.Background(), "https://example.com/a.htm")
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), b1)

	b2, err := fetch(context.Background(), "https://example.com/a.htm")
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), b2)

	require.Equal(t, int64(1), calls.Load(), "second read must be served from cache")
}

func TestFetching_PropagatesError(t *testing.T) {
	c := openTemp(t)
	wantErr := context.Canceled
	fetch := c.Fetching(func(_ context.Context, _ string) ([]byte, error) {
		return nil, wantErr
	})
	_, err := fetch(context.Background(), "https://example.com/a.htm")
	require.ErrorIs(t, err, wantErr)

	// A failed fetch must not be cached.
	_, ok := c.GetRaw("https://example.com/a.htm")
	require.False(t, ok)
}

func TestClear(t *testing.T) {
	c := openTemp(t)
	require.NoError(t, c.PutRaw("https://example.com/a.htm", []byte("bytes")))
	require.NoError(t, c.PutDocument("0000000000-24-000001", sampleDoc()))

	require.NoError(t, c.Clear())

	_, ok := c.GetRaw("https://example.com/a.htm")
	require.False(t, ok, "raw layer cleared")
	_, ok = c.GetDocument("0000000000-24-000001")
	require.False(t, ok, "parsed layer cleared")
}

// TestMigration_ReopenKeepsVersion opens, closes, and reopens a cache file and
// asserts the schema version is correct and data survives.
func TestMigration_ReopenKeepsVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")

	c1, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, c1.PutRaw("https://example.com/a.htm", []byte("bytes")))

	var v int
	require.NoError(t, c1.db.QueryRow("PRAGMA user_version").Scan(&v))
	require.Equal(t, schemaVersion, v)
	require.NoError(t, c1.Close())

	c2, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c2.Close() })

	require.NoError(t, c2.db.QueryRow("PRAGMA user_version").Scan(&v))
	require.Equal(t, schemaVersion, v, "reopen keeps user_version")

	got, ok := c2.GetRaw("https://example.com/a.htm")
	require.True(t, ok, "data survives reopen")
	require.Equal(t, []byte("bytes"), got)
}

// TestNoOp asserts a no-op cache always misses, drops writes, and its Fetching
// wrapper always calls through.
func TestNoOp(t *testing.T) {
	c := NoOp()

	require.NoError(t, c.PutRaw("u", []byte("x")))
	_, ok := c.GetRaw("u")
	require.False(t, ok)

	require.NoError(t, c.PutDocument("acc", sampleDoc()))
	_, ok = c.GetDocument("acc")
	require.False(t, ok)

	require.NoError(t, c.Clear())
	require.NoError(t, c.Close())
	require.Equal(t, "", c.Path())

	var calls atomic.Int64
	fetch := c.Fetching(func(_ context.Context, _ string) ([]byte, error) {
		calls.Add(1)
		return []byte("p"), nil
	})
	_, _ = fetch(context.Background(), "u")
	_, _ = fetch(context.Background(), "u")
	require.Equal(t, int64(2), calls.Load(), "no-op cache never serves from cache")
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}
