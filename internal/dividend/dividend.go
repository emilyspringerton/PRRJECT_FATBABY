// Package dividend classifies press release bodies as dividend announcements
// and extracts structured dividend event data.
//
// Signal mapping:
//   - Cut / suspension → SignalDividendCut (high severity)
//   - Raise / increase → SignalDividendRaise
//   - Special / extra  → SignalSpecialDividend
//   - Regular (unchanged) → no signal (noise)
package dividend

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// EventType classifies the dividend announcement.
type EventType string

const (
	EventCut       EventType = "cut"
	EventSuspension EventType = "suspension"
	EventRaise     EventType = "raise"
	EventSpecial   EventType = "special"
	EventRegular   EventType = "regular" // unchanged quarterly — not scored as a signal
)

// DividendEvent holds structured data extracted from a dividend press release.
type DividendEvent struct {
	Ticker        string
	Headline      string
	EventType     EventType
	AmountPerShare float64 // dollars; 0 if not parsed
	RecordDate    string  // YYYY-MM-DD or empty
	PaymentDate   string  // YYYY-MM-DD or empty
	SourceURL     string
	PublishedAt   string
}

// ── Detection ─────────────────────────────────────────────────────────────────

var (
	// Mandatory — must be present or it's not a dividend PR.
	reDividendCore = regexp.MustCompile(`(?i)\b(dividend|distribution)\b`)

	// Cut / suspension patterns. Suspension is checked first in the switch.
	// Both patterns allow "its quarterly cash" intervening words.
	reSuspend = regexp.MustCompile(`(?i)\b(suspend|eliminat|omit)\w*\s+(?:its\s+)?(?:quarterly\s+)?(?:cash\s+)?dividend\b`)
	reCut     = regexp.MustCompile(`(?i)\b(cut|reduc|decreas|discontinu)\w*\s+(?:its\s+)?(?:quarterly\s+)?(?:cash\s+)?dividend\b`)

	// Raise / increase patterns.
	reRaise = regexp.MustCompile(`(?i)\b(increas|rais|boost|upward|higher|grow)\w*\s+(?:its\s+)?(?:quarterly\s+)?(?:cash\s+)?dividend\b`)

	// Special dividend patterns.
	reSpecial = regexp.MustCompile(`(?i)\bspecial\s+(?:cash\s+)?dividend\b|\bextra\s+(?:cash\s+)?dividend\b|\bone-time\s+(?:cash\s+)?dividend\b`)

	// Amount extraction: "$0.35 per share", "35 cents per share".
	reAmount = regexp.MustCompile(`(?i)\$\s*([\d]+\.[\d]{1,4})\s+per\s+share|([\d]+\.[\d]{1,4})\s+(?:cents?|per\s+share)`)

	// Date extraction.
	reDateLabel = regexp.MustCompile(`(?i)(record|payment|payable|pay)\s+date[:\s]+([A-Z][a-z]+ \d{1,2},? \d{4}|\d{4}-\d{2}-\d{2})`)
	reDateMDY   = regexp.MustCompile(`(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2}),?\s+(\d{4})`)
)

// Classify examines headline and body text for dividend announcement patterns.
// Returns nil if the text is not a dividend announcement.
func Classify(headline, body string) *DividendEvent {
	combined := headline + "\n" + body
	if !reDividendCore.MatchString(combined) {
		return nil
	}

	ev := &DividendEvent{
		Headline: headline,
	}

	switch {
	case reSuspend.MatchString(combined):
		ev.EventType = EventSuspension
	case reCut.MatchString(combined):
		ev.EventType = EventCut
	case reSpecial.MatchString(combined):
		ev.EventType = EventSpecial
	case reRaise.MatchString(combined):
		ev.EventType = EventRaise
	default:
		// Regular unchanged quarterly — common, low signal value.
		ev.EventType = EventRegular
	}

	ev.AmountPerShare = extractAmount(combined)
	ev.RecordDate, ev.PaymentDate = extractDates(combined)
	return ev
}

func extractAmount(text string) float64 {
	m := reAmount.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	// Group 1 = dollar amount, group 2 = cents
	raw := m[1]
	if raw == "" {
		raw = m[2]
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if m[1] == "" && v > 0 {
		// "cents per share" format
		v = v / 100.0
	}
	return math.Round(v*10000) / 10000
}

func extractDates(text string) (recordDate, paymentDate string) {
	matches := reDateLabel.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		label := strings.ToLower(m[1])
		raw := strings.TrimSpace(m[2])
		parsed := parseHumanDate(raw)
		switch {
		case strings.Contains(label, "record"):
			recordDate = parsed
		case strings.Contains(label, "pay"):
			paymentDate = parsed
		}
	}
	return
}

func parseHumanDate(s string) string {
	// Try YYYY-MM-DD first.
	if len(s) == 10 && s[4] == '-' {
		return s
	}
	// "January 15, 2026" or "January 15 2026"
	if m := reDateMDY.FindStringSubmatch(s); m != nil {
		months := map[string]string{
			"January": "01", "February": "02", "March": "03", "April": "04",
			"May": "05", "June": "06", "July": "07", "August": "08",
			"September": "09", "October": "10", "November": "11", "December": "12",
		}
		mon := months[m[1]]
		day := fmt.Sprintf("%02s", m[2])
		return m[3] + "-" + mon + "-" + day
	}
	return s
}

// ── Signal scoring ────────────────────────────────────────────────────────────

// Score converts a classified DividendEvent into entity-graph signals.
// Regular dividends (no change) return nil — they are not significant signals.
func Score(ev *DividendEvent) []entitygraph.Signal {
	if ev == nil || ev.EventType == EventRegular || ev.Ticker == "" {
		return nil
	}

	today := time.Now().UTC().Format("2006-01-02")
	validThrough := time.Now().UTC().AddDate(0, 6, 0).Format("2006-01-02")

	sigDate := ev.PublishedAt
	if sigDate == "" {
		sigDate = today
	}
	if len(sigDate) > 10 {
		sigDate = sigDate[:10]
	}

	var sigType entitygraph.SignalType
	var sev entitygraph.Severity
	var conf float64
	var interp string
	score := 0.5

	amtStr := ""
	if ev.AmountPerShare > 0 {
		amtStr = fmt.Sprintf(" ($%.4f/share)", ev.AmountPerShare)
	}

	switch ev.EventType {
	case EventCut:
		sigType = entitygraph.SignalDividendCut
		sev = entitygraph.SeverityHigh
		conf = 0.82
		score = 0.70
		interp = fmt.Sprintf("%s reduced its dividend%s. Dividend cuts indicate deteriorating cash flow, rising leverage, or a strategic capital reallocation. Strong bearish divergence signal when paired with governance stress.", ev.Ticker, amtStr)
	case EventSuspension:
		sigType = entitygraph.SignalDividendCut
		sev = entitygraph.SeverityCritical
		conf = 0.88
		score = 0.90
		interp = fmt.Sprintf("%s suspended its dividend entirely%s. Complete suspension signals severe financial stress or a major strategic pivot. Highly bearish — historically precedes significant price dislocation.", ev.Ticker, amtStr)
	case EventRaise:
		sigType = entitygraph.SignalDividendRaise
		sev = entitygraph.SeverityLow
		conf = 0.78
		score = 0.55
		interp = fmt.Sprintf("%s raised its dividend%s. Dividend increases signal management confidence in sustained free cash flow generation. Bullish when paired with low governance stress.", ev.Ticker, amtStr)
	case EventSpecial:
		sigType = entitygraph.SignalSpecialDividend
		sev = entitygraph.SeverityLow
		conf = 0.80
		score = 0.60
		interp = fmt.Sprintf("%s declared a special/one-time dividend%s. Special dividends indicate excess cash or monetisation of an asset — management is returning capital rather than reinvesting it.", ev.Ticker, amtStr)
	default:
		return nil
	}

	sigID := fmt.Sprintf("%s_%s_%s", string(sigType), strings.ToLower(ev.Ticker), sigDate)
	meta := map[string]string{
		"event_type":      string(ev.EventType),
		"amount_per_share": fmt.Sprintf("%.4f", ev.AmountPerShare),
		"source_url":      ev.SourceURL,
	}
	if ev.RecordDate != "" {
		meta["record_date"] = ev.RecordDate
	}
	if ev.PaymentDate != "" {
		meta["payment_date"] = ev.PaymentDate
	}

	return []entitygraph.Signal{{
		SignalID:       sigID,
		Type:           sigType,
		Ticker:         ev.Ticker,
		Severity:       sev,
		Confidence:     conf,
		Score:          score,
		DetectedAt:     today,
		FilingDate:     sigDate,
		ValidThrough:   validThrough,
		Interpretation: interp,
		Metadata:       meta,
	}}
}
