// Package insider parses SEC Form 4 (Statement of Changes in Beneficial Ownership)
// XML documents and scores insider transaction signals.
//
// Only open-market conviction transactions are scored:
//   - Code P: open-market purchase (bullish insider signal)
//   - Code S: open-market sale (bearish insider signal / liquidity signal)
//
// Award/grant codes (A, M, F, G, J) are ignored because they reflect compensation
// structure rather than insider conviction about the stock's value.
package insider

import (
	"encoding/xml"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// TransactionCode classifies the type of insider transaction.
type TransactionCode string

const (
	CodePurchase      TransactionCode = "P" // open-market purchase — conviction buy
	CodeSale          TransactionCode = "S" // open-market sale — conviction sell
	CodeAward         TransactionCode = "A" // grant/award — compensation, not conviction
	CodeOptionExercise TransactionCode = "M" // option exercise — may or may not be conviction
	CodeTaxWithholding TransactionCode = "F" // tax withholding — forced sale, not conviction
	CodeGift          TransactionCode = "G" // gift / charitable donation
)

// IsConviction returns true only for open-market transactions that reflect
// the insider's view of the stock's value rather than compensation mechanics.
func (c TransactionCode) IsConviction() bool {
	return c == CodePurchase || c == CodeSale
}

// InsiderRole describes the reporting owner's relationship to the company.
type InsiderRole struct {
	IsDirector bool   // sits on the board
	IsOfficer  bool   // named executive officer
	IsTenPct   bool   // beneficial owner of ≥10% of shares
	Title      string // e.g. "Chief Executive Officer"
}

func (r InsiderRole) String() string {
	switch {
	case r.IsOfficer && r.Title != "":
		return r.Title
	case r.IsOfficer:
		return "Officer"
	case r.IsDirector:
		return "Director"
	case r.IsTenPct:
		return "10% Owner"
	default:
		return "Insider"
	}
}

// InsiderTransaction is one row from Form 4's nonDerivativeTable.
type InsiderTransaction struct {
	Ticker          string
	CIK             string          // issuer CIK
	AccessionNumber string
	FilingDate      string          // YYYY-MM-DD — date the Form 4 was filed on EDGAR
	PeriodOfReport  string          // YYYY-MM-DD — date(s) the transaction(s) occurred
	OwnerName       string
	OwnerCIK        string
	Role            InsiderRole
	SecurityTitle   string
	TransactionDate string          // YYYY-MM-DD
	Code            TransactionCode
	Shares          float64         // number of shares transacted (always positive)
	PricePerShare   float64         // 0 if not disclosed
	Acquired        bool            // true=acquired (A), false=disposed (D)
	ValueUSD        float64         // Shares × PricePerShare; 0 if price not disclosed
}

// ── XML parsing ───────────────────────────────────────────────────────────────

// xmlValue is a helper for EDGAR's "<field><value>X</value></field>" pattern.
type xmlValue struct {
	Value string `xml:"value"`
}

type xmlOwnershipDoc struct {
	XMLName        xml.Name  `xml:"ownershipDocument"`
	PeriodOfReport xmlValue  `xml:"periodOfReport"`
	Issuer struct {
		IssuerCIK           string `xml:"issuerCik"`
		IssuerTradingSymbol string `xml:"issuerTradingSymbol"`
	} `xml:"issuer"`
	ReportingOwner struct {
		ID struct {
			OwnerCIK  string `xml:"rptOwnerCik"`
			OwnerName string `xml:"rptOwnerName"`
		} `xml:"reportingOwnerId"`
		Relationship struct {
			IsDirector string `xml:"isDirector"`
			IsOfficer  string `xml:"isOfficer"`
			IsTenPct   string `xml:"isTenPercentOwner"`
			Title      string `xml:"officerTitle"`
		} `xml:"reportingOwnerRelationship"`
	} `xml:"reportingOwner"`
	NonDerivativeTable struct {
		Transactions []struct {
			SecurityTitle xmlValue `xml:"securityTitle"`
			TransactionDate xmlValue `xml:"transactionDate"`
			TransactionCoding struct {
				Code string `xml:"transactionCode"`
			} `xml:"transactionCoding"`
			TransactionAmounts struct {
				Shares              xmlValue `xml:"transactionShares"`
				PricePerShare       xmlValue `xml:"transactionPricePerShare"`
				AcquiredDisposedCode xmlValue `xml:"transactionAcquiredDisposedCode"`
			} `xml:"transactionAmounts"`
		} `xml:"nonDerivativeTransaction"`
	} `xml:"nonDerivativeTable"`
}

// ParseForm4XML parses a Form 4 XML document and returns all non-derivative
// transactions. Transactions with missing or empty transactionCode are skipped.
// Only the conviction codes (P and S) are typically useful to callers;
// callers should filter on InsiderTransaction.Code if they only want those.
func ParseForm4XML(xmlBytes []byte, ticker, cik, accessionNumber, filingDate string) ([]InsiderTransaction, error) {
	// Strip any BOM or leading whitespace that might confuse the XML parser.
	b := xmlBytes
	for len(b) > 0 && (b[0] == 0xEF || b[0] == 0xBB || b[0] == 0xBF || b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t') {
		if b[0] == 0xEF && len(b) >= 3 && b[1] == 0xBB && b[2] == 0xBF {
			b = b[3:]
		} else {
			b = b[1:]
		}
	}

	var doc xmlOwnershipDoc
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse form4 xml: %w", err)
	}

	role := InsiderRole{
		IsDirector: doc.ReportingOwner.Relationship.IsDirector == "1",
		IsOfficer:  doc.ReportingOwner.Relationship.IsOfficer == "1",
		IsTenPct:   doc.ReportingOwner.Relationship.IsTenPct == "1",
		Title:      strings.TrimSpace(doc.ReportingOwner.Relationship.Title),
	}

	var out []InsiderTransaction
	for _, tx := range doc.NonDerivativeTable.Transactions {
		code := TransactionCode(strings.TrimSpace(tx.TransactionCoding.Code))
		if code == "" {
			continue
		}
		shares := parseFloat(tx.TransactionAmounts.Shares.Value)
		price := parseFloat(tx.TransactionAmounts.PricePerShare.Value)
		acquired := strings.TrimSpace(tx.TransactionAmounts.AcquiredDisposedCode.Value) == "A"

		t := InsiderTransaction{
			Ticker:          ticker,
			CIK:             cik,
			AccessionNumber: accessionNumber,
			FilingDate:      filingDate,
			PeriodOfReport:  strings.TrimSpace(doc.PeriodOfReport.Value),
			OwnerName:       strings.TrimSpace(doc.ReportingOwner.ID.OwnerName),
			OwnerCIK:        strings.TrimSpace(doc.ReportingOwner.ID.OwnerCIK),
			Role:            role,
			SecurityTitle:   strings.TrimSpace(tx.SecurityTitle.Value),
			TransactionDate: strings.TrimSpace(tx.TransactionDate.Value),
			Code:            code,
			Shares:          shares,
			PricePerShare:   price,
			Acquired:        acquired,
			ValueUSD:        shares * price,
		}
		out = append(out, t)
	}
	return out, nil
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// ── Signal scoring ────────────────────────────────────────────────────────────

// ScoreInsiderActivity scores insider transactions into entity-graph signals.
// Only conviction transactions (P and S codes) by officers and directors are scored.
// allTx should be all known transactions for the ticker within the relevant window.
func ScoreInsiderActivity(allTx []InsiderTransaction, ticker string) []entitygraph.Signal {
	// Filter to conviction transactions by officers/directors.
	var conviction []InsiderTransaction
	for _, tx := range allTx {
		if !tx.Code.IsConviction() {
			continue
		}
		if !tx.Role.IsOfficer && !tx.Role.IsDirector && !tx.Role.IsTenPct {
			continue
		}
		if strings.ToUpper(tx.Ticker) != strings.ToUpper(ticker) {
			continue
		}
		conviction = append(conviction, tx)
	}
	if len(conviction) == 0 {
		return nil
	}

	var signals []entitygraph.Signal

	// Cluster sell events: if 3+ distinct officers/directors sold within 30 days, emit cluster.
	signals = append(signals, scoreSellCluster(conviction, ticker)...)

	// Score individual notable buys (open-market purchases by C-suite).
	signals = append(signals, scoreNotableBuys(conviction, ticker)...)

	return signals
}

// scoreSellCluster detects when 3+ distinct insiders (by ownerCIK) sold shares
// within any 30-day rolling window. A cluster sell is a bearish divergence signal:
// coordinated liquidity often precedes negative disclosures.
func scoreSellCluster(tx []InsiderTransaction, ticker string) []entitygraph.Signal {
	sells := filterCode(tx, CodeSale)
	if len(sells) < 3 {
		return nil
	}

	// Sort by transaction date.
	sort.Slice(sells, func(i, j int) bool {
		return sells[i].TransactionDate < sells[j].TransactionDate
	})

	// Sliding 30-day window.
	const window = 30
	var signals []entitygraph.Signal
	emitted := map[string]bool{}

	for i := 0; i < len(sells); i++ {
		anchor, err := parseDate(sells[i].TransactionDate)
		if err != nil {
			continue
		}
		cutoff := anchor.AddDate(0, 0, window)

		sellers := map[string]bool{}
		var totalValue float64
		var latestDate string
		for j := i; j < len(sells); j++ {
			d, err := parseDate(sells[j].TransactionDate)
			if err != nil {
				continue
			}
			if d.After(cutoff) {
				break
			}
			sellers[sells[j].OwnerCIK] = true
			totalValue += sells[j].ValueUSD
			if sells[j].TransactionDate > latestDate {
				latestDate = sells[j].TransactionDate
			}
		}

		if len(sellers) < 3 {
			continue
		}
		key := ticker + ":" + anchor.Format("2006-01-02")
		if emitted[key] {
			continue
		}
		emitted[key] = true

		sigID := fmt.Sprintf("insider_sell_cluster_%s_%s", strings.ToLower(ticker), anchor.Format("20060102"))
		interp := fmt.Sprintf("%d distinct insiders at %s sold shares within a 30-day window ending %s (total value: $%.0f). Coordinated insider selling can precede negative disclosures or reflect broad insider pessimism.", len(sellers), ticker, latestDate, totalValue)
		signals = append(signals, entitygraph.Signal{
			SignalID:       sigID,
			Type:           entitygraph.SignalInsiderSellCluster,
			Ticker:         ticker,
			Severity:       entitygraph.SeverityHigh,
			Confidence:     0.72,
			Score:          math.Min(float64(len(sellers))/5.0, 1.0),
			DetectedAt:     latestDate,
			FilingDate:     anchor.Format("2006-01-02"),
			ValidThrough:   anchor.AddDate(0, 6, 0).Format("2006-01-02"),
			Interpretation: interp,
			Metadata: map[string]string{
				"seller_count":   strconv.Itoa(len(sellers)),
				"window_days":    "30",
				"total_value_usd": fmt.Sprintf("%.0f", totalValue),
			},
		})
	}
	return signals
}

// scoreNotableBuys emits insider_buy signals for open-market purchases by
// C-suite officers (CEO, CFO, COO) or by any insider above a value threshold.
// These are the highest-signal conviction buys.
func scoreNotableBuys(tx []InsiderTransaction, ticker string) []entitygraph.Signal {
	buys := filterCode(tx, CodePurchase)
	var signals []entitygraph.Signal

	for _, b := range buys {
		isCLevel := isCLevelOfficer(b.Role.Title)
		aboveThreshold := b.ValueUSD >= 100_000 // $100k minimum

		if !isCLevel && !aboveThreshold {
			continue
		}

		sev := entitygraph.SeverityMedium
		conf := 0.68
		if isCLevel && b.ValueUSD >= 500_000 {
			sev = entitygraph.SeverityHigh
			conf = 0.78
		}

		sigID := fmt.Sprintf("insider_buy_%s_%s_%s", strings.ToLower(ticker), strings.ToLower(strings.ReplaceAll(b.OwnerCIK, "0", "")), b.TransactionDate)
		interp := fmt.Sprintf("%s (%s) at %s purchased %.0f shares at $%.2f/share (total $%.0f) on %s. Open-market insider buy — conviction signal.", b.OwnerName, b.Role.String(), ticker, b.Shares, b.PricePerShare, b.ValueUSD, b.TransactionDate)
		if b.ValueUSD == 0 {
			interp = fmt.Sprintf("%s (%s) at %s purchased %.0f shares (price not disclosed) on %s. Open-market insider buy — conviction signal.", b.OwnerName, b.Role.String(), ticker, b.Shares, b.TransactionDate)
		}
		signals = append(signals, entitygraph.Signal{
			SignalID:       sigID,
			Type:           entitygraph.SignalInsiderBuy,
			Ticker:         ticker,
			Entity:         b.OwnerName,
			Severity:       sev,
			Confidence:     conf,
			Score:          math.Min(b.ValueUSD/1_000_000, 1.0),
			DetectedAt:     b.FilingDate,
			FilingDate:     b.TransactionDate,
			ValidThrough:   parseAddDate(b.TransactionDate, 0, 6, 0),
			Interpretation: interp,
			Metadata: map[string]string{
				"owner_name":      b.OwnerName,
				"role":            b.Role.String(),
				"shares":          fmt.Sprintf("%.0f", b.Shares),
				"price_per_share": fmt.Sprintf("%.2f", b.PricePerShare),
				"value_usd":       fmt.Sprintf("%.0f", b.ValueUSD),
			},
		})
	}
	return signals
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func filterCode(tx []InsiderTransaction, code TransactionCode) []InsiderTransaction {
	var out []InsiderTransaction
	for _, t := range tx {
		if t.Code == code {
			out = append(out, t)
		}
	}
	return out
}

func isCLevelOfficer(title string) bool {
	t := strings.ToLower(title)
	return strings.Contains(t, "chief") ||
		strings.Contains(t, "president") ||
		strings.Contains(t, "ceo") ||
		strings.Contains(t, "cfo") ||
		strings.Contains(t, "coo") ||
		strings.Contains(t, "cto") ||
		strings.Contains(t, "general counsel")
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		s = s[:10]
	}
	return time.Parse("2006-01-02", s)
}

func parseAddDate(s string, years, months, days int) string {
	t, err := parseDate(s)
	if err != nil {
		return s
	}
	return t.AddDate(years, months, days).Format("2006-01-02")
}
