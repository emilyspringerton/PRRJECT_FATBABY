// Package buyback classifies press release bodies as share repurchase
// announcements and scores buyback_authorization / buyback_suspension signals.
//
// Signal mapping:
//   authorization / completion → buyback_authorization (moderately bullish)
//   suspension / termination   → buyback_suspension    (moderately bearish)
//   regular update             → no signal
package buyback

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// EventType classifies the buyback announcement.
type EventType string

const (
	EventAuthorization EventType = "authorization"
	EventCompletion    EventType = "completion"
	EventSuspension    EventType = "suspension"
	EventUpdate        EventType = "update" // routine progress update — not scored
)

// BuybackEvent holds structured data from a buyback press release.
type BuybackEvent struct {
	Ticker          string
	Headline        string
	EventType       EventType
	AuthorizedUSD   float64 // authorized dollar amount; 0 if not parsed
	AuthorizedShares float64 // authorized share count; 0 if not parsed
	SourceURL       string
	PublishedAt     string
}

var (
	reBuybackCore = regexp.MustCompile(`(?i)\b(repurchase|buyback|buy back|buy-back)\b`)

	// Suspension / termination — check before authorization to avoid false positives.
	reSuspend = regexp.MustCompile(`(?i)\b(suspend|terminat|halt|discontinu|end|ceas)\w*\s+(?:its\s+)?(?:share\s+)?(?:repurchase|buyback)\b`)

	// New authorization.
	reAuthorize = regexp.MustCompile(`(?i)\b(authoriz|approv|announc|initiat|adopt)\w*\s+(?:a\s+)?(?:new\s+)?(?:share\s+)?(?:repurchase|buyback)\b`)

	// Completion — "completed [its|the] ... repurchase/buyback program".
	// Uses separate verbs + core word proximity; avoids dollar-amount gaps.
	reCompleteVerb = regexp.MustCompile(`(?i)\b(complet|finish|exhaust)\w*\b`)
	reComplete     = regexp.MustCompile(`(?i)\b(complet|finish|exhaust)\w*\s+(?:its\s+)?(?:the\s+)?(?:\w+\s+){0,2}(?:repurchase|buyback)\b`)

	// Dollar amount: "$500 million", "$1.5 billion", "$200M"
	reUSD = regexp.MustCompile(`(?i)\$\s*([\d,]+(?:\.[\d]+)?)\s*(billion|million|bn|mn|m|b)\b`)

	// Share count: "10 million shares", "5,000,000 shares"
	reShares = regexp.MustCompile(`(?i)([\d,]+(?:\.[\d]+)?)\s*(million|billion)?\s*shares`)
)

// Classify examines headline and body text for buyback announcement patterns.
// Returns nil when the text is not a buyback announcement.
func Classify(headline, body string) *BuybackEvent {
	combined := headline + "\n" + body
	if !reBuybackCore.MatchString(combined) {
		return nil
	}

	ev := &BuybackEvent{Headline: headline}

	switch {
	case reSuspend.MatchString(combined):
		ev.EventType = EventSuspension
	case reCompleteVerb.MatchString(combined) && reBuybackCore.MatchString(combined):
		ev.EventType = EventCompletion
	case reAuthorize.MatchString(combined):
		ev.EventType = EventAuthorization
	default:
		ev.EventType = EventUpdate
	}

	ev.AuthorizedUSD = extractUSD(combined)
	ev.AuthorizedShares = extractShares(combined)
	return ev
}

func extractUSD(text string) float64 {
	m := reUSD.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	raw := strings.ReplaceAll(m[1], ",", "")
	v, _ := strconv.ParseFloat(raw, 64)
	switch strings.ToLower(m[2]) {
	case "billion", "bn", "b":
		v *= 1e9
	case "million", "mn", "m":
		v *= 1e6
	}
	return v
}

func extractShares(text string) float64 {
	m := reShares.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	raw := strings.ReplaceAll(m[1], ",", "")
	v, _ := strconv.ParseFloat(raw, 64)
	if strings.EqualFold(m[2], "million") {
		v *= 1e6
	} else if strings.EqualFold(m[2], "billion") {
		v *= 1e9
	}
	return v
}

// Score converts a classified BuybackEvent into entity-graph signals.
// Update events (routine progress) return nil.
func Score(ev *BuybackEvent) []entitygraph.Signal {
	if ev == nil || ev.EventType == EventUpdate || ev.Ticker == "" {
		return nil
	}

	today := time.Now().UTC().Format("2006-01-02")
	validThrough := time.Now().UTC().AddDate(0, 12, 0).Format("2006-01-02")

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
	score := 0.4

	amtStr := ""
	if ev.AuthorizedUSD > 0 {
		amtStr = fmt.Sprintf(" ($%.0fM program)", ev.AuthorizedUSD/1e6)
	} else if ev.AuthorizedShares > 0 {
		amtStr = fmt.Sprintf(" (%.0f shares authorized)", ev.AuthorizedShares)
	}

	switch ev.EventType {
	case EventAuthorization:
		sigType = entitygraph.SignalBuybackAuthorization
		sev = entitygraph.SeverityLow
		conf = 0.75
		score = 0.45
		interp = fmt.Sprintf("%s authorized a share repurchase program%s. Management signalling belief that stock is undervalued. Bullish when governance health is stable; check for debt-funded buybacks as a risk factor.", ev.Ticker, amtStr)
	case EventCompletion:
		sigType = entitygraph.SignalBuybackAuthorization
		sev = entitygraph.SeverityLow
		conf = 0.72
		score = 0.35
		interp = fmt.Sprintf("%s completed its share repurchase program%s. Execution of buyback as announced — signals financial follow-through and continued confidence in capital allocation.", ev.Ticker, amtStr)
	case EventSuspension:
		sigType = entitygraph.SignalBuybackSuspension
		sev = entitygraph.SeverityMedium
		conf = 0.78
		score = 0.60
		interp = fmt.Sprintf("%s suspended or terminated its share repurchase program%s. Buyback suspension suggests management is conserving cash — may indicate upcoming capital needs, regulatory pressure, or deteriorating financial position.", ev.Ticker, amtStr)
	default:
		return nil
	}

	sigID := fmt.Sprintf("%s_%s_%s", string(sigType), strings.ToLower(ev.Ticker), sigDate)
	meta := map[string]string{
		"event_type": string(ev.EventType),
		"source_url": ev.SourceURL,
	}
	if ev.AuthorizedUSD > 0 {
		meta["authorized_usd"] = fmt.Sprintf("%.0f", ev.AuthorizedUSD)
	}
	if ev.AuthorizedShares > 0 {
		meta["authorized_shares"] = fmt.Sprintf("%.0f", ev.AuthorizedShares)
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
