package edgar

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentURL_AAPLFY2024(t *testing.T) {
	f := Filing{
		AccessionNumber: "0000320193-24-000123",
		PrimaryDocument: "aapl-20240928.htm",
	}
	want := "https://www.sec.gov/Archives/edgar/data/320193/000032019324000123/aapl-20240928.htm"
	require.Equal(t, want, DocumentURL(aaplCIK, f))
}

func TestDocumentURL_StripsAllDashesAndDoesNotPadCIK(t *testing.T) {
	// A small CIK must appear unpadded in the Archives path (the submissions
	// endpoint pads to 10 digits; the Archives path does not).
	f := Filing{
		AccessionNumber: "0000012345-08-000001",
		PrimaryDocument: "oldfiling.txt",
	}
	got := DocumentURL(789, f)
	require.Equal(t, "https://www.sec.gov/Archives/edgar/data/789/000001234508000001/oldfiling.txt", got)
	require.NotContains(t, got, "-", "accession dashes must be stripped from the URL path")
}

func TestFetchPrimaryDocument_ReturnsRawBytes(t *testing.T) {
	const doc = "<html xmlns:ix=\"http://www.xbrl.org/2013/inlineXBRL\">...</html>"
	var gotURL string
	client := newTestClient(t, fakeTransport(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return response(http.StatusOK, doc), nil
	}))

	f := Filing{AccessionNumber: "0000320193-24-000123", PrimaryDocument: "aapl-20240928.htm"}
	body, err := client.FetchPrimaryDocument(context.Background(), aaplCIK, f)
	require.NoError(t, err)
	require.Equal(t, doc, string(body), "bytes must be returned unmodified")
	require.Equal(t, DocumentURL(aaplCIK, f), gotURL, "fetch must hit the constructed document URL")
}

func TestFetchPrimaryDocument_PropagatesHTTPError(t *testing.T) {
	client := newTestClient(t, fakeTransport(func(*http.Request) (*http.Response, error) {
		return response(http.StatusNotFound, ""), nil
	}))

	f := Filing{AccessionNumber: "0000320193-24-000123", PrimaryDocument: "missing.htm"}
	_, err := client.FetchPrimaryDocument(context.Background(), aaplCIK, f)

	var se *StatusError
	require.ErrorAs(t, err, &se)
	require.Equal(t, http.StatusNotFound, se.Status)
	require.True(t, strings.Contains(err.Error(), "fetch primary document"),
		"error must be wrapped with package context")
}
