package edgar

// Company maps a ticker symbol to its EDGAR CIK.
type Company struct {
	CIK    int64  `json:"cik_str"`
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
}

// Submissions is the top-level response from the EDGAR submissions API.
type Submissions struct {
	CIK     string `json:"cik"`
	Name    string `json:"name"`
	Filings struct {
		Recent RecentFilings `json:"recent"`
	} `json:"filings"`
}

// RecentFilings is the parallel-array structure EDGAR returns for recent filings.
type RecentFilings struct {
	AccessionNumber []string `json:"accessionNumber"`
	FilingDate      []string `json:"filingDate"`
	Form            []string `json:"form"`
	PrimaryDocument []string `json:"primaryDocument"`
}

// Filing is a resolved, single-filing record.
type Filing struct {
	AccessionNumber string
	FilingDate      string
	Form            string
	PrimaryDocument string
}
