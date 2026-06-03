package earningscal

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExtractDate attempts to find an upcoming earnings report date from a press
// release body. Returns ("", false) when no credible date announcement is found.
//
// Detects patterns such as:
//   "to report Q3 2026 results on November 14, 2026"
//   "will release fiscal Q1 results on Nov. 14"
//   "earnings release scheduled for November 14, 2026"
//   "conference call on November 14, 2026"
func ExtractDate(text string) (reportDate string, ok bool) {
	norm := strings.ToLower(text)

	// Gate: must contain earnings-announcement language.
	if !reAnnounceTrigger.MatchString(norm) {
		return "", false
	}

	// Walk through the text sentence-by-sentence to keep date context tight.
	for _, sentence := range splitOnPunct(text) {
		sn := strings.ToLower(sentence)
		if !reAnnounceTrigger.MatchString(sn) {
			continue
		}
		if d := findDateInSentence(sentence); d != "" {
			return d, true
		}
	}
	return "", false
}

// ExtractPeriod extracts the fiscal period from a press release body.
// Returns ("", 0) when period is not found.
func ExtractPeriod(text string) (quarter string, year int) {
	norm := strings.ToLower(text)
	switch {
	case reFY.MatchString(norm):
		quarter = "FY"
	case reQ4.MatchString(norm):
		quarter = "Q4"
	case reQ3.MatchString(norm):
		quarter = "Q3"
	case reQ2.MatchString(norm):
		quarter = "Q2"
	case reQ1.MatchString(norm):
		quarter = "Q1"
	}
	if m := reYear.FindString(norm); m != "" {
		year, _ = strconv.Atoi(m)
	}
	return quarter, year
}

// ExtractBeforeMarket returns true if the text says "before the market opens" /
// "before market open" / "BMO", false for "after market close" / "AMC".
// Returns nil when unknown.
func ExtractBeforeMarket(text string) *bool {
	norm := strings.ToLower(text)
	bmo := reBMO.MatchString(norm)
	amc := reAMC.MatchString(norm)
	switch {
	case bmo && !amc:
		v := true
		return &v
	case amc && !bmo:
		v := false
		return &v
	default:
		return nil
	}
}

// ---- regexps ----------------------------------------------------------------

var (
	reAnnounceTrigger = regexp.MustCompile(
		`(?i)\b(to\s+report|will\s+report|to\s+release|will\s+release|to\s+announce|will\s+announce|earnings\s+release|earnings\s+call|conference\s+call|scheduled\s+(?:for|to)|report\s+(?:its|financial|quarterly|fiscal))\b`)

	reBMO = regexp.MustCompile(`(?i)\b(before\s+(?:the\s+)?market\s+open|before\s+(?:the\s+)?open|bmo|pre-?market)\b`)
	reAMC = regexp.MustCompile(`(?i)\b(after\s+(?:the\s+)?market\s+close|after\s+(?:the\s+)?close|amc|after-?market)\b`)

	reQ1  = regexp.MustCompile(`(?i)\b(?:first\s+quarter|q1|1q)\b`)
	reQ2  = regexp.MustCompile(`(?i)\b(?:second\s+quarter|q2|2q)\b`)
	reQ3  = regexp.MustCompile(`(?i)\b(?:third\s+quarter|q3|3q)\b`)
	reQ4  = regexp.MustCompile(`(?i)\b(?:fourth\s+quarter|q4|4q)\b`)
	reFY  = regexp.MustCompile(`(?i)\b(?:full.?year|fiscal\s+year|annual|fy)\b`)
	reYear = regexp.MustCompile(`\b(20[2-9][0-9])\b`)

	// Date patterns — ordered most-specific to least-specific.
	reDateISO        = regexp.MustCompile(`\b(20[2-9][0-9]-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01]))\b`)
	reDateLong       = regexp.MustCompile(`(?i)\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(20[2-9][0-9])\b`)
	reDateLongNoYear = regexp.MustCompile(`(?i)\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	reDateAbbr       = regexp.MustCompile(`(?i)\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(20[2-9][0-9])\b`)
	reDateSlash      = regexp.MustCompile(`\b(0?[1-9]|1[0-2])/(0?[1-9]|[12]\d|3[01])/(20[2-9][0-9])\b`)
)

var monthNames = map[string]int{
	"january": 1, "february": 2, "march": 3, "april": 4,
	"may": 5, "june": 6, "july": 7, "august": 8,
	"september": 9, "october": 10, "november": 11, "december": 12,
	"jan": 1, "feb": 2, "mar": 3, "apr": 4,
	"jun": 6, "jul": 7, "aug": 8,
	"sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// findDateInSentence tries each date pattern and returns YYYY-MM-DD on match.
func findDateInSentence(s string) string {
	// ISO date: highest precision, no ambiguity.
	if m := reDateISO.FindString(s); m != "" {
		return m
	}

	// "Month DD, YYYY"
	if m := reDateLong.FindStringSubmatch(s); m != nil {
		return formatDate(monthNames[strings.ToLower(m[1])], atoi(m[2]), atoi(m[3]))
	}

	// "Mon. DD, YYYY"
	if m := reDateAbbr.FindStringSubmatch(s); m != nil {
		return formatDate(monthNames[strings.ToLower(strings.TrimRight(m[1], "."))], atoi(m[2]), atoi(m[3]))
	}

	// MM/DD/YYYY
	if m := reDateSlash.FindStringSubmatch(s); m != nil {
		return formatDate(atoi(m[1]), atoi(m[2]), atoi(m[3]))
	}

	// "Month DD" — use current year if month is still in the future, else next year.
	if m := reDateLongNoYear.FindStringSubmatch(s); m != nil {
		mon := monthNames[strings.ToLower(m[1])]
		day := atoi(m[2])
		now := time.Now().UTC()
		year := now.Year()
		d := time.Date(year, time.Month(mon), day, 0, 0, 0, 0, time.UTC)
		if d.Before(now.AddDate(0, 0, -7)) {
			d = d.AddDate(1, 0, 0) // rolled past; assume next year
		}
		return d.Format("2006-01-02")
	}

	return ""
}

func formatDate(month, day, year int) string {
	if month < 1 || month > 12 || day < 1 || day > 31 || year < 2020 || year > 2035 {
		return ""
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// splitOnPunct splits on sentence-ending punctuation followed by whitespace,
// matching the guidance extractor approach to avoid splitting decimal numbers.
var reSentSplit = regexp.MustCompile(`[.!?]\s+`)

func splitOnPunct(text string) []string {
	parts := reSentSplit.Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) > 15 {
			out = append(out, p)
		}
	}
	return out
}
