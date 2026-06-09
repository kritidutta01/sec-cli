package pipeline

import (
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kritidutta01/sec-cli/internal/cache"
	"github.com/kritidutta01/sec-cli/internal/edgar"
	"github.com/kritidutta01/sec-cli/internal/model"
	"github.com/kritidutta01/sec-cli/internal/render"
)

var update = flag.Bool("update", false, "update golden files")

// fakeTransport is a function-backed http.RoundTripper, the hermetic pattern
// from internal/edgar/client_test.go (no httptest.Server, which hangs on this
// Windows/Go build).
type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func httpResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// filingServer routes EDGAR URLs to fixtures and counts hits per URL, so a test
// can assert the cache prevented a re-fetch. primaryDoc lets a test swap the
// primary document bytes (e.g. to serve a non-iXBRL filing for the refusal case).
type filingServer struct {
	t          *testing.T
	primaryDoc []byte
	hits       map[string]*atomic.Int64
}

func newFilingServer(t *testing.T, primaryDoc []byte) *filingServer {
	return &filingServer{t: t, primaryDoc: primaryDoc, hits: map[string]*atomic.Int64{}}
}

func (s *filingServer) count(url string) int64 {
	if c, ok := s.hits[url]; ok {
		return c.Load()
	}
	return 0
}

func (s *filingServer) transport() fakeTransport {
	return func(r *http.Request) (*http.Response, error) {
		url := r.URL.String()
		c, ok := s.hits[url]
		if !ok {
			c = &atomic.Int64{}
			s.hits[url] = c
		}
		c.Add(1)

		switch {
		case strings.Contains(url, "company_tickers.json"):
			return httpResponse(http.StatusOK, readFixture(s.t, "company_tickers.json")), nil
		case strings.Contains(url, "/submissions/CIK0001234567.json"):
			return httpResponse(http.StatusOK, readFixture(s.t, "submissions.json")), nil
		case strings.HasSuffix(url, "/index.json"):
			return httpResponse(http.StatusOK, readFixture(s.t, "index.json")), nil
		case strings.HasSuffix(url, "acme-10k.htm"):
			return httpResponse(http.StatusOK, s.primaryDoc), nil
		case strings.HasSuffix(url, "acme_pre.xml"):
			return httpResponse(http.StatusOK, readFixture(s.t, "acme_pre.xml")), nil
		case strings.HasSuffix(url, "acme_lab.xml"):
			return httpResponse(http.StatusOK, readFixture(s.t, "acme_lab.xml")), nil
		default:
			return httpResponse(http.StatusNotFound, []byte("not found: "+url)), nil
		}
	}
}

// testClient builds a fixture-backed edgar.Client over the server's transport.
func (s *filingServer) testClient(t *testing.T) *edgar.Client {
	t.Helper()
	t.Setenv("SEC_CLI_USER_AGENT", "sec-cli-test contact@example.com")
	client, err := edgar.NewClient(edgar.WithHTTPClient(&http.Client{Transport: s.transport()}))
	require.NoError(t, err)
	return client
}

func openTempCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.Open(filepath.Join(t.TempDir(), "cache.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestRun_AssemblesDocument drives the full pipeline against the fixture filing
// and asserts the assembled Document's metadata, statements, and sections.
func TestRun_AssemblesDocument(t *testing.T) {
	srv := newFilingServer(t, readFixture(t, "acme-10k.htm"))
	doc, err := Run(context.Background(), Options{
		Ticker: "ACME",
		Cache:  openTempCache(t),
		Client: srv.testClient(t),
	})
	require.NoError(t, err)

	// Metadata.
	require.Equal(t, "ACME", doc.Metadata.Ticker)
	require.Equal(t, int64(1234567), doc.Metadata.CIK)
	require.Equal(t, "10-K", doc.Metadata.Form)
	require.Equal(t, "0001234567-24-000001", doc.Metadata.Accession)
	require.Equal(t, "2024-11-01", doc.Metadata.FilingDate)
	require.Equal(t, "2024-12-31", doc.Metadata.PeriodEnd)
	require.Equal(t, model.SchemaVersion, doc.Metadata.SchemaVersion)
	require.Equal(t, model.ParserVersion, doc.Metadata.ParserVersion)

	// Sections: TOC path recovers items 1, 1A, 7, 8 in order at high confidence.
	require.Len(t, doc.Sections, 4)
	items := []string{doc.Sections[0].Item, doc.Sections[1].Item, doc.Sections[2].Item, doc.Sections[3].Item}
	require.Equal(t, []string{"1", "1A", "7", "8"}, items)
	require.Equal(t, model.KindFinancial, doc.Sections[3].Kind)
	require.Contains(t, doc.Sections[0].Text, "widgets")
	require.Contains(t, doc.Sections[0].Text, "2024", "ix:nonNumeric value is kept, tag stripped")
	// Free text strips iXBRL scaffolding and ix:hidden subtrees.
	require.NotContains(t, doc.Sections[0].Text, "ix:")
	require.NotContains(t, doc.Sections[0].Text, "ZZHIDDENONLYVALUEZZ", "ix:hidden content must be dropped")

	// Statements: only the income statement projects (partial success — balance,
	// cashflow, etc. have no role and are skipped, not fatal).
	require.Len(t, doc.Statements, 1)
	inc := doc.Statements[0]
	require.Equal(t, []string{"2024", "2023"}, []string{inc.Columns[0].Label, inc.Columns[1].Label})

	byLabel := map[string][]*float64{}
	for _, r := range inc.Rows {
		byLabel[r.Label] = r.Values
	}
	// Values carry scale="6" (millions), applied during projection.
	require.Equal(t, 1_000e6, *byLabel["Revenue"][0])
	require.Equal(t, 900e6, *byLabel["Revenue"][1])
	require.Equal(t, 320e6, *byLabel["Net income"][0])
	require.Nil(t, byLabel["Research and development"][1], "missing 2023 R&D cell must be null, not zero")
}

// TestRun_ParsedCacheHit asserts the second Run for the same accession returns
// the cached Document without re-fetching the primary document or linkbases.
func TestRun_ParsedCacheHit(t *testing.T) {
	srv := newFilingServer(t, readFixture(t, "acme-10k.htm"))
	c := openTempCache(t)
	client := srv.testClient(t)

	const primaryURL = "https://www.sec.gov/Archives/edgar/data/1234567/000123456724000001/acme-10k.htm"
	const preURL = "https://www.sec.gov/Archives/edgar/data/1234567/000123456724000001/acme_pre.xml"

	_, err := Run(context.Background(), Options{Ticker: "ACME", Cache: c, Client: client})
	require.NoError(t, err)
	require.Equal(t, int64(1), srv.count(primaryURL), "primary fetched once on first run")
	require.Equal(t, int64(1), srv.count(preURL), "linkbase fetched once on first run")

	doc2, err := Run(context.Background(), Options{Ticker: "ACME", Cache: c, Client: client})
	require.NoError(t, err)
	require.Equal(t, "0001234567-24-000001", doc2.Metadata.Accession)

	require.Equal(t, int64(1), srv.count(primaryURL), "parsed-cache hit must not re-fetch the primary document")
	require.Equal(t, int64(1), srv.count(preURL), "parsed-cache hit must not re-fetch linkbases")
}

// TestRun_NoCacheRefetches asserts that without a cache (nil → no-op) the second
// run fetches the document again.
func TestRun_NoCacheRefetches(t *testing.T) {
	srv := newFilingServer(t, readFixture(t, "acme-10k.htm"))
	client := srv.testClient(t)
	const primaryURL = "https://www.sec.gov/Archives/edgar/data/1234567/000123456724000001/acme-10k.htm"

	_, err := Run(context.Background(), Options{Ticker: "ACME", Client: client})
	require.NoError(t, err)
	_, err = Run(context.Background(), Options{Ticker: "ACME", Client: client})
	require.NoError(t, err)

	require.Equal(t, int64(2), srv.count(primaryURL), "no-op cache must re-fetch every run")
}

// TestRun_RefusesUnsupportedFormat asserts a non-iXBRL filing yields the standard
// refusal message.
func TestRun_RefusesUnsupportedFormat(t *testing.T) {
	plain := []byte("<html><body><p>Plain HTML filing, no inline XBRL.</p></body></html>")
	srv := newFilingServer(t, plain)
	_, err := Run(context.Background(), Options{
		Ticker: "ACME",
		Cache:  openTempCache(t),
		Client: srv.testClient(t),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "v1.0 supports inline-XBRL filings only")
}

// TestRun_YearSelectsFiling asserts --year resolves the older filing.
func TestRun_YearSelectsFiling(t *testing.T) {
	srv := newFilingServer(t, readFixture(t, "acme-10k.htm"))
	// The 2023 filing's primary doc name differs; serve the same iXBRL bytes for it.
	srv.primaryDoc = readFixture(t, "acme-10k.htm")
	_, err := Run(context.Background(), Options{
		Ticker: "ACME",
		Year:   2023,
		Cache:  openTempCache(t),
		Client: srv.testClient(t),
	})
	// The 2023 doc name is acme-10k-2023.htm, which the server 404s; assert the
	// year selection reached fetch (a 404 on the right doc, not a wrong-year hit).
	require.Error(t, err)
	require.Contains(t, err.Error(), "fetch primary document")
}

// TestRun_GoldenRendering golden-files the assembled document rendered in each
// format, exercising the full pipeline → render path.
func TestRun_GoldenRendering(t *testing.T) {
	srv := newFilingServer(t, readFixture(t, "acme-10k.htm"))
	doc, err := Run(context.Background(), Options{
		Ticker: "ACME",
		Cache:  openTempCache(t),
		Client: srv.testClient(t),
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		format, golden string
	}{
		{"json", "get.json"},
		{"md", "get.md"},
		{"text", "get.txt"},
	} {
		renderer, err := render.For(tc.format)
		require.NoError(t, err)
		var b strings.Builder
		require.NoError(t, renderer.Render(doc, &b))
		goldenCheck(t, tc.golden, b.String())
	}
}

func goldenCheck(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden %s (run: go test ./internal/pipeline -update)", name)
	require.Equal(t, string(want), got)
}
