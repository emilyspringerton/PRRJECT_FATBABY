// Package earningscal tracks earnings report dates for watchlisted tickers.
// An EarningsDate record links a ticker + fiscal period to the specific calendar
// date when the company is expected to (or did) report financial results.
//
// Sources ranked by trust:
//   confirmed  — actual 8-K Item 2.02 filing date (company already reported)
//   announced  — press release explicitly announcing an upcoming earnings date
//   backfilled — derived from eps.Article.PublishAt (historical approximate)
package earningscal

// Status describes how confident we are in the earnings date.
type Status string

const (
	StatusConfirmed  Status = "confirmed"  // actual 8-K filing date
	StatusAnnounced  Status = "announced"  // press release announcing upcoming date
	StatusBackfilled Status = "backfilled" // derived from EPS article publish date
)

// EarningsDate is one earnings report date entry.
type EarningsDate struct {
	// ID is stable across updates: "earncal-{ticker}-{Q}{year}".
	// Used for deduplication; newer records with higher-status sources overwrite.
	ID string `json:"id"`

	Ticker        string `json:"ticker"`
	Issuer        string `json:"issuer,omitempty"`
	FiscalQuarter string `json:"fiscal_quarter"` // Q1|Q2|Q3|Q4|FY
	FiscalYear    int    `json:"fiscal_year"`

	// ReportDate is YYYY-MM-DD — the date earnings were/will be reported.
	ReportDate string `json:"report_date"`

	// BeforeMarket is true when the report is before market open (BMO),
	// false for after market close (AMC), and null/zero when unknown.
	BeforeMarket *bool `json:"before_market,omitempty"`

	Status   Status `json:"status"`
	Source   string `json:"source"`    // "8k_filing"|"press_release"|"eps_article"
	SourceID string `json:"source_id"` // accession number or article/PR ID

	UpdatedAt string `json:"updated_at"` // RFC3339
}

// ID builds the stable dedup key for a (ticker, quarter, year) triple.
func MakeID(ticker, quarter string, year int) string {
	return "earncal-" + ticker + "-" + quarter + "-" + itoa(year)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [10]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
