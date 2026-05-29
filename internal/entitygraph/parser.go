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
)

// ParseItem507 extracts vote data from the cleaned text of an 8-K filing.
// It returns an error only when Item 5.07 is entirely absent; partial
// results (e.g. no director rows) are returned with a nil error so callers
// can distinguish "no vote section" from "vote section parsed but empty".
func ParseItem507(text string) (Item507Result, error) {
	idx := re507Section.FindStringIndex(text)
	if idx == nil {
		return Item507Result{}, fmt.Errorf("Item 5.07 not found in filing text")
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
	proposalSplitter := regexp.MustCompile(`(?i)Proposal\s+(?:[2-9]|1\d)\b`)
	splits := proposalSplitter.FindAllStringIndex(body, -1)
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

	// Split body on proposal markers (Proposal 2, Proposal 3, ..., Proposal 19).
	// Use a word-boundary anchor so "Proposal 4:" and "Proposal 4." also split correctly.
	proposalSplitter := regexp.MustCompile(`(?i)Proposal\s+(?:[2-9]|1\d)\b`)
	splits := proposalSplitter.FindAllStringIndex(body, -1)
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
var headerPhrases = map[string]bool{
	"broker non-votes": true,
	"broker non votes": true,
	"non-votes":        true,
	"for against":      true,
	"against abstain":  true,
}

// looksLikePersonName returns true when s has at least two Title-cased words
// and does not look like a heading (e.g. "For Against Abstain", "Broker Non-Votes").
func looksLikePersonName(s string) bool {
	if headerPhrases[strings.ToLower(s)] {
		return false
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
