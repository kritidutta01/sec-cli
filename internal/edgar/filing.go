package edgar

import (
	"context"
	"fmt"
	"strings"
)

// archivesURLFmt is the EDGAR Archives path for a filing's documents. The CIK
// appears without zero-padding (unlike the submissions endpoint), and the
// accession number has its dashes stripped.
const archivesURLFmt = "https://www.sec.gov/Archives/edgar/data/%d/%s/%s"

// DocumentURL builds the EDGAR Archives URL for a filing's primary document.
// Example: CIK 320193, accession 0000320193-24-000123, document aapl-20240928.htm
// →https://www.sec.gov/Archives/edgar/data/320193/000032019324000123/aapl-20240928.htm
func DocumentURL(cik int64, filing Filing) string {
	accession := strings.ReplaceAll(filing.AccessionNumber, "-", "")
	return fmt.Sprintf(archivesURLFmt, cik, accession, filing.PrimaryDocument)
}

// FetchPrimaryDocument returns the raw bytes of a filing's primary document. It
// does not interpret the bytes — classifying the format is the router's job
// (Phase 4).
func (c *Client) FetchPrimaryDocument(ctx context.Context, cik int64, filing Filing) ([]byte, error) {
	url := DocumentURL(cik, filing)
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("edgar: fetch primary document %s: %w", url, err)
	}
	return body, nil
}
