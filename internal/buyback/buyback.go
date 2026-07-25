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
	// "buyback"/"buy-back"/"buy back" are unambiguous on their own. Bare
	// "repurchase" is not -- real false positives found live (S170-06):
	// "customer repurchase rate" (an ecommerce retention metric) and
	// "repurchases of common stock" figures buried in routine financial
	// tables both use the word with nothing to do with a buyback
	// announcement. Requiring share/stock proximity (or "repurchase ...
	// program/plan/agreement", small gap allowed -- "repurchased under the
	// program" is real, common phrasing) is what every genuine buyback
	// mention actually contains.
	reBuybackCore = regexp.MustCompile(`(?i)\b(?:buyback|buy back|buy-back)\b|\b(?:share|stock)s?\s+repurchases?\b|\brepurchases?\s+of\s+(?:its|our|common)\b|\brepurchase\w*\s+(?:\S+\s+){0,3}(?:program|plan|agreement)\b`)

	// Suspension / termination — check before authorization to avoid false positives.
	reSuspend = regexp.MustCompile(`(?i)\b(suspend|terminat|halt|discontinu|end|ceas)\w*\s+(?:its\s+)?(?:share\s+)?(?:repurchase|buyback)\b`)

	// New authorization. Includes "normal course issuer bid" / "NCIB" as a
	// direct alternative to repurchase/buyback -- standard Canadian
	// securities terminology for exactly a buyback program (confirmed live,
	// S170-06: a genuine CAE "renewal of normal course issuer bid" press
	// release doesn't use the word "repurchase" anywhere near the actual
	// authorize/renew verb -- "repurchase" only appears many sentences later
	// describing purchase mechanics -- so without this alternative the real
	// authorization language is invisible to this regex even though the
	// event is completely genuine). "renew" is included as an authorize-type
	// verb for the same reason: renewing an NCIB is functionally a new
	// authorization.
	reAuthorize = regexp.MustCompile(`(?i)\b(authoriz|approv|announc|initiat|adopt|renew)\w*\s+(?:a\s+)?(?:the\s+)?(?:its\s+)?(?:new\s+)?(?:share\s+)?(?:repurchase|buyback|normal course issuer bid|NCIB)\b`)

	// Completion — "completed [its|the] ... repurchase/buyback program".
	// Requires proximity to the core word, not just co-occurrence anywhere in
	// the text (S170-06: the switch below used to check completion-verb
	// presence and buyback-word presence independently, with no proximity
	// requirement at all -- this regex existed with the right proximity
	// constraint but was dead code, never actually referenced). Gap uses
	// \S+ (not \w+) and a 6-token budget, not 2 -- a real sentence like
	// "completed its previously authorized $60 billion share repurchase
	// program" has 5 tokens between "its" and "repurchase", including a
	// dollar amount \w+ can't match at all (leading $). A tight \w+{0,2}
	// gap silently fails on any real dollar-figure sentence, which is
	// plausibly why this regex went unused in the first place.
	reComplete = regexp.MustCompile(`(?i)\b(complet|finish|exhaust)\w*\s+(?:its\s+)?(?:the\s+)?(?:\S+\s+){0,6}(?:repurchase|buyback)\b`)

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

	// anchorRe scopes dollar/share extraction to the SAME regex that drove
	// the classification, not the generic reBuybackCore gate -- real bug
	// found live (S170-06): a genuine authorization press release states
	// both the authorized cap ("may repurchase up to $10 million") AND a
	// separately-reported cumulative amount already spent under the program
	// ("has repurchased ... for approximately $4.9 million") in the same
	// document. Generic proximity-to-any-buyback-word picked whichever
	// number happened to sit closer to ANY mention, which isn't necessarily
	// the number that matches what actually got classified (the authorized
	// cap for an authorization event, not a running tally). Anchoring to
	// reAuthorize's own match keeps the search near the authorize language
	// specifically.
	var anchorRe *regexp.Regexp
	switch {
	case reSuspend.MatchString(combined):
		ev.EventType = EventSuspension
		anchorRe = reSuspend
	case reComplete.MatchString(combined):
		ev.EventType = EventCompletion
		anchorRe = reComplete
	case reAuthorize.MatchString(combined):
		ev.EventType = EventAuthorization
		anchorRe = reAuthorize
	default:
		ev.EventType = EventUpdate
		anchorRe = reBuybackCore
	}

	ev.AuthorizedUSD = extractUSD(combined, anchorRe)
	ev.AuthorizedShares = extractShares(combined, anchorRe)
	return ev
}

// proximityWindowChars bounds how far a dollar/share figure can sit from a
// buyback/repurchase word and still be attributed to it. Real press releases
// report many unrelated dollar amounts in the same document (revenue, cash
// flow, market cap) -- picking "the first one anywhere in the text" silently
// mislabels those as the buyback's own size. Confirmed live (S170-06):
// Docusign's real repurchase figure is $317.5M, sitting right next to
// "Repurchases of common stock were $317.5 million" -- but the first dollar
// figure in the whole press release is unrelated revenue, $830.2M, reported
// several paragraphs earlier.
const proximityWindowChars = 200

func extractUSD(text string, anchorRe *regexp.Regexp) float64 {
	m := nearestToBuyback(text, reUSD, anchorRe)
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

// rePriorPeriod matches the lead-in to a prior-period comparison figure --
// "$X million compared to $Y million in the same period last year" is
// extremely common in earnings press releases (real example, S170-06:
// Docusign's release uses this exact shape four separate times). $Y is
// structurally never this period's real figure, so a value immediately
// preceded by "compared to"/"vs"/"versus" is excluded as a candidate
// entirely, rather than relying on proximity alone to avoid picking it.
var rePriorPeriod = regexp.MustCompile(`(?i)(?:compared to|vs\.?|versus)\s*$`)

// nearestToBuyback returns the submatch groups of valueRe's occurrence
// closest (by character distance) to any anchorRe occurrence in text, or
// nil if none exists within proximityWindowChars.
func nearestToBuyback(text string, valueRe, anchorRe *regexp.Regexp) []string {
	keywordLocs := anchorRe.FindAllStringIndex(text, -1)
	if len(keywordLocs) == 0 {
		return nil
	}
	valueMatches := valueRe.FindAllStringSubmatchIndex(text, -1)
	if valueMatches == nil {
		return nil
	}
	best := -1
	bestDist := -1
	for i, vm := range valueMatches {
		vStart, vEnd := vm[0], vm[1]
		lookback := vStart - 20
		if lookback < 0 {
			lookback = 0
		}
		if rePriorPeriod.MatchString(text[lookback:vStart]) {
			continue
		}
		for _, kw := range keywordLocs {
			kStart, kEnd := kw[0], kw[1]
			dist := 0
			switch {
			case vEnd <= kStart:
				dist = kStart - vEnd
			case kEnd <= vStart:
				dist = vStart - kEnd
			}
			if bestDist == -1 || dist < bestDist {
				bestDist = dist
				best = i
			}
		}
	}
	if best == -1 || bestDist > proximityWindowChars {
		return nil
	}
	vm := valueMatches[best]
	groups := make([]string, len(vm)/2)
	for i := range groups {
		if vm[2*i] < 0 {
			continue
		}
		groups[i] = text[vm[2*i]:vm[2*i+1]]
	}
	return groups
}

func extractShares(text string, anchorRe *regexp.Regexp) float64 {
	m := nearestToBuyback(text, reShares, anchorRe)
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
