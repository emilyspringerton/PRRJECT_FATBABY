package entitygraph

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Item507Result holds parsed vote data from an 8-K Item 5.07 section.
type Item507Result struct {
	DirectorVotes []VoteResult
	Proposals     []ProposalResult
	// Auditor is the name of the public accounting firm identified in the
	// auditor ratification proposal (e.g. "Deloitte Touche LLP"), or empty
	// if no ratification proposal was found.
	Auditor string
}

// VoteResult holds the vote counts for a single director nominee.
type VoteResult struct {
	Name           string
	ForVotes       int64
	AgainstVotes   int64
	AbstainVotes   int64
	BrokerNonVotes int64
	ApprovalPct    float64
}

// ProposalResult holds aggregate vote counts for a non-director proposal.
type ProposalResult struct {
	Description string
	ForVotes    int64
	AgainstVotes int64
	AbstainVotes int64
	// TotalOutstanding is populated when a supermajority threshold applies.
	TotalOutstanding int64
	// RequiredPct is the supermajority threshold (e.g. 0.80), or 0 for simple majority.
	RequiredPct float64
	Passed      bool
}

var (
	// re507Section matches the start of an Item 5.07 section.
	re507Section = regexp.MustCompile(`(?i)Item\s+5\.07`)

	// reOutstandingShares captures the total outstanding share count from the preamble.
	// Handles variants: "N shares of common stock outstanding", "N shares of Common Stock outstanding",
	// "N common shares outstanding".
	reOutstandingShares = regexp.MustCompile(`(?i)([\d,]+)\s+(?:shares\s+of\s+(?:common\s+)?stock|common\s+shares)\s+outstanding`)

	// reSupermajority captures a supermajority threshold percentage.
	// Handles variants: "80% of the outstanding shares", "80% of outstanding shares",
	// "80% of all outstanding shares".
	reSupermajority = regexp.MustCompile(`(?i)(\d{2,3})%\s+of\s+(?:all\s+)?(?:the\s+)?outstanding\s+shares`)

	// reCommas strips comma separators from numbers.
	reCommas = regexp.MustCompile(`,`)

	// reDirectorRow matches: <name> <for> <against> <abstain> <broker-nv>
	// Name: first name, optional dotted middle initials (C.), last name that
	// may be compound-hyphenated (Schwab-Pomerantz), optional suffix.
	// Dotted middle initials are required to have a dot so the regex does not
	// greedily consume the first capital of a compound surname.
	// A mandatory \s+ separates the (optional) middle initials from the last name.
	reDirectorRow = regexp.MustCompile(
		`([A-Z][a-z']+(?:\s+[A-Z]\.)*\s+(?:[A-Z][a-z']+(?:-[A-Z][a-z']+)*)(?:\s+(?:Jr\.|Sr\.|II|III|IV))?)\s+([\d,]{5,})\s+([\d,]+)\s+([\d,]+)\s+([\d,]+)`,
	)

	// reForAgainstAbstain matches proposal-level aggregate votes (3 numbers after optional labels).
	reProposalVotes = regexp.MustCompile(
		`(?i)(?:For[:\s]*)?([\d,]{5,})\s+(?:Against[:\s]*)?([\d,]+)\s+(?:Abstain[:\s]*)?([\d,]+)`,
	)

	// reDeclineKeyword detects "did not pass" or "failed" near a supermajority statement.
	reDidNotPass = regexp.MustCompile(`(?i)did\s+not\s+pass|failed\s+to\s+pass|not\s+approved`)

	// reAuditorName extracts the public accounting firm name from a ratification proposal.
	// Matches "appointment of <Firm LLP> as" — "ratification of" refers to the proposal
	// title, not the firm name, so only "appointment of" anchors the firm name capture.
	reAuditorName = regexp.MustCompile(`(?i)appointment\s+of\s+([\w\s&,\.]+?(?:LLP|LLC|PC|PLLC|L\.L\.P\.|P\.C\.))\s+as\s+`)

	// reRatificationProposal detects that a proposal chunk is an auditor ratification.
	reRatificationProposal = regexp.MustCompile(`(?i)(?:ratif(?:y|ication|ying)|independent\s+registered\s+public\s+accounting)`)

	// reProposalSplitter matches the start of a non-director (proposal 2+) block.
	// Handles three common EDGAR Item 5.07 formats:
	//   "Proposal 2"  — explicit word prefix (e.g. SCHW proxy)
	//   "(2)"         — parenthesised number (e.g. ABBV annual meeting 8-K)
	//   "2."          — bare number + period + space + uppercase (e.g. BA annual meeting 8-K)
	// Proposal 1 (director election) is intentionally excluded in all three forms.
	reProposalSplitter = regexp.MustCompile(
		`(?i)(?:Proposal\s+(?:[2-9]|1\d)\b|\((?:[2-9]|1\d)\)\s|\b(?:[2-9]|1\d)\.\s+[A-Z])`,
	)
)

// ErrItem507NotFound is returned by ParseItem507 when the filing contains no
// Item 5.07 section. This is normal for most 8-K subtypes (earnings releases,
// officer appointments, M&A disclosures, etc.). Callers should treat it as a
// graceful skip rather than a parse failure.
var ErrItem507NotFound = fmt.Errorf("Item 5.07 not found in filing text")

// ParseItem507 extracts vote data from the cleaned text of an 8-K filing.
// It returns ErrItem507NotFound when Item 5.07 is entirely absent; partial
// results (e.g. no director rows) are returned with a nil error so callers
// can distinguish "no vote section" from "vote section parsed but empty".
func ParseItem507(text string) (Item507Result, error) {
	idx := re507Section.FindStringIndex(text)
	if idx == nil {
		return Item507Result{}, ErrItem507NotFound
	}
	body := text[idx[0]:]

	outstanding := extractOutstanding(text)
	result := Item507Result{}
	result.DirectorVotes = extractDirectorVotes(body)
	result.Proposals = extractProposals(body, outstanding)
	result.Auditor = extractAuditor(body)
	return result, nil
}

// extractAuditor finds the public accounting firm name from an auditor ratification
// proposal in the Item 5.07 body. Returns an empty string if not found.
func extractAuditor(body string) string {
	// Only search within ratification proposal chunks.
	splits := reProposalSplitter.FindAllStringIndex(body, -1)
	for i, loc := range splits {
		start := loc[0]
		end := len(body)
		if i+1 < len(splits) {
			end = splits[i+1][0]
		}
		chunk := body[start:end]
		if !reRatificationProposal.MatchString(chunk) {
			continue
		}
		m := reAuditorName.FindStringSubmatch(chunk)
		if m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// extractOutstanding parses the total outstanding shares from the preamble.
func extractOutstanding(text string) int64 {
	m := reOutstandingShares.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	return parseInt64(m[1])
}

// extractDirectorVotes finds rows of the form <name> <for> <against> <abstain> <bnv>.
// A row qualifies when the first field looks like a human name (Title case, multiple words).
func extractDirectorVotes(body string) []VoteResult {
	var results []VoteResult
	seen := map[string]bool{}

	matches := reDirectorRow.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		if len(m) < 6 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if !looksLikePersonName(name) {
			continue
		}
		key := Canonicalize(name)
		if seen[key] {
			continue
		}
		seen[key] = true

		forV := parseInt64(m[2])
		againstV := parseInt64(m[3])
		abstainV := parseInt64(m[4])
		bnv := parseInt64(m[5])
		total := forV + againstV + abstainV
		var approvalPct float64
		if total > 0 {
			approvalPct = float64(forV) / float64(total)
		}
		results = append(results, VoteResult{
			Name:           name,
			ForVotes:       forV,
			AgainstVotes:   againstV,
			AbstainVotes:   abstainV,
			BrokerNonVotes: bnv,
			ApprovalPct:    approvalPct,
		})
	}
	return results
}

// extractProposals finds non-director proposal vote blocks after director election text ends.
// It looks for 3-number aggregate vote sequences following proposal keywords.
func extractProposals(body string, outstanding int64) []ProposalResult {
	var results []ProposalResult

	// Split body on proposal markers (Proposal 2+, (2)+, or "2. Title" bare-number format).
	// reProposalSplitter handles all three common EDGAR Item 5.07 layouts.
	splits := reProposalSplitter.FindAllStringIndex(body, -1)
	for i, loc := range splits {
		start := loc[0]
		end := len(body)
		if i+1 < len(splits) {
			end = splits[i+1][0]
		}
		chunk := body[start:end]
		prop := parseProposalChunk(chunk, outstanding)
		if prop != nil {
			results = append(results, *prop)
		}
	}
	return results
}

func parseProposalChunk(chunk string, outstanding int64) *ProposalResult {
	// Extract description: everything before the first large number.
	descEnd := reProposalVotes.FindStringIndex(chunk)
	desc := chunk
	if descEnd != nil {
		desc = strings.TrimSpace(chunk[:descEnd[0]])
	}
	// Trim description to ~120 chars.
	if len(desc) > 120 {
		desc = desc[:120]
	}
	desc = strings.TrimSpace(desc)

	m := reProposalVotes.FindStringSubmatch(chunk)
	if m == nil {
		return nil
	}
	forV := parseInt64(m[1])
	againstV := parseInt64(m[2])
	abstainV := parseInt64(m[3])
	if forV == 0 {
		return nil
	}

	// Detect supermajority requirement.
	var reqPct float64
	sm := reSupermajority.FindStringSubmatch(chunk)
	if sm != nil {
		pct, err := strconv.ParseFloat(sm[1], 64)
		if err == nil {
			reqPct = pct / 100.0
		}
	}

	passed := true
	total := forV + againstV + abstainV
	if reqPct > 0 && outstanding > 0 {
		passed = float64(forV)/float64(outstanding) >= reqPct
	} else if total > 0 {
		passed = float64(forV)/float64(total) > 0.5
	}
	if reDidNotPass.MatchString(chunk) {
		passed = false
	}

	return &ProposalResult{
		Description:      desc,
		ForVotes:         forV,
		AgainstVotes:     againstV,
		AbstainVotes:     abstainV,
		TotalOutstanding: outstanding,
		RequiredPct:      reqPct,
		Passed:           passed,
	}
}

// headerPhrases is a set of vote-table header strings to reject.
// These are column headers and aggregate-row labels that appear in 8-K Item 5.07
// vote tables and can be mistaken for director names by the regex.
var headerPhrases = map[string]bool{
	"broker non-votes":     true,
	"broker non votes":     true,
	"broker non-vote":      true,
	"broker non vote":      true,
	"non-votes":            true,
	"non votes":            true,
	"for against":          true,
	"against abstain":      true,
	"against abstained":    true,
	"abstain broker":       true,
	"votes cast":           true,
	"total votes":          true,
	"withheld abstained":   true,
	"withheld abstain":     true,
}

// nonNameWords are keywords that never appear in a real director name but do
// appear in vote-table headers and aggregate rows. Any match disqualifies the
// candidate regardless of title-casing.
var nonNameWords = map[string]bool{
	"against":   true,
	"abstain":   true,
	"abstained": true,
	"withheld":  true,
	"non-vote":  true,
	"non-votes": true,
	"broker":    true,
	"cast":      true,
}

// isSpuriousName returns true when a name contains known vote-table non-name
// keywords (e.g. "Against", "Broker", "Abstained"). Used to filter previously
// persisted spurious nodes from the graph store without requiring a full re-parse.
// Less strict than looksLikePersonName — only rejects confirmed non-name words.
func isSpuriousName(s string) bool {
	if headerPhrases[strings.ToLower(s)] {
		return true
	}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.TrimRight(w, ".,;:")
		if nonNameWords[w] {
			return true
		}
	}
	return false
}

// looksLikePersonName returns true when s has at least two Title-cased words
// and does not look like a heading (e.g. "For Against Abstain", "Broker Non-Votes").
func looksLikePersonName(s string) bool {
	if headerPhrases[strings.ToLower(s)] {
		return false
	}
	// Reject any candidate containing a known non-name keyword.
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.TrimRight(w, ".,;:")
		if nonNameWords[w] {
			return false
		}
	}
	words := strings.Fields(s)
	if len(words) < 2 {
		return false
	}
	titleCount := 0
	for _, w := range words {
		w = strings.TrimRight(w, ".")
		if len(w) > 0 && w[0] >= 'A' && w[0] <= 'Z' {
			titleCount++
		}
	}
	// Reject all-caps words (headers like "FOR AGAINST") and single-word matches.
	if strings.ToUpper(s) == s {
		return false
	}
	// Require at least 2 out of the first 3 words to be Title-cased.
	check := words
	if len(check) > 3 {
		check = words[:3]
	}
	tc := 0
	for _, w := range check {
		w = strings.TrimRight(w, ".")
		if len(w) < 2 || w[0] < 'A' || w[0] > 'Z' {
			continue
		}
		// Split on hyphen so "Schwab-Pomerantz" → ["Schwab","Pomerantz"].
		parts := strings.Split(w, "-")
		allTitleCase := true
		for _, p := range parts {
			if len(p) == 0 {
				continue
			}
			if p[0] < 'A' || p[0] > 'Z' {
				allTitleCase = false
				break
			}
			rest := strings.ToLower(p[1:])
			if p[1:] != rest {
				allTitleCase = false
				break
			}
		}
		if allTitleCase {
			tc++
		}
	}
	return tc >= 2 && titleCount >= 2
}

func parseInt64(s string) int64 {
	s = reCommas.ReplaceAllString(strings.TrimSpace(s), "")
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// ── Item 5.02 — Leadership departure / appointment ────────────────────────────

// ErrItem502NotFound is returned when the filing contains no Item 5.02 section.
var ErrItem502NotFound = fmt.Errorf("Item 5.02 not found in filing text")

// DepartureType classifies how a departure was characterised.
type DepartureType string

const (
	DepartureResignation DepartureType = "resignation"
	DepartureRetirement  DepartureType = "retirement"
	DepartureTermination DepartureType = "termination"
	DepartureUnknown     DepartureType = "unknown"
)

// LeadershipEvent describes one departure or appointment extracted from Item 5.02.
type LeadershipEvent struct {
	PersonName    string
	Role          string        // e.g. "Chief Financial Officer", "Director"
	IsDeparture   bool          // true = departure, false = appointment
	DepartureType DepartureType // only meaningful when IsDeparture=true
	Voluntary     bool          // resignation/retirement = voluntary; termination = involuntary
}

// Item502Result holds all leadership events parsed from one Item 5.02 section.
type Item502Result struct {
	Departures   []LeadershipEvent
	Appointments []LeadershipEvent
}

var (
	re502Section = regexp.MustCompile(`(?i)Item\s+5\.02`)

	// Departure verb patterns (earliest-matching wins).
	reResign  = regexp.MustCompile(`(?i)\bresigned?\b|\bresignation\b|\bstepped?\s+down\b|\bwill\s+not\s+stand\s+for\s+re-?election\b`)
	reRetire  = regexp.MustCompile(`(?i)\bretired?\b|\bretirement\b`)
	reTerm    = regexp.MustCompile(`(?i)\bterminated?\b|\bdismissed?\b|\bremoved?\s+from\b`)
	reAppoint = regexp.MustCompile(`(?i)\bappointed?\s+(?:as\s+)?|elected?\s+(?:as\s+)?|named\s+(?:as\s+)?|promoted\s+(?:to\s+)?`)

	// Executive role patterns — ordered longest first to avoid partial matches.
	reRoles = regexp.MustCompile(`(?i)\b(Chief\s+Executive\s+Officer|Chief\s+Financial\s+Officer|Chief\s+Operating\s+Officer|Chief\s+Technology\s+Officer|Chief\s+Legal\s+Officer|Chief\s+Accounting\s+Officer|Chief\s+Marketing\s+Officer|Chief\s+Revenue\s+Officer|General\s+Counsel|Executive\s+Vice\s+President|Senior\s+Vice\s+President|Vice\s+President|Executive\s+Chairman|Chairman\s+of\s+the\s+Board|Chairman|President\s+and\s+Chief\s+Executive\s+Officer|President|Chief\s+of\s+Staff|Chief\s+People\s+Officer|Director|Principal\s+Accounting\s+Officer|Principal\s+Financial\s+Officer)\b`)

	// Person name heuristic: TitleCase word(s) possibly followed by suffix.
	rePersonName = regexp.MustCompile(`\b([A-Z][a-z]{1,}(?:\s+[A-Z][a-z]{1,}){1,3}(?:\s+(?:Jr\.|Sr\.|II|III|IV))?)\b`)
)

// ParseItem502 extracts leadership departure and appointment events from the
// cleaned text of an 8-K filing. Returns ErrItem502NotFound when Item 5.02
// is absent (normal for proxy and earnings filings).
func ParseItem502(text string) (Item502Result, error) {
	idx := re502Section.FindStringIndex(text)
	if idx == nil {
		return Item502Result{}, ErrItem502NotFound
	}
	// Extract the Item 5.02 section body (up to the next Item header or 4000 chars).
	sectionStart := idx[1]
	sectionEnd := len(text)
	if next := regexp.MustCompile(`(?i)Item\s+[0-9]+\.[0-9]+`).FindStringIndex(text[sectionStart:]); next != nil && next[0] < 4000 {
		sectionEnd = sectionStart + next[0]
	} else if sectionStart+4000 < len(text) {
		sectionEnd = sectionStart + 4000
	}
	body := text[sectionStart:sectionEnd]

	var result Item502Result

	// Split into sentences for per-sentence analysis.
	sentences := splitSentences(body)

	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if len(s) < 20 {
			continue
		}

		// Detect roles mentioned in this sentence.
		roles := reRoles.FindAllString(s, -1)
		if len(roles) == 0 {
			continue
		}
		role := normaliseRole(roles[0])

		// Departure?
		if reResign.MatchString(s) {
			ev := LeadershipEvent{
				Role:          role,
				IsDeparture:   true,
				DepartureType: DepartureResignation,
				Voluntary:     true,
				PersonName:    extractPersonName(s, role),
			}
			result.Departures = appendUniq(result.Departures, ev)
			continue
		}
		if reRetire.MatchString(s) {
			ev := LeadershipEvent{
				Role:          role,
				IsDeparture:   true,
				DepartureType: DepartureRetirement,
				Voluntary:     true,
				PersonName:    extractPersonName(s, role),
			}
			result.Departures = appendUniq(result.Departures, ev)
			continue
		}
		if reTerm.MatchString(s) {
			ev := LeadershipEvent{
				Role:          role,
				IsDeparture:   true,
				DepartureType: DepartureTermination,
				Voluntary:     false,
				PersonName:    extractPersonName(s, role),
			}
			result.Departures = appendUniq(result.Departures, ev)
			continue
		}

		// Appointment?
		if reAppoint.MatchString(s) {
			ev := LeadershipEvent{
				Role:          role,
				IsDeparture:   false,
				PersonName:    extractPersonName(s, role),
			}
			result.Appointments = appendUniq(result.Appointments, ev)
		}
	}

	if len(result.Departures) == 0 && len(result.Appointments) == 0 {
		return Item502Result{}, ErrItem502NotFound
	}
	return result, nil
}

// normaliseRole cleans role strings (e.g. collapses "President and CEO" → "President and CEO").
func normaliseRole(r string) string {
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(r, " "))
}

// extractPersonName tries to find a person name near the role in the sentence.
// It skips common non-name TitleCase patterns.
func extractPersonName(sentence, role string) string {
	// Remove the role itself so we don't match it as a name.
	stripped := strings.Replace(sentence, role, "", 1)
	candidates := rePersonName.FindAllString(stripped, -1)
	for _, c := range candidates {
		if !isSpuriousName(c) && len(strings.Fields(c)) >= 2 {
			return c
		}
	}
	return ""
}

// splitSentences splits text on sentence-ending punctuation.
func splitSentences(text string) []string {
	re := regexp.MustCompile(`[.!?]\s+`)
	parts := re.Split(text, -1)
	// Re-include trailing punctuation stripped by the split.
	var out []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// appendUniq appends ev only if no existing entry has the same Role+IsDeparture.
func appendUniq(evs []LeadershipEvent, ev LeadershipEvent) []LeadershipEvent {
	for _, e := range evs {
		if e.Role == ev.Role && e.IsDeparture == ev.IsDeparture {
			return evs
		}
	}
	return append(evs, ev)
}
