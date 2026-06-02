// Package edgar is the HTTP client layer for SEC EDGAR. It enforces the two
// rules EDGAR rejects requests over: a present User-Agent header and the
// 10 req/sec rate cap.
package edgar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	userAgentEnv  = "SEC_CLI_USER_AGENT"
	fairAccessURL = "https://www.sec.gov/os/accessing-edgar-data"

	// rateLimitPerSec is one below EDGAR's 10 req/sec cap to absorb clock skew
	// between the local limiter and EDGAR's edge.
	rateLimitPerSec = 9
	rateBurst       = 1

	defaultHTTPTimeout = 30 * time.Second
	retryDelay         = 500 * time.Millisecond
)

// StatusError is returned by Get for any non-2xx HTTP response. Callers can
// use errors.As to inspect the status code.
type StatusError struct {
	Status int
	URL    string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("edgar: HTTP %d for %s", e.Status, e.URL)
}

// Client issues rate-limited, User-Agent-identified requests to SEC EDGAR.
// One Client owns one rate limiter; share a single Client across goroutines
// to keep concurrent fetches under the global rate cap.
type Client struct {
	http      *http.Client
	limiter   *rate.Limiter
	userAgent string

	// tickerMu guards the lazily-loaded ticker→CIK map. The map is fetched once
	// per process on first LookupCIK and reused for the Client's lifetime.
	tickerMu  sync.Mutex
	tickerCIK map[string]int64
}

// NewClient reads SEC_CLI_USER_AGENT from the environment and returns a Client.
// EDGAR's Fair Access policy requires every request identify the caller; a
// missing or empty env var returns an error here instead of letting EDGAR
// answer 403 on the wire.
func NewClient() (*Client, error) {
	ua := os.Getenv(userAgentEnv)
	if ua == "" {
		return nil, fmt.Errorf(
			"%s environment variable is not set; SEC EDGAR Fair Access policy requires a contact identifier — see %s",
			userAgentEnv, fairAccessURL,
		)
	}
	return &Client{
		http:      &http.Client{Timeout: defaultHTTPTimeout},
		limiter:   rate.NewLimiter(rate.Limit(rateLimitPerSec), rateBurst),
		userAgent: ua,
	}, nil
}

// Get fetches url and returns the response body. It blocks on the shared rate
// limiter, sets the User-Agent header, retries once on 5xx with a 500ms delay,
// and returns *StatusError for any non-2xx response.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	body, err := c.do(ctx, url)
	if err == nil {
		return body, nil
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Status < 500 || se.Status > 599 {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(retryDelay):
	}
	return c.do(ctx, url)
}

func (c *Client) do(ctx context.Context, url string) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("edgar: rate limit wait: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("edgar: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("edgar: GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &StatusError{Status: resp.StatusCode, URL: url}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("edgar: read body from %s: %w", url, err)
	}
	return body, nil
}
