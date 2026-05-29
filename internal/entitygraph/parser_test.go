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
