package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// archivesURLFmt is the EDGAR Archives path for a filing's documents. The CIK
// appears without zero-padding (unlike the submissions endpoint), and the
// accession number has its dashes stripped.
const archivesURLFmt = "https://www.sec.gov/Archives/edgar/data/%d/%s/%s"

// archivesDirURLFmt is the EDGAR Archives directory for a filing — the folder
// that holds the primary document, the XBRL linkbases, and the schema.
const archivesDirURLFmt = "https://www.sec.gov/Archives/edgar/data/%d/%s/"

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

// CompanionURL builds the Archives URL for another file in the same filing
// directory as the primary document — an XBRL linkbase or schema, for instance.
func CompanionURL(cik int64, filing Filing, name string) string {
	accession := strings.ReplaceAll(filing.AccessionNumber, "-", "")
	return fmt.Sprintf(archivesURLFmt, cik, accession, name)
}

// FilingFiles lists the file names in a filing's Archives directory, read from
// the directory's index.json. It is how callers discover the linkbase and
// schema companions whose names aren't carried in the submissions metadata.
func (c *Client) FilingFiles(ctx context.Context, cik int64, filing Filing) ([]string, error) {
	accession := strings.ReplaceAll(filing.AccessionNumber, "-", "")
	url := fmt.Sprintf(archivesDirURLFmt, cik, accession) + "index.json"
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("edgar: list filing files for %s: %w", filing.AccessionNumber, err)
	}

	var doc struct {
		Directory struct {
			Item []struct {
				Name string `json:"name"`
			} `json:"item"`
		} `json:"directory"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("edgar: parse filing index for %s: %w", filing.AccessionNumber, err)
	}

	names := make([]string, 0, len(doc.Directory.Item))
	for _, it := range doc.Directory.Item {
		names = append(names, it.Name)
	}
	return names, nil
}
