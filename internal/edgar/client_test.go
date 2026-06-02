package edgar

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testUA = "Test contact@example.com"

// fakeTransport is an http.RoundTripper backed by a plain function. Tests use it
// instead of httptest.Server: it performs no socket I/O, which keeps the suite
// fast and deterministic, and sidesteps a Windows netpoll defect in this Go
// build where httptest's listener Close hangs on a never-returning cancelIO.
type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// response builds an *http.Response with the given status and body, as a
// RoundTripper must return one (no httptest server involved).
func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// newTestClient builds a Client wired to the given transport, with the
// User-Agent env var set for the duration of the test.
func newTestClient(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	t.Setenv(userAgentEnv, testUA)
	c, err := NewClient()
	require.NoError(t, err)
	c.http.Transport = rt
	return c
}

func TestNewClient_MissingUserAgent(t *testing.T) {
	t.Setenv(userAgentEnv, "")

	client, err := NewClient()
	require.Nil(t, client)
	require.Error(t, err)
	require.Contains(t, err.Error(), userAgentEnv)
	require.Contains(t, err.Error(), fairAccessURL)
}

func TestClientGet_SendsUserAgent(t *testing.T) {
	var got string
	client := newTestClient(t, fakeTransport(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Get("User-Agent")
		return response(http.StatusOK, "ok"), nil
	}))

	body, err := client.Get(context.Background(), "https://sec.example/doc")
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
	require.Equal(t, testUA, got)
}

func TestClientGet_RateLimit(t *testing.T) {
	client := newTestClient(t, fakeTransport(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "ok"), nil
	}))

	const N = 20
	var wg sync.WaitGroup
	errs := make(chan error, N)
	start := time.Now()
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.Get(context.Background(), "https://sec.example/doc"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("unexpected error: %v", e)
	}
	elapsed := time.Since(start)

	// With burst=1 and 9 req/sec, 20 calls = 1 immediate + 19 * (1/9)s ≈ 2.11s.
	// Lower bound 1.9s detects a missing limiter without flaking on scheduling jitter.
	require.GreaterOrEqual(t, elapsed, 1900*time.Millisecond,
		"rate limiter not enforced: 20 calls finished in %s", elapsed)
}

func TestClientGet_StatusErrorOn403(t *testing.T) {
	var calls atomic.Int32
	const url = "https://sec.example/forbidden"
	client := newTestClient(t, fakeTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(http.StatusForbidden, ""), nil
	}))

	body, err := client.Get(context.Background(), url)
	require.Nil(t, body)

	var se *StatusError
	require.ErrorAs(t, err, &se)
	require.Equal(t, http.StatusForbidden, se.Status)
	require.Equal(t, url, se.URL)
	require.Equal(t, int32(1), calls.Load(), "4xx must not retry")
}

func TestClientGet_RetriesOn500(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, fakeTransport(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return response(http.StatusInternalServerError, ""), nil
		}
		return response(http.StatusOK, "retried-ok"), nil
	}))

	body, err := client.Get(context.Background(), "https://sec.example/flaky")
	require.NoError(t, err)
	require.Equal(t, "retried-ok", string(body))
	require.Equal(t, int32(2), calls.Load(), "5xx must retry exactly once")
}
