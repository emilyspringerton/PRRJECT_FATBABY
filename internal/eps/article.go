package eps

import (
	"fmt"
	"strings"
	"time"
)

// Article is a generated earnings headline ready for publishing.
// Persisted in var/eps/articles.ndjson.
type Article struct {
	// SourceIdentity links back to the SourceDocument (press release).
	SourceIdentity string        `json:"source_identity"`
	Ticker         string        `json:"ticker"`
	Headline       string        `json:"headline"`
	Dek            string        `json:"dek"`
	Body           string        `json:"body"`
	SourceURL      string        `json:"source_url,omitempty"`
	Period         EarningsPeriod `json:"period"`
	EPSValue       float64       `json:"eps_value"`
	IsGAAP         bool          `json:"is_gaap"`
	PublishAt      string        `json:"publish_at"` // RFC3339 UTC
}

// Generate builds a publishable Article from EarningsData.
// Returns (Article, true) when the extraction meets publish criteria:
// confidence ≥ 0.70 and no FlagTraceFailure.
// Returns (Article{}, false) when the release should not auto-publish.
func Generate(e *EarningsData) (Article, bool) {
	if e.ExtractionConfidence < 0.70 || e.HasFlag(FlagTraceFailure) {
		return Article{}, false
	}

	val, isGAAP := e.HeadlineEPS()
	if val == 0 {
		return Article{}, false
	}

	issuer := e.Issuer
	if issuer == "" {
		issuer = e.Ticker
	}
	if issuer == "" {
		return Article{}, false
	}

	headline := buildHeadline(issuer, e.Period, val, isGAAP)
	dek := buildDek(e, val, isGAAP)
	body := buildBody(e, val, isGAAP)

	return Article{
		SourceIdentity: e.SourceIdentity,
		Ticker:         e.Ticker,
		Headline:       headline,
		Dek:            dek,
		Body:           body,
		Period:         e.Period,
		EPSValue:       val,
		IsGAAP:         isGAAP,
		PublishAt:      time.Now().UTC().Format(time.RFC3339),
	}, true
}

// buildHeadline formats the headline per northstar spec:
//   - GAAP positive: "{Issuer} reports {period} EPS of ${N.NN}"
//   - GAAP loss:     "{Issuer} reports {period} loss of ${N.NN} per share"
//   - Adjusted-only: "{Issuer} reports {period} adjusted EPS of ${N.NN}"
func buildHeadline(issuer string, period EarningsPeriod, val float64, isGAAP bool) string {
	periodLabel := formatPeriodLabel(period)
	if isGAAP {
		if val < 0 {
			return fmt.Sprintf("%s reports %s loss of $%.2f per share", issuer, periodLabel, -val)
		}
		return fmt.Sprintf("%s reports %s EPS of $%.2f", issuer, periodLabel, val)
	}
	// Adjusted-only: label it clearly.
	if val < 0 {
		return fmt.Sprintf("%s reports %s adjusted loss of $%.2f per share", issuer, periodLabel, -val)
	}
	return fmt.Sprintf("%s reports %s adjusted EPS of $%.2f", issuer, periodLabel, val)
}

func buildDek(e *EarningsData, val float64, isGAAP bool) string {
	var parts []string

	label := "GAAP diluted EPS"
	if !isGAAP {
		label = "Non-GAAP diluted EPS"
	}
	if val < 0 {
		parts = append(parts, fmt.Sprintf("%s: ($%.2f)", label, -val))
	} else {
		parts = append(parts, fmt.Sprintf("%s: $%.2f", label, val))
	}

	if e.YoYEPS != nil {
		yoy := *e.YoYEPS
		change := val - yoy
		pct := 0.0
		if yoy != 0 {
			pct = change / yoy * 100
		}
		dir := "up"
		if change < 0 {
			dir = "down"
		}
		parts = append(parts, fmt.Sprintf("%s %.1f%% year-over-year from $%.2f", dir, abs(pct), yoy))
	}

	if e.Revenue != nil {
		parts = append(parts, fmt.Sprintf("revenue $%.1fB", *e.Revenue/1000))
	}

	return strings.Join(parts, "; ")
}

func buildBody(e *EarningsData, val float64, isGAAP bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Period: %s", formatPeriodLabel(e.Period)))
	if e.Period.PeriodEnd != "" {
		sb.WriteString(fmt.Sprintf(" (ended %s)", e.Period.PeriodEnd))
	}
	sb.WriteString("\n\n")

	if isGAAP && e.EPSGAAPDiluted != nil {
		sb.WriteString(fmt.Sprintf("GAAP diluted EPS: $%.2f\n", *e.EPSGAAPDiluted))
	}
	if e.EPSGAAPBasic != nil {
		sb.WriteString(fmt.Sprintf("GAAP basic EPS: $%.2f\n", *e.EPSGAAPBasic))
	}
	if e.EPSAdjusted != nil {
		sb.WriteString(fmt.Sprintf("Non-GAAP adjusted EPS: $%.2f (issuer's non-GAAP measure)\n", *e.EPSAdjusted))
	}
	if !isGAAP && val != 0 {
		sb.WriteString(fmt.Sprintf("Non-GAAP diluted EPS (headline): $%.2f\n", val))
	}

	if e.NetIncome != nil {
		sb.WriteString(fmt.Sprintf("Net income: $%.0fM\n", *e.NetIncome))
	}
	if e.Revenue != nil {
		sb.WriteString(fmt.Sprintf("Revenue: $%.0fM\n", *e.Revenue))
	}
	if e.SharesDiluted != nil {
		sb.WriteString(fmt.Sprintf("Diluted shares: %.0fM\n", *e.SharesDiluted))
	}
	if e.YoYEPS != nil {
		sb.WriteString(fmt.Sprintf("Prior-year EPS: $%.2f\n", *e.YoYEPS))
	}

	if e.EPSBasis == "continuing_ops" {
		sb.WriteString("\nNote: EPS is on a continuing operations basis.\n")
	}

	return sb.String()
}

// formatPeriodLabel returns a human-readable period string, e.g. "Q1 2026" or "full-year 2026".
func formatPeriodLabel(p EarningsPeriod) string {
	if p.FiscalQuarter == "FY" {
		if p.FiscalYear > 0 {
			return fmt.Sprintf("full-year %d", p.FiscalYear)
		}
		return "full-year"
	}
	if p.FiscalQuarter != "" && p.FiscalYear > 0 {
		return fmt.Sprintf("%s %d", p.FiscalQuarter, p.FiscalYear)
	}
	if p.FiscalQuarter != "" {
		return p.FiscalQuarter
	}
	if p.FiscalYear > 0 {
		return fmt.Sprintf("%d", p.FiscalYear)
	}
	return "current period"
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
