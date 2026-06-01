package guidance

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Guidance trigger words — any of these near a number suggest guidance text.
	reGuidanceTrigger = regexp.MustCompile(
		`(?i)\b(guidance|outlook|expects?\s+to|forecast|anticipates?\s+to|project(?:s|ing)?|guides?)\b`)

	// Action words immediately before or after "guidance" / "outlook".
	// Order of detection in the switch: withdrawn > raised > lowered > maintained > initiated.
	reRaised     = regexp.MustCompile(`(?i)\b(rais(?:ed|ing|es?)|increas(?:ed|ing|es?)|upward(?:ly)?)\b`)
	reLowered    = regexp.MustCompile(`(?i)\b(lower(?:ed|ing|s?)|reduc(?:ed|ing|es?)|downward(?:ly)?|cut(?:ting)?)\b`)
	reMaintained = regexp.MustCompile(`(?i)\b(maintain(?:ed|ing|s?)|reaffirm(?:ed|ing|s?)|affirm(?:ed|ing|s?)|reiterat(?:ed|ing|es?)|confirm(?:ed|ing|s?))\b`)
	reWithdrawn  = regexp.MustCompile(`(?i)\b(withdrew?|suspend(?:ed|ing|s?)|withdraw(?:ing|n)?|rescind(?:ed|ing)?)\b`)
	// reInitiated requires "initiat" — avoids false match on "previously issued guidance".
	reInitiated  = regexp.MustCompile(`(?i)\b(initiat(?:ed|ing|es?)|provid(?:ed|ing)\s+(?:new|initial)\s+guidance)\b`)

	// EPS range: "$X.XX to $X.XX" or "$X.XX–$X.XX" or "$X.XX - $X.XX".
	reEPSRange = regexp.MustCompile(
		`\$\s*([\d]+\.[\d]{1,2})\s*(?:to|[-–])\s*\$?\s*([\d]+\.[\d]{1,2})`)

	// Single EPS point: "$X.XX per diluted share" or "EPS of $X.XX".
	reEPSPoint = regexp.MustCompile(
		`(?i)(?:eps|diluted)\s+(?:of|at)\s+\$([\d]+\.[\d]{1,2})`)

	// Revenue range: "$X.X billion to $Y.Y billion" or "$X.X to $Y.Y billion".
	// The unit may appear after each number or only after the second number.
	reRevRange = regexp.MustCompile(
		`(?i)\$\s*([\d]+\.?[\d]*)\s*(?:billion|million|B\b|M\b)?\s*(?:to|[-–])\s*\$?\s*([\d]+\.?[\d]*)\s*(billion|million|B\b|M\b)`)

	// Revenue multiplier words.
	reBillion = regexp.MustCompile(`(?i)\b(billion|B)\b`)
	reMillion = regexp.MustCompile(`(?i)\b(million|M)\b`)

	// Quarter / period detection (same as eps package).
	reQ1 = regexp.MustCompile(`(?i)\b(?:first\s+quarter|q1|1q)\b`)
	reQ2 = regexp.MustCompile(`(?i)\b(?:second\s+quarter|q2|2q)\b`)
	reQ3 = regexp.MustCompile(`(?i)\b(?:third\s+quarter|q3|3q)\b`)
	reQ4 = regexp.MustCompile(`(?i)\b(?:fourth\s+quarter|q4|4q)\b`)
	reFY = regexp.MustCompile(`(?i)\b(?:full.?year|fiscal\s+year|annual|fy)\b`)
	reYear = regexp.MustCompile(`\b(20[2-9][0-9])\b`)
)

// Extract scans a press release for forward-looking guidance statements.
// Returns a GuidanceData; check HasGuidance before publishing.
func Extract(text, sourceIdentity, ticker string) *GuidanceData {
	g := &GuidanceData{
		SourceIdentity: sourceIdentity,
		Ticker:         ticker,
	}
	norm := strings.ToLower(text)

	// Only proceed if the text contains guidance trigger words.
	if !reGuidanceTrigger.MatchString(norm) {
		return g
	}

	// Detect action. Order: withdrawn > raised > lowered > maintained > initiated.
	// raised/lowered checked before initiated to avoid "previously issued guidance"
	// triggering ActionInitiated when the context is clearly a guidance raise/lower.
	switch {
	case reWithdrawn.MatchString(norm):
		g.Action = ActionWithdrawn
	case reRaised.MatchString(norm):
		g.Action = ActionRaised
	case reLowered.MatchString(norm):
		g.Action = ActionLowered
	case reMaintained.MatchString(norm):
		g.Action = ActionMaintained
	case reInitiated.MatchString(norm):
		g.Action = ActionInitiated
	default:
		g.Action = ActionUpdated
	}

	// Extract EPS guidance range.
	g.EPSLow, g.EPSHigh = extractEPSRange(text, norm)

	// Extract revenue guidance range.
	g.RevenueLow, g.RevenueHigh, g.RevenueUnit = extractRevenueRange(text)

	// Determine metric.
	hasEPS := g.EPSLow != nil || g.EPSHigh != nil
	hasRev := g.RevenueLow != nil || g.RevenueHigh != nil
	switch {
	case hasEPS && hasRev:
		g.Metric = MetricBoth
	case hasRev:
		g.Metric = MetricRevenue
	case hasEPS:
		g.Metric = MetricEPS
	}

	g.HasGuidance = hasEPS || hasRev || g.Action == ActionWithdrawn

	// Extract period.
	g.Period = extractPeriod(norm)

	// Compute confidence.
	g.Confidence = scoreConfidence(g, norm)

	return g
}

func extractEPSRange(text, norm string) (*float64, *float64) {
	// Only search within sentences that contain guidance language.
	sentences := splitSentences(text)
	for _, s := range sentences {
		sn := strings.ToLower(s)
		if !reGuidanceTrigger.MatchString(sn) && !strings.Contains(sn, "per diluted share") && !strings.Contains(sn, "eps of") {
			continue
		}
		// Range first.
		if m := reEPSRange.FindStringSubmatch(s); m != nil {
			lo, err1 := strconv.ParseFloat(m[1], 64)
			hi, err2 := strconv.ParseFloat(m[2], 64)
			if err1 == nil && err2 == nil && lo <= hi && lo > 0 {
				return &lo, &hi
			}
		}
		// Point.
		if m := reEPSPoint.FindStringSubmatch(s); m != nil {
			v, err := strconv.ParseFloat(m[1], 64)
			if err == nil && v > 0 {
				return &v, &v
			}
		}
	}
	return nil, nil
}

func extractRevenueRange(text string) (*float64, *float64, float64) {
	sentences := splitSentences(text)
	for _, s := range sentences {
		sn := strings.ToLower(s)
		if !reGuidanceTrigger.MatchString(sn) && !strings.Contains(sn, "revenue") {
			continue
		}
		m := reRevRange.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		lo, err1 := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		hi, err2 := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", ""), 64)
		if err1 != nil || err2 != nil || lo > hi || lo <= 0 {
			continue
		}
		unit := 1.0
		if reBillion.MatchString(m[3]) {
			unit = 1000.0
		}
		loM := lo * unit
		hiM := hi * unit
		return &loM, &hiM, unit
	}
	return nil, nil, 0
}

func extractPeriod(norm string) Period {
	p := Period{}
	switch {
	case reFY.MatchString(norm):
		p.FiscalQuarter = "FY"
	case reQ4.MatchString(norm):
		p.FiscalQuarter = "Q4"
	case reQ3.MatchString(norm):
		p.FiscalQuarter = "Q3"
	case reQ2.MatchString(norm):
		p.FiscalQuarter = "Q2"
	case reQ1.MatchString(norm):
		p.FiscalQuarter = "Q1"
	}
	if m := reYear.FindString(norm); m != "" {
		y, err := strconv.Atoi(m)
		if err == nil {
			p.FiscalYear = y
		}
	}
	return p
}

func scoreConfidence(g *GuidanceData, norm string) float64 {
	if !g.HasGuidance {
		return 0
	}
	score := 0.5 // base for trigger word match

	if g.EPSLow != nil && g.EPSHigh != nil {
		score += 0.25
	}
	if g.RevenueLow != nil && g.RevenueHigh != nil {
		score += 0.15
	}
	if g.Action != ActionUpdated && g.Action != "" {
		score += 0.10
	}
	if g.Period.FiscalQuarter != "" {
		score += 0.05
	}
	if g.Action == ActionWithdrawn {
		// Withdrawn guidance is high confidence even without numbers.
		score = math.Max(score, 0.75)
	}
	return math.Min(score, 1.0)
}

// reSentenceSplit splits on ". " or ".\n" to avoid splitting decimal numbers.
var reSentenceSplit = regexp.MustCompile(`\.\s+`)

// splitSentences splits on period+whitespace (not on decimal points in numbers).
func splitSentences(text string) []string {
	parts := reSentenceSplit.Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if len(s) > 20 {
			out = append(out, s)
		}
	}
	return out
}

// reMillion is used in extractRevenueRange.
var _ = reMillion
