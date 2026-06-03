package router

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// noFetch is a Fetcher that fails — used for standalone documents that should
// never trigger index resolution.
func noFetch(context.Context, string) ([]byte, error) {
	return nil, errors.New("unexpected fetch")
}

func TestDetect_iXBRL(t *testing.T) {
	dec, err := Detect(context.Background(), readFixture(t, "ixbrl.htm"), noFetch)
	require.NoError(t, err)
	require.Equal(t, IXBRL, dec.Format)
	require.False(t, dec.ResolvedFromIndex)
}

func TestDetect_PartialIXBRL(t *testing.T) {
	dec, err := Detect(context.Background(), readFixture(t, "partial.htm"), noFetch)
	require.NoError(t, err)
	require.Equal(t, PartialIXBRL, dec.Format)
}

func TestDetect_PlainHTML(t *testing.T) {
	dec, err := Detect(context.Background(), readFixture(t, "plainhtml.htm"), noFetch)
	require.NoError(t, err)
	require.Equal(t, PlainHTML, dec.Format)
}

func TestDetect_PlainText(t *testing.T) {
	dec, err := Detect(context.Background(), readFixture(t, "plaintext.txt"), noFetch)
	require.NoError(t, err)
	require.Equal(t, PlainText, dec.Format)
}

func TestDetect_Unknown(t *testing.T) {
	dec, err := Detect(context.Background(), readFixture(t, "unknown.bin"), noFetch)
	require.NoError(t, err)
	require.Equal(t, Unknown, dec.Format)
}

func TestDetect_FilingIndex(t *testing.T) {
	const wantRef = "/Archives/edgar/data/320193/000032019324000123/aapl-20240928.htm"
	primary := readFixture(t, "ixbrl.htm")

	var gotRef string
	fetch := func(_ context.Context, ref string) ([]byte, error) {
		gotRef = ref
		return primary, nil
	}

	dec, err := Detect(context.Background(), readFixture(t, "index.htm"), fetch)
	require.NoError(t, err)
	require.Equal(t, wantRef, gotRef, "router must resolve the index's primary-document link")
	require.True(t, dec.ResolvedFromIndex)
	require.Equal(t, IXBRL, dec.Format, "classification reflects the resolved primary document")
	require.Equal(t, primary, dec.PrimaryDocument, "decision carries the resolved bytes")
}

func TestDetect_IndexRecursionBoundedAtOne(t *testing.T) {
	index := readFixture(t, "index.htm")

	var calls int
	fetch := func(_ context.Context, _ string) ([]byte, error) {
		calls++
		return index, nil // pathological: index points at another index
	}

	dec, err := Detect(context.Background(), index, fetch)
	require.NoError(t, err)
	require.Equal(t, 1, calls, "index resolution must not recurse more than once")
	require.True(t, dec.ResolvedFromIndex)
	// The re-fetched bytes are still an index page (HTML, no ix namespace) and
	// are classified rather than resolved again.
	require.Equal(t, PlainHTML, dec.Format)
	require.NotEmpty(t, dec.Warnings)
}

func TestDetect_IndexWithoutFetcher(t *testing.T) {
	dec, err := Detect(context.Background(), readFixture(t, "index.htm"), nil)
	require.NoError(t, err)
	require.Equal(t, Unknown, dec.Format)
	require.NotEmpty(t, dec.Warnings)
}

func TestDetect_FetcherErrorPropagates(t *testing.T) {
	fetch := func(context.Context, string) ([]byte, error) {
		return nil, errors.New("network down")
	}
	_, err := Detect(context.Background(), readFixture(t, "index.htm"), fetch)
	require.ErrorContains(t, err, "resolve filing index")
	require.ErrorContains(t, err, "network down")
}

// TestDetect_Manifest enforces 100% classification accuracy across the held-out
// corpus listed in testdata/manifest.json.
func TestDetect_Manifest(t *testing.T) {
	var manifest struct {
		Cases []struct {
			File     string `json:"file"`
			Expected string `json:"expected"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(readFixture(t, "manifest.json"), &manifest))
	require.NotEmpty(t, manifest.Cases)

	for _, c := range manifest.Cases {
		t.Run(c.File, func(t *testing.T) {
			dec, err := Detect(context.Background(), readFixture(t, c.File), noFetch)
			require.NoError(t, err)
			require.Equal(t, c.Expected, dec.Format.String(),
				"%s misclassified", c.File)
		})
	}
}
