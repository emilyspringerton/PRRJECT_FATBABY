package entitygraph

import (
	"os"
	"testing"
)

func loadFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("load fixture %s: %v", path, err)
	}
	return string(data)
}

func TestParseItem507_SCHW(t *testing.T) {
	text := loadFixture(t, "../../fixtures/entitygraph/schw_8k_5_07_2026.txt")

	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}

	// Expect 5 director nominees.
	if len(result.DirectorVotes) != 5 {
		t.Errorf("want 5 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}

	// Verify Marianne C. Brown high-approval extraction.
	brown := findDirector(result.DirectorVotes, "Marianne")
	if brown == nil {
		t.Fatal("Marianne C. Brown not found in results")
	}
	if brown.ApprovalPct < 0.97 || brown.ApprovalPct > 0.99 {
		t.Errorf("Brown approval = %.3f, want ~0.979", brown.ApprovalPct)
	}

	// Verify Frank C. Herringer friction-level extraction.
	herringer := findDirector(result.DirectorVotes, "Herringer")
	if herringer == nil {
		t.Fatal("Frank C. Herringer not found in results")
	}
	if herringer.ApprovalPct < 0.83 || herringer.ApprovalPct > 0.86 {
		t.Errorf("Herringer approval = %.3f, want ~0.843", herringer.ApprovalPct)
	}

	// Verify Carolyn Schwab-Pomerantz family-name director.
	schwab := findDirector(result.DirectorVotes, "Schwab")
	if schwab == nil {
		t.Fatal("Carolyn Schwab-Pomerantz not found in results")
	}

	// Expect at least 3 proposals (auditor ratification, comp, declassification).
	if len(result.Proposals) < 3 {
		t.Errorf("want >= 3 proposals, got %d", len(result.Proposals))
	}

	// Governance entrenchment: declassification should have failed (Proposal 4).
	var entrenchProp *ProposalResult
	for i := range result.Proposals {
		p := &result.Proposals[i]
		if !p.Passed && p.RequiredPct >= 0.79 {
			entrenchProp = p
			break
		}
	}
	if entrenchProp == nil {
		t.Error("expected a failed supermajority proposal (board declassification), none found")
	} else {
		total := entrenchProp.ForVotes + entrenchProp.AgainstVotes + entrenchProp.AbstainVotes
		forPct := float64(entrenchProp.ForVotes) / float64(total)
		if forPct < 0.90 || forPct > 0.94 {
			t.Errorf("declassification for%% = %.3f, want ~0.92", forPct)
		}
	}
}

// TestParseItem507_SupermajorityWithoutThe verifies the parser handles "80% of outstanding shares"
// (no "the") — a common EDGAR filing variant that previously would fail to detect the threshold.
func TestParseItem507_AuditorExtraction(t *testing.T) {
	text := loadFixture(t, "../../fixtures/entitygraph/schw_8k_5_07_2026.txt")
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if result.Auditor == "" {
		t.Fatal("expected Auditor to be populated from Proposal 2 ratification, got empty")
	}
	if result.Auditor != "Deloitte Touche LLP" {
		t.Errorf("Auditor = %q, want %q", result.Auditor, "Deloitte Touche LLP")
	}
}

func TestParseItem507_AuditorExtraction_NoRatification(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.
As of March 1, 2026 there were 500,000,000 shares of common stock outstanding.
John A. Smith 300,000,000 10,000,000 5,000,000 25,000,000
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if result.Auditor != "" {
		t.Errorf("expected empty Auditor when no ratification proposal present, got %q", result.Auditor)
	}
}

func TestParseItem507_SupermajorityWithoutThe(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.
As of March 1, 2026 there were 500,000,000 common shares outstanding.
Proposal 2 Declassify the Board.
This proposal required 80% of outstanding shares to pass.
For Against Abstain Broker Non-Votes
412,000,000 55,000,000 8,000,000 25,000,000
The proposal did not pass because it did not receive the required 80% of outstanding shares.
John A. Smith 300,000,000 10,000,000 5,000,000 25,000,000
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.Proposals) == 0 {
		t.Fatal("expected at least 1 proposal, got 0")
	}
	prop := result.Proposals[0]
	if prop.RequiredPct == 0 {
		t.Error("expected supermajority RequiredPct to be detected (without 'the'), got 0")
	}
	if prop.Passed {
		t.Error("proposal should have Passed=false (did not receive required 80%)")
	}
}

// TestParseItem507_ProposalNumbering10Plus verifies proposals numbered 10+ are split correctly.
func TestParseItem507_ProposalNumbering10Plus(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.
As of March 1, 2026 there were 500,000,000 shares of common stock outstanding.
John A. Smith 300,000,000 10,000,000 5,000,000 25,000,000
Proposal 10 Ratification of Auditor.
For Against Abstain
450,000,000 30,000,000 20,000,000
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.Proposals) == 0 {
		t.Error("expected Proposal 10 to be parsed; got 0 proposals")
	}
}

// TestLooksLikePersonName_HeaderRejection ensures vote-table headers and aggregate
// row labels are never mistaken for director names. This guards against the
// "Against Abstained" and "Broker Non-Vote" false-positives observed in live data.
func TestLooksLikePersonName_HeaderRejection(t *testing.T) {
	rejects := []string{
		"Against Abstained",
		"Against Abstain",
		"Broker Non-Vote",
		"Broker Non-Votes",
		"Withheld Abstained",
		"Votes Cast",
		"Total Votes",
	}
	for _, s := range rejects {
		if looksLikePersonName(s) {
			t.Errorf("looksLikePersonName(%q) = true, want false", s)
		}
	}
}

func TestLooksLikePersonName_ValidNames(t *testing.T) {
	accepts := []string{
		"James Bell",
		"Tim Cook",
		"Andrea Jung",
		"Carolyn Schwab-Pomerantz",
		"Frank C. Herringer",
		"Marianne C. Brown",
		"Robert J. Smith Jr.",
	}
	for _, s := range accepts {
		if !looksLikePersonName(s) {
			t.Errorf("looksLikePersonName(%q) = false, want true", s)
		}
	}
}

func TestParseItem507_NoSpuriousHeaderNodes(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.

Director Election — Proposal 1

	Name              For           Against Abstained    Broker Non-Vote
	James Bell     800,000,000    5,000,000  2,000,000   30,000,000
	Tim Cook       820,000,000    3,000,000  1,500,000   30,000,000

Proposal 2 Ratification of independent auditors.
appointment of Ernst Young LLP as independent
800,000,000 10,000,000 5,000,000
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	for _, v := range result.DirectorVotes {
		if v.Name == "Against Abstained" || v.Name == "Broker Non-Vote" {
			t.Errorf("spurious header node extracted as director: %q", v.Name)
		}
	}
	// Should find exactly James Bell and Tim Cook.
	if len(result.DirectorVotes) != 2 {
		t.Errorf("want 2 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
}

func TestParseItem507_NoSection(t *testing.T) {
	_, err := ParseItem507("This is a regular 8-K with no vote section.")
	if err == nil {
		t.Error("expected error for filing without Item 5.07, got nil")
	}
}

func TestParseItem507_Empty(t *testing.T) {
	_, err := ParseItem507("")
	if err == nil {
		t.Error("expected error for empty text, got nil")
	}
}

func findDirector(votes []VoteResult, nameFragment string) *VoteResult {
	for i := range votes {
		if contains(votes[i].Name, nameFragment) {
			return &votes[i]
		}
	}
	return nil
}

func directorNames(votes []VoteResult) []string {
	names := make([]string, len(votes))
	for i, v := range votes {
		names[i] = v.Name
	}
	return names
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
