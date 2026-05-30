package eps

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Extract parses an earnings press release text and returns structured EarningsData.
// The caller should pass sourceIdentity (SourceDocument.Identity) and ticker.
// Returns a non-nil *EarningsData even on partial extraction; inspect
// ExtractionConfidence and ExtractionFlags to decide whether to publish.
func Extract(text, sourceIdentity, ticker string) *EarningsData {
	e := &EarningsData{
		SourceIdentity: sourceIdentity,
		Ticker:         ticker,
		Currency:       "USD",
		EPSBasis:       "total",
	}

	norm := strings.ToLower(text)

	e.Period = extractPeriod(text, norm)
	e.Issuer = extractIssuer(text, norm)

	e.EPSGAAPDiluted = extractGAAPDiluted(text, norm)
	e.EPSGAAPBasic = extractGAAPBasic(text, norm)
	e.EPSAdjusted = extractAdjusted(text, norm)
	e.NetIncome = extractNetIncome(text, norm)
	e.SharesDiluted = extractSharesDiluted(text, norm)
	e.Revenue = extractRevenue(text, norm)
	e.YoYEPS = extractYoYEPS(text, norm, e.EPSGAAPDiluted)

	if hasContinuingOpsLanguage(norm) {
		e.EPSBasis = "continuing_ops"
	}

	if e.EPSGAAPDiluted == nil && e.EPSAdjusted != nil {
		e.addFlag(FlagAdjustedOnly)
		e.addFlag(FlagNoGAAP)
	}

	e.ExtractionConfidence = scoreConfidence(e, norm)
	if e.ExtractionConfidence < 0.70 {
		e.addFlag(FlagLowConfidence)
	}

	return e
}

// ---- period -----------------------------------------------------------

var (
	reQ1        = regexp.MustCompile(`(?i)\b(?:first\s+quarter|q1|1q)\b`)
	reQ2        = regexp.MustCompile(`(?i)\b(?:second\s+quarter|q2|2q)\b`)
	reQ3        = regexp.MustCompile(`(?i)\b(?:third\s+quarter|q3|3q)\b`)
	reQ4        = regexp.MustCompile(`(?i)\b(?:fourth\s+quarter|q4|4q)\b`)
	reFY        = regexp.MustCompile(`(?i)\b(?:full.?year|fiscal\s+year|annual)\b`)
	reYear      = regexp.MustCompile(`\b(20[2-9][0-9])\b`)
	reFiscalYear = regexp.MustCompile(`(?i)fiscal\s+(?:year\s+)?(\d{4})`)
	rePeriodEnd  = regexp.MustCompile(`(?i)(?:quarter\s+ended?|period\s+ended?|as\s+of)\s+([A-Za-z]+ \d{1,2},? \d{4}|\d{4}-\d{2}-\d{2}|\d{1,2}/\d{1,2}/\d{4})`)
	reMonthDay   = regexp.MustCompile(`(?i)(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2}),?\s+(\d{4})`)
)

func extractPeriod(text, norm string) EarningsPeriod {
	var p EarningsPeriod

	// Use the *first* quarter label that appears in the text — not a switch on
	// existence — so a title saying "Third Quarter" wins over a later comparison
	// line that mentions "Q2".
	type qm struct {
		q   string
		pos int
	}
	var candidates []qm
	if m := reQ1.FindStringIndex(norm); m != nil {
		candidates = append(candidates, qm{"Q1", m[0]})
	}
	if m := reQ2.FindStringIndex(norm); m != nil {
		candidates = append(candidates, qm{"Q2", m[0]})
	}
	if m := reQ3.FindStringIndex(norm); m != nil {
		candidates = append(candidates, qm{"Q3", m[0]})
	}
	if m := reQ4.FindStringIndex(norm); m != nil {
		candidates = append(candidates, qm{"Q4", m[0]})
	}
	if m := reFY.FindStringIndex(norm); m != nil {
		candidates = append(candidates, qm{"FY", m[0]})
	}
	if len(candidates) > 0 {
		best := candidates[0]
		for _, c := range candidates[1:] {
			if c.pos < best.pos {
				best = c
			}
		}
		p.FiscalQuarter = best.q
	}

	// Extract year — prefer explicit "fiscal YYYY" label over first year found,
	// since many releases have a period-end date (e.g. "December 28, 2025") that
	// predates the labeled fiscal year (e.g. "fiscal 2026").
	if m := reFiscalYear.FindStringSubmatch(text); len(m) > 1 {
		p.FiscalYear, _ = strconv.Atoi(m[1])
	} else {
		years := reYear.FindAllString(text, -1)
		if len(years) > 0 {
			p.FiscalYear, _ = strconv.Atoi(years[0])
		}
	}

	// Try to find an explicit period-end date.
	if m := rePeriodEnd.FindStringSubmatch(text); len(m) > 1 {
		p.PeriodEnd = normalizeDate(m[1])
	} else if m := reMonthDay.FindStringSubmatch(text); len(m) > 0 {
		p.PeriodEnd = normalizeDate(m[0])
	}

	// Estimate period_end from quarter + year when not explicit.
	if p.PeriodEnd == "" && p.FiscalYear > 0 {
		p.IsFiscalCalendarAligned = true
		switch p.FiscalQuarter {
		case "Q1":
			p.PeriodEnd = strconv.Itoa(p.FiscalYear) + "-03-31"
		case "Q2":
			p.PeriodEnd = strconv.Itoa(p.FiscalYear) + "-06-30"
		case "Q3":
			p.PeriodEnd = strconv.Itoa(p.FiscalYear) + "-09-30"
		case "Q4", "FY":
			p.PeriodEnd = strconv.Itoa(p.FiscalYear) + "-12-31"
		}
	}

	return p
}

// ---- issuer -----------------------------------------------------------

var reIssuerReports = regexp.MustCompile(`(?i)^([A-Z][A-Za-z0-9 ,\.&'-]{2,60})\s+(?:reports?|announces?|posts?)`)

func extractIssuer(text, norm string) string {
	// Look for "Company Name Reports Q1 2026 Results" near the top.
	lines := strings.SplitN(text, "\n", 10)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) < 5 {
			continue
		}
		if m := reIssuerReports.FindStringSubmatch(line); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// ---- EPS extraction ---------------------------------------------------
//
// Strategy: find all EPS-like numbers with their surrounding context,
// score each by how strongly the context indicates "current period GAAP diluted",
// return the best candidate.

type epsCandidate struct {
	value   float64
	context string
	score   int
}

// reDollarAmt matches $N.NN, $(N.NN) (loss), and plain N.NN in EPS context.
var (
	reDollarPositive = regexp.MustCompile(`\$\s*([\d,]+\.[\d]{1,4})`)
	reDollarLoss     = regexp.MustCompile(`\(\s*\$?\s*([\d,]+\.[\d]{1,4})\s*\)|\bloss\s+of\s+\$\s*([\d,]+\.[\d]{1,4})`)
	reDollarEPS      = regexp.MustCompile(`(?i)(?:\$\s*|\s)([\d,]+\.[\d]{1,4})\s+per\s+(?:diluted\s+)?share`)
	// Full context pattern: captures [loss indicator] [dollar sign] [number] ... per [diluted] share
	reEPSFull = regexp.MustCompile(`(?i)(?:(?:net\s+)?(?:income|loss|earnings?|profit)\s+(?:per|of\s+\$[\d,\.]+\s*(?:billion|million|B|M)?\s*(?:,\s*or\s+)?)?\$?\s*(?:\((\d[\d,\.]+)\)|(\d[\d,\.]+))\s*(?:per\s+(?:diluted\s+)?share)|\$?\s*(?:\((\d[\d,\.]+)\)|(\d[\d,\.]+))\s*per\s+diluted\s+share)`)
)

func extractGAAPDiluted(text, norm string) *float64 {
	candidates := gatherEPSCandidates(text, norm, true)
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return &best.value
}

func gatherEPSCandidates(text, norm string, gaapOnly bool) []epsCandidate {
	var candidates []epsCandidate

	// Find all "per diluted share" patterns and their values.
	perDilutedRe := regexp.MustCompile(`(?i)(?:\$\s*|\b)(\(?\d[\d,\.]+\)?)\s+per\s+diluted\s+share`)
	matches := perDilutedRe.FindAllStringSubmatchIndex(text, -1)

	for _, idx := range matches {
		if len(idx) < 4 {
			continue
		}
		valStr := text[idx[2]:idx[3]]
		val, isLoss := parseEPSValue(valStr)

		// Broad context (±200/+50 chars) for positive signal detection — GAAP and
		// financial terms may appear on an adjacent line, so cast a wide net.
		start := idx[0] - 200
		if start < 0 {
			start = 0
		}
		end := idx[1] + 50
		if end > len(text) {
			end = len(text)
		}
		ctx := strings.ToLower(text[start:end])

		// Tight leading context (50 chars) for penalty detection.  A prior-period
		// label or a non-GAAP label directly introduces its value, so 50 chars is
		// enough to catch "Previous quarter: GAAP EPS was $0.68" or
		// "Non-GAAP diluted EPS was $0.81".  Using a tight window avoids penalising
		// the current-period figure because a comparison clause or a non-GAAP
		// disclosure from a prior sentence appears nearby.
		tightStart := idx[0] - 50
		if tightStart < 0 {
			tightStart = 0
		}
		tightLead := strings.ToLower(text[tightStart:idx[0]])

		score := 0

		// GAAP indicators (positive score, broad window).
		if strings.Contains(ctx, "gaap") {
			score += 5
		}
		if strings.Contains(ctx, "diluted earnings") || strings.Contains(ctx, "diluted net") {
			score += 4
		}
		if strings.Contains(ctx, "net income") || strings.Contains(ctx, "net loss") {
			score += 3
		}

		// Non-GAAP penalty: tight leading window only — catches the direct label
		// "Non-GAAP … $X per diluted share" without penalising nearby GAAP figures
		// that happen to share a paragraph with a non-GAAP disclosure.
		if gaapOnly && (strings.Contains(tightLead, "non-gaap") || strings.Contains(tightLead, "non gaap") ||
			strings.Contains(tightLead, "adjusted") || strings.Contains(tightLead, "pro forma") ||
			strings.Contains(tightLead, "core earnings") || strings.Contains(tightLead, "operating eps")) {
			score -= 10
		}

		// Prior-period penalty: tight leading window only — catches introducing phrases
		// like "compared to $X", "Previous quarter: $X", "Q2 2025 comparison: $X".
		// Trailing "compared to …" is irrelevant: it means the *next* figure is the
		// comparison, not the current one.
		if strings.Contains(tightLead, "prior year") || strings.Contains(tightLead, "prior-year") ||
			strings.Contains(tightLead, "year ago") || strings.Contains(tightLead, "year-ago") ||
			strings.Contains(tightLead, "compared to") || strings.Contains(tightLead, "last year") ||
			strings.Contains(tightLead, "prior quarter") || strings.Contains(tightLead, "previous quarter") ||
			strings.Contains(tightLead, "last quarter") || strings.Contains(tightLead, "comparison") {
			score -= 5
		}

		// Continuing-ops basis (mild negative since we prefer total net).
		if strings.Contains(tightLead, "continuing operations") || strings.Contains(tightLead, "continuing ops") {
			score -= 1
		}

		if isLoss {
			val = -val
		}
		candidates = append(candidates, epsCandidate{value: val, context: ctx, score: score})
	}

	return candidates
}

// parseEPSValue parses a string like "2.40", "(0.34)", "2,40" into (float64, isLoss).
func parseEPSValue(s string) (float64, bool) {
	isLoss := false
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		isLoss = true
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, ",", "")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, isLoss
}

func extractGAAPBasic(text, norm string) *float64 {
	re := regexp.MustCompile(`(?i)\$?\s*(\(?\d[\d,\.]+\)?)\s+per\s+(?:basic\s+)?share[^s]`)
	// Look for context explicitly mentioning "basic" EPS.
	basicRe := regexp.MustCompile(`(?i)(?:basic\s+(?:earnings|eps|net)\s+per\s+share|earnings\s+per\s+(?:basic\s+)?share)[^d].*?\$\s*([\d,\.]+)|\$\s*([\d,\.]+)\s+per\s+basic\s+share`)
	if m := basicRe.FindStringSubmatch(text); len(m) > 0 {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				v, _ := strconv.ParseFloat(strings.ReplaceAll(m[i], ",", ""), 64)
				if v > 0 {
					return &v
				}
			}
		}
	}
	_ = re
	return nil
}

var (
	reAdjusted = regexp.MustCompile(`(?i)(?:non-gaap|adjusted|core|pro.?forma)\s+(?:diluted\s+)?(?:earnings\s+per\s+share|eps|net\s+income\s+per\s+(?:diluted\s+)?share)[^\d]*\$?\s*([\d,\.]+)|\$?\s*([\d,\.]+)\s+per\s+(?:diluted\s+)?share[^.]*?(?:non-gaap|adjusted|exclud)`)
)

func extractAdjusted(text, norm string) *float64 {
	if m := reAdjusted.FindStringSubmatch(text); len(m) > 0 {
		for i := 1; i < len(m); i++ {
			if m[i] != "" {
				v, err := strconv.ParseFloat(strings.ReplaceAll(m[i], ",", ""), 64)
				if err == nil && v > 0 {
					return &v
				}
			}
		}
	}
	return nil
}

// ---- net income -------------------------------------------------------

var reNetIncome = regexp.MustCompile(`(?i)net\s+(?:income|earnings|profit)\s+(?:was|of|were)?\s+\$?\s*([\d,\.]+)\s*(billion|million|B\b|M\b)`)

func extractNetIncome(text, norm string) *float64 {
	m := reNetIncome.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return nil
	}
	v = toMillions(v, m[2])
	return &v
}

// ---- shares diluted ---------------------------------------------------

var reSharesDiluted = regexp.MustCompile(`(?i)(?:approximately\s+)?([\d,\.]+)\s*(billion|million|B\b|M\b)\s+(?:diluted\s+)?(?:weighted[- ]average\s+)?(?:diluted\s+)?shares`)

func extractSharesDiluted(text, norm string) *float64 {
	m := reSharesDiluted.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return nil
	}
	v = toMillions(v, m[2])
	return &v
}

// ---- revenue ----------------------------------------------------------

var reRevenue = regexp.MustCompile(`(?i)(?:net\s+)?(?:revenue|sales|net\s+sales)\s+(?:of|was|were)?\s+\$?\s*([\d,\.]+)\s*(billion|million|B\b|M\b)`)

func extractRevenue(text, norm string) *float64 {
	m := reRevenue.FindStringSubmatch(text)
	if len(m) < 3 {
		return nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return nil
	}
	v = toMillions(v, m[2])
	return &v
}

// ---- YoY EPS ----------------------------------------------------------

var reYoY = regexp.MustCompile(`(?i)(?:compared\s+to|vs\.?|prior.year|year.ago|same\s+period\s+(?:last|prior)\s+year)[^\d]*\$\s*([\d,\.]+)\s+per\s+(?:diluted\s+)?share`)

func extractYoYEPS(text, norm string, current *float64) *float64 {
	m := reYoY.FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil || v == 0 {
		return nil
	}
	// If the YoY number is the same as the current EPS, it's likely the same
	// number repeated — skip it to avoid contaminating the YoY field.
	if current != nil && math.Abs(v-*current) < 0.001 {
		return nil
	}
	return &v
}

// ---- continuing ops ---------------------------------------------------

func hasContinuingOpsLanguage(norm string) bool {
	return strings.Contains(norm, "continuing operations") &&
		(strings.Contains(norm, "per share") || strings.Contains(norm, "eps"))
}

// ---- confidence scoring -----------------------------------------------

func scoreConfidence(e *EarningsData, norm string) float64 {
	score := 0.0

	// Core signal: did we get a diluted EPS?
	if e.EPSGAAPDiluted != nil {
		score += 0.40
	} else if e.EPSAdjusted != nil {
		score += 0.20
	}

	// Period quality.
	if e.Period.FiscalQuarter != "" {
		score += 0.15
	}
	if e.Period.FiscalYear > 0 {
		score += 0.10
	}
	if e.Period.PeriodEnd != "" {
		score += 0.05
	}

	// Supporting data.
	if e.NetIncome != nil {
		score += 0.10
	}
	if e.SharesDiluted != nil {
		score += 0.05
	}
	if e.Revenue != nil {
		score += 0.05
	}
	if e.Issuer != "" {
		score += 0.05
	}

	// GAAP explicitly mentioned near the headline number.
	if e.EPSGAAPDiluted != nil && strings.Contains(norm, "gaap") {
		score += 0.05
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

// ---- helpers ----------------------------------------------------------

func toMillions(v float64, unit string) float64 {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "billion", "b":
		return v * 1000
	default: // million, M
		return v
	}
}

// normalizeDate converts common date formats to YYYY-MM-DD.
// Handles: "March 31, 2026", "March 31 2026", "2026-03-31", "3/31/2026".
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)

	// Already ISO.
	if regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(s) {
		return s
	}

	months := map[string]string{
		"january": "01", "february": "02", "march": "03", "april": "04",
		"may": "05", "june": "06", "july": "07", "august": "08",
		"september": "09", "october": "10", "november": "11", "december": "12",
	}

	// "Month DD, YYYY" or "Month DD YYYY"
	if m := reMonthDay.FindStringSubmatch(s); len(m) == 4 {
		mon := months[strings.ToLower(m[1])]
		day := m[2]
		if len(day) == 1 {
			day = "0" + day
		}
		return m[3] + "-" + mon + "-" + day
	}

	// M/D/YYYY
	if m := regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})$`).FindStringSubmatch(s); len(m) == 4 {
		mon := m[1]
		if len(mon) == 1 {
			mon = "0" + mon
		}
		day := m[2]
		if len(day) == 1 {
			day = "0" + day
		}
		return m[3] + "-" + mon + "-" + day
	}

	return ""
}
