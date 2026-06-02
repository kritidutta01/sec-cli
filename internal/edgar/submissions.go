package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	tickersURL        = "https://www.sec.gov/files/company_tickers.json"
	submissionsURLFmt = "https://data.sec.gov/submissions/CIK%010d.json"

	// edgarDateLayout is the YYYY-MM-DD format EDGAR uses for filing and report
	// dates in the submissions JSON.
	edgarDateLayout = "2006-01-02"
)

// ErrUnknownTicker is returned by LookupCIK when a ticker is not in EDGAR's
// company_tickers map.
type ErrUnknownTicker struct{ Ticker string }

func (e *ErrUnknownTicker) Error() string {
	return fmt.Sprintf("edgar: unknown ticker %q", e.Ticker)
}

// ErrNoFiling is returned by LatestFiling and FilingForYear when no filing
// matches the requested form type (and year).
type ErrNoFiling struct {
	CIK  int64
	Form string
	Year int // 0 when the query was not year-scoped
}

func (e *ErrNoFiling) Error() string {
	if e.Year != 0 {
		return fmt.Sprintf("edgar: no %s filing for CIK %d in %d", e.Form, e.CIK, e.Year)
	}
	return fmt.Sprintf("edgar: no %s filing for CIK %d", e.Form, e.CIK)
}

// Filing is one entry from a company's submission history.
type Filing struct {
	AccessionNumber string
	FilingDate      time.Time
	ReportDate      time.Time
	Form            string
	PrimaryDocument string
}

// LookupCIK resolves a ticker symbol to its SEC CIK. The ticker→CIK map is
// fetched from EDGAR once and cached on the Client for the process lifetime;
// the lookup is case-insensitive. An unknown ticker returns *ErrUnknownTicker.
func (c *Client) LookupCIK(ctx context.Context, ticker string) (int64, error) {
	key := strings.ToUpper(strings.TrimSpace(ticker))

	if err := c.ensureTickers(ctx); err != nil {
		return 0, err
	}

	c.tickerMu.Lock()
	cik, ok := c.tickerCIK[key]
	c.tickerMu.Unlock()
	if !ok {
		return 0, &ErrUnknownTicker{Ticker: key}
	}
	return cik, nil
}

// ensureTickers loads the ticker→CIK map on first use. Subsequent calls are
// no-ops, so a typo does not trigger a refetch (the map covers every active
// ticker, so a miss after a successful load is genuinely unknown).
func (c *Client) ensureTickers(ctx context.Context) error {
	c.tickerMu.Lock()
	loaded := c.tickerCIK != nil
	c.tickerMu.Unlock()
	if loaded {
		return nil
	}

	body, err := c.Get(ctx, tickersURL)
	if err != nil {
		return fmt.Errorf("edgar: fetch company tickers: %w", err)
	}

	// company_tickers.json is a JSON object keyed by arbitrary index strings,
	// each value carrying the ticker and its CIK.
	var raw map[string]struct {
		CIK    int64  `json:"cik_str"`
		Ticker string `json:"ticker"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("edgar: parse company tickers: %w", err)
	}

	m := make(map[string]int64, len(raw))
	for _, e := range raw {
		m[strings.ToUpper(e.Ticker)] = e.CIK
	}

	c.tickerMu.Lock()
	c.tickerCIK = m
	c.tickerMu.Unlock()
	return nil
}

// Filings returns every filing of formType for cik, newest first. The form
// match is exact (e.g. "10-K" does not match "10-K/A"). An empty result is not
// an error — callers that need one filing use LatestFiling or FilingForYear.
func (c *Client) Filings(ctx context.Context, cik int64, formType string) ([]Filing, error) {
	url := fmt.Sprintf(submissionsURLFmt, cik)
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("edgar: fetch submissions for CIK %d: %w", cik, err)
	}

	var doc struct {
		Filings struct {
			Recent struct {
				AccessionNumber []string `json:"accessionNumber"`
				FilingDate      []string `json:"filingDate"`
				ReportDate      []string `json:"reportDate"`
				Form            []string `json:"form"`
				PrimaryDocument []string `json:"primaryDocument"`
			} `json:"recent"`
		} `json:"filings"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("edgar: parse submissions for CIK %d: %w", cik, err)
	}

	r := doc.Filings.Recent
	n := len(r.AccessionNumber)
	if len(r.FilingDate) != n || len(r.Form) != n || len(r.PrimaryDocument) != n || len(r.ReportDate) != n {
		return nil, fmt.Errorf("edgar: submissions for CIK %d have mismatched array lengths", cik)
	}

	var out []Filing
	for i := 0; i < n; i++ {
		if r.Form[i] != formType {
			continue
		}
		filed, err := time.Parse(edgarDateLayout, r.FilingDate[i])
		if err != nil {
			return nil, fmt.Errorf("edgar: parse filing date %q for CIK %d: %w", r.FilingDate[i], cik, err)
		}
		// reportDate is occasionally blank (some 8-K rows); treat as zero time.
		var reported time.Time
		if r.ReportDate[i] != "" {
			reported, err = time.Parse(edgarDateLayout, r.ReportDate[i])
			if err != nil {
				return nil, fmt.Errorf("edgar: parse report date %q for CIK %d: %w", r.ReportDate[i], cik, err)
			}
		}
		out = append(out, Filing{
			AccessionNumber: r.AccessionNumber[i],
			FilingDate:      filed,
			ReportDate:      reported,
			Form:            r.Form[i],
			PrimaryDocument: r.PrimaryDocument[i],
		})
	}

	// Newest first; tie-break on accession number so ordering is deterministic
	// regardless of the source array's order.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].FilingDate.Equal(out[j].FilingDate) {
			return out[i].FilingDate.After(out[j].FilingDate)
		}
		return out[i].AccessionNumber > out[j].AccessionNumber
	})
	return out, nil
}

// LatestFiling returns the most recent filing of formType for cik. It returns
// *ErrNoFiling when the company has filed none.
func (c *Client) LatestFiling(ctx context.Context, cik int64, formType string) (Filing, error) {
	filings, err := c.Filings(ctx, cik, formType)
	if err != nil {
		return Filing{}, err
	}
	if len(filings) == 0 {
		return Filing{}, &ErrNoFiling{CIK: cik, Form: formType}
	}
	return filings[0], nil
}

// FilingForYear returns the filing of formType whose filing date falls in the
// given calendar year. When more than one qualifies (e.g. an amendment filed
// the same year), the most recently filed wins. It returns *ErrNoFiling when
// no filing of that form was filed in that year.
func (c *Client) FilingForYear(ctx context.Context, cik int64, formType string, year int) (Filing, error) {
	filings, err := c.Filings(ctx, cik, formType)
	if err != nil {
		return Filing{}, err
	}
	// filings is newest first, so the first match in the year is the latest one.
	for _, f := range filings {
		if f.FilingDate.Year() == year {
			return f, nil
		}
	}
	return Filing{}, &ErrNoFiling{CIK: cik, Form: formType, Year: year}
}
