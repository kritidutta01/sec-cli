package edgar

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const aaplCIK = 320193

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

// fixtureClient returns a Client whose transport serves the two EDGAR
// endpoints Phase 2 uses from recorded fixtures, and a pointer to a map of
// per-URL hit counts so tests can assert the in-memory ticker cache.
func fixtureClient(t *testing.T) (*Client, *map[string]int) {
	t.Helper()
	hits := map[string]int{}
	var mu sync.Mutex

	tickers := readFixture(t, "company_tickers.json")
	submissions := readFixture(t, "CIK0000320193.json")

	client := newTestClient(t, fakeTransport(func(r *http.Request) (*http.Response, error) {
		url := r.URL.String()
		mu.Lock()
		hits[url]++
		mu.Unlock()

		switch url {
		case tickersURL:
			return response(http.StatusOK, tickers), nil
		case "https://data.sec.gov/submissions/CIK0000320193.json":
			return response(http.StatusOK, submissions), nil
		default:
			return response(http.StatusNotFound, ""), nil
		}
	}))
	return client, &hits
}

func TestLookupCIK_AAPL(t *testing.T) {
	client, _ := fixtureClient(t)

	cik, err := client.LookupCIK(context.Background(), "AAPL")
	require.NoError(t, err)
	require.Equal(t, int64(aaplCIK), cik)
}

func TestLookupCIK_CaseInsensitive(t *testing.T) {
	client, _ := fixtureClient(t)

	cik, err := client.LookupCIK(context.Background(), "  aapl ")
	require.NoError(t, err)
	require.Equal(t, int64(aaplCIK), cik)
}

func TestLookupCIK_UnknownTicker(t *testing.T) {
	client, _ := fixtureClient(t)

	_, err := client.LookupCIK(context.Background(), "NOPE")
	var unknown *ErrUnknownTicker
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, "NOPE", unknown.Ticker)
}

func TestLookupCIK_CachesTickerMap(t *testing.T) {
	client, hits := fixtureClient(t)

	for i := 0; i < 3; i++ {
		_, err := client.LookupCIK(context.Background(), "AAPL")
		require.NoError(t, err)
	}
	// An unknown ticker must not trigger a refetch either.
	_, _ = client.LookupCIK(context.Background(), "NOPE")

	require.Equal(t, 1, (*hits)[tickersURL], "ticker map should be fetched exactly once")
}

func TestFilings_TenAnnual10K(t *testing.T) {
	client, _ := fixtureClient(t)

	filings, err := client.Filings(context.Background(), aaplCIK, "10-K")
	require.NoError(t, err)
	require.Len(t, filings, 10)

	// Newest first, and every result is the requested form.
	for i, f := range filings {
		require.Equal(t, "10-K", f.Form)
		if i > 0 {
			require.False(t, f.FilingDate.After(filings[i-1].FilingDate),
				"filings must be sorted newest first")
		}
	}
}

func TestFilings_RequestsZeroPaddedCIKURL(t *testing.T) {
	client, hits := fixtureClient(t)

	_, err := client.Filings(context.Background(), aaplCIK, "10-K")
	require.NoError(t, err)
	require.Equal(t, 1, (*hits)["https://data.sec.gov/submissions/CIK0000320193.json"],
		"submissions URL must zero-pad the CIK to 10 digits")
}

func TestLatestFiling_IsFY2024(t *testing.T) {
	client, _ := fixtureClient(t)

	f, err := client.LatestFiling(context.Background(), aaplCIK, "10-K")
	require.NoError(t, err)
	require.Equal(t, "0000320193-24-000123", f.AccessionNumber)
	require.Equal(t, "aapl-20240928.htm", f.PrimaryDocument)
	require.Equal(t, 2024, f.FilingDate.Year())
	require.Equal(t, 2024, f.ReportDate.Year())
	require.Equal(t, 9, int(f.ReportDate.Month()))
}

func TestFilingForYear_2020(t *testing.T) {
	client, _ := fixtureClient(t)

	f, err := client.FilingForYear(context.Background(), aaplCIK, "10-K", 2020)
	require.NoError(t, err)
	require.Equal(t, "0000320193-20-000096", f.AccessionNumber)
	require.Equal(t, 2020, f.FilingDate.Year())
}

func TestFilingForYear_NoMatch(t *testing.T) {
	client, _ := fixtureClient(t)

	_, err := client.FilingForYear(context.Background(), aaplCIK, "10-K", 1999)
	var none *ErrNoFiling
	require.ErrorAs(t, err, &none)
	require.Equal(t, 1999, none.Year)
}

func TestLatestFiling_NoneOfForm(t *testing.T) {
	client, _ := fixtureClient(t)

	_, err := client.LatestFiling(context.Background(), aaplCIK, "S-1")
	var none *ErrNoFiling
	require.ErrorAs(t, err, &none)
	require.Equal(t, "S-1", none.Form)
}

// submissionsClient wires a Client to serve fixed submissions JSON for any
// CIK URL, for exercising parse edge cases without a fixture file.
func submissionsClient(t *testing.T, body string) *Client {
	t.Helper()
	return newTestClient(t, fakeTransport(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, body), nil
	}))
}

func TestFilings_EmptyReportDateIsZeroTime(t *testing.T) {
	// An 8-K with a blank reportDate — common in real EDGAR data.
	const body = `{"filings":{"recent":{
		"accessionNumber":["0000320193-24-000079"],
		"filingDate":["2024-08-01"],
		"reportDate":[""],
		"form":["8-K"],
		"primaryDocument":["aapl-20240801.htm"]}}}`
	client := submissionsClient(t, body)

	filings, err := client.Filings(context.Background(), aaplCIK, "8-K")
	require.NoError(t, err)
	require.Len(t, filings, 1)
	require.True(t, filings[0].ReportDate.IsZero(), "blank reportDate should parse to the zero time")
	require.Equal(t, 2024, filings[0].FilingDate.Year())
}

func TestFilings_MismatchedArrayLengths(t *testing.T) {
	// form array is shorter than accessionNumber — malformed EDGAR payload.
	const body = `{"filings":{"recent":{
		"accessionNumber":["a","b"],
		"filingDate":["2024-08-01","2024-05-01"],
		"reportDate":["2024-08-01","2024-05-01"],
		"form":["8-K"],
		"primaryDocument":["x.htm","y.htm"]}}}`
	client := submissionsClient(t, body)

	_, err := client.Filings(context.Background(), aaplCIK, "8-K")
	require.ErrorContains(t, err, "mismatched array lengths")
}

func TestFilings_PropagatesHTTPError(t *testing.T) {
	// An unknown CIK yields a 404 from EDGAR; Filings must surface it.
	client := newTestClient(t, fakeTransport(func(*http.Request) (*http.Response, error) {
		return response(http.StatusNotFound, ""), nil
	}))

	_, err := client.Filings(context.Background(), 9999999999, "10-K")
	var se *StatusError
	require.ErrorAs(t, err, &se)
	require.Equal(t, http.StatusNotFound, se.Status)
}

func TestLookupCIK_PropagatesFetchError(t *testing.T) {
	client := newTestClient(t, fakeTransport(func(*http.Request) (*http.Response, error) {
		return response(http.StatusForbidden, ""), nil
	}))

	_, err := client.LookupCIK(context.Background(), "AAPL")
	var se *StatusError
	require.ErrorAs(t, err, &se)
	require.Equal(t, http.StatusForbidden, se.Status)
}
