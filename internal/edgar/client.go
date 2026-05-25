package edgar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	secBase  = "https://www.sec.gov"
	dataBase = "https://data.sec.gov"

	// EDGAR requires "Company Name contact@email.com" format — bare tool names get 403.
	// See: https://www.sec.gov/os/accessing-edgar-data
	userAgent = "sec-cli data.kriti.dutta@gmail.com"
)

// Client talks to EDGAR's public APIs.
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("EDGAR %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// LookupCIK resolves a ticker symbol to its EDGAR CIK.
// Uses the full company_tickers.json map (~4 MB, cached by EDGAR CDN).
func (c *Client) LookupCIK(ticker string) (int64, error) {
	data, err := c.get(secBase + "/files/company_tickers.json")
	if err != nil {
		return 0, fmt.Errorf("fetching ticker map: %w", err)
	}

	var tickers map[string]Company
	if err := json.Unmarshal(data, &tickers); err != nil {
		return 0, fmt.Errorf("parsing ticker map: %w", err)
	}

	upper := strings.ToUpper(ticker)
	for _, co := range tickers {
		if strings.ToUpper(co.Ticker) == upper {
			return co.CIK, nil
		}
	}
	return 0, fmt.Errorf("ticker %q not found in EDGAR", ticker)
}

// GetSubmissions returns filing metadata for a company.
// cik must be the raw integer CIK (no padding needed here).
func (c *Client) GetSubmissions(cik int64) (*Submissions, error) {
	// Submissions endpoint uses zero-padded 10-digit CIK.
	url := fmt.Sprintf("%s/submissions/CIK%010d.json", dataBase, cik)
	data, err := c.get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching submissions for CIK %d: %w", cik, err)
	}

	var subs Submissions
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("parsing submissions: %w", err)
	}
	return &subs, nil
}

// LatestFiling returns the most recent filing of the given form type.
func (c *Client) LatestFiling(cik int64, formType string) (*Filing, error) {
	subs, err := c.GetSubmissions(cik)
	if err != nil {
		return nil, err
	}

	r := subs.Filings.Recent
	for i, form := range r.Form {
		if form == formType {
			return &Filing{
				AccessionNumber: r.AccessionNumber[i],
				FilingDate:      r.FilingDate[i],
				Form:            form,
				PrimaryDocument: r.PrimaryDocument[i],
			}, nil
		}
	}
	return nil, fmt.Errorf("no %s filing found for CIK %d", formType, cik)
}

// FetchFilingHTML downloads the primary HTML document for a filing.
// Archives URLs use the raw (unpadded) CIK integer.
func (c *Client) FetchFilingHTML(cik int64, filing *Filing) ([]byte, error) {
	// Accession number "0000320193-24-000123" → "000032019324000123" in archive paths.
	accNo := strings.ReplaceAll(filing.AccessionNumber, "-", "")
	url := fmt.Sprintf("%s/Archives/edgar/data/%d/%s/%s",
		secBase, cik, accNo, filing.PrimaryDocument)
	return c.get(url)
}
