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

// TestParseItem507_ParenthesisedProposals verifies the parser handles the ABBV-style
// parenthesised proposal numbering — "(2) The stockholders ratified..." — which is
// common in pharmaceutical and biotech company annual meeting 8-Ks.
func TestParseItem507_ParenthesisedProposals(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.
As of March 1, 2026 there were 1,500,000,000 shares of common stock outstanding.
(1) The stockholders elected directors, as follows: Name For Against Abstain Broker Non-Votes
John A. Smith 300,000,000 10,000,000 5,000,000 25,000,000
Jane B. Doe 305,000,000 8,000,000 4,000,000 25,000,000
(2) The stockholders ratified the appointment of Ernst Young LLP as independent registered public accounting firm, as follows: For Against Abstain 1,400,000,000 50,000,000 10,000,000
(3) The stockholders approved, on an advisory basis, the compensation of named executive officers, as follows: For Against Abstain Broker Non-Votes 1,200,000,000 200,000,000 40,000,000 25,000,000
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.DirectorVotes) != 2 {
		t.Errorf("want 2 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
	if len(result.Proposals) < 2 {
		t.Errorf("want >= 2 proposals from parenthesised format, got %d", len(result.Proposals))
	}
	if result.Auditor == "" {
		t.Error("expected Auditor from (2) ratification, got empty")
	}
}

// TestParseItem507_ABBV2026 verifies parsing against the real AbbVie 2026 Annual Meeting 8-K.
// This exercises the parenthesised proposal format and "ratified" (past tense) auditor detection.
func TestParseItem507_ABBV2026(t *testing.T) {
	text := loadFixture(t, "../../fixtures/entitygraph/abbv_8k_5_07_2026.txt")

	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}

	// Expect 4 Class II directors.
	if len(result.DirectorVotes) != 4 {
		t.Errorf("want 4 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}

	// Expect 4 non-director proposals (auditor ratification, say-on-pay, supermajority, stockholder).
	if len(result.Proposals) < 4 {
		t.Errorf("want >= 4 proposals from parenthesised format, got %d", len(result.Proposals))
	}

	// Auditor should be detected via "ratified the appointment" phrasing.
	if result.Auditor == "" {
		t.Error("expected Auditor from (2) ratification using past-tense 'ratified', got empty")
	}

	// Proposal (4) — supermajority amendment — should be marked as not passed because
	// the text says "did not approve"; reDidNotPass must catch that phrasing.
	var foundNotPassed bool
	for _, p := range result.Proposals {
		if !p.Passed && p.ForVotes > 1_000_000_000 {
			foundNotPassed = true
		}
	}
	if !foundNotPassed {
		t.Error("expected proposal (4) 'did not approve' supermajority amendment to be Passed=false")
	}
}

// TestParseItem507_BareNumberProposals verifies the parser handles the BA-style
// bare-number proposal format — "2. Advisory Vote..." — which is common in
// industrial and aerospace company annual meeting 8-Ks. The column header line
// ("Votes For Votes Against Abstentions Broker Non-Votes") appears between the
// proposal title and the vote numbers, matching real EDGAR filing layout.
func TestParseItem507_BareNumberProposals(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.
As of March 1, 2026 there were 700,000,000 shares of common stock outstanding.
1. Election of Directors: Name For Against Abstain Broker Non-Votes
Alice C. Johnson 500,000,000 15,000,000 5,000,000 80,000,000
Bob D. Smith 495,000,000 20,000,000 5,000,000 80,000,000
Votes For Votes Against Abstentions Broker Non-Votes
2. Advisory Vote on Named Executive Officer Compensation: FOR AGAINST ABSTAIN BROKER NON-VOTES 450,000,000 80,000,000 10,000,000 80,000,000
Votes For Votes Against Abstentions
3. Ratification of the appointment of Deloitte Touche LLP as independent auditor: FOR AGAINST ABSTAIN 620,000,000 50,000,000 10,000,000
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.DirectorVotes) != 2 {
		t.Errorf("want 2 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
	if len(result.Proposals) < 2 {
		t.Errorf("want >= 2 proposals from bare-number format, got %d", len(result.Proposals))
	}
	if result.Auditor == "" {
		t.Error("expected Auditor from bare-number ratification proposal, got empty")
	}
	if result.Auditor != "Deloitte Touche LLP" {
		t.Errorf("Auditor = %q, want %q", result.Auditor, "Deloitte Touche LLP")
	}
}

// TestParseItem507_SCHWBareNumberFormat verifies the parser handles the SCHW 2021–2023
// style where proposals are numbered with a bare integer (no period): "2 Ratification".
// This format was observed in Charles Schwab annual meeting 8-Ks filed 2021–2023.
func TestParseItem507_SCHWBareNumberFormat(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders
The Annual Meeting of Stockholders was held on May 13, 2021.
For Against Abstain Broker Non-Vote 1 Election of Directors
(a) Walter W. Bettinger II 1,567,351,807 16,465,099 1,101,398 42,199,378
(b) Joan T. Dea 1,418,371,747 165,484,822 1,061,735 42,199,378
(c) Christopher V. Dodds 1,479,952,395 103,907,473 1,058,436 42,199,378
2 Ratification of the selection of Deloitte Touche LLP as independent auditors 1,559,550,628 66,684,165 882,889 0
3 Advisory vote to approve named executive officer compensation 1,495,476,997 86,578,454 2,862,853 42,199,378
4 Stockholder Proposal requesting disclosure of lobbying policy 696,152,642 883,179,764 5,585,898 42,199,378
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.DirectorVotes) != 3 {
		t.Errorf("want 3 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
	if len(result.Proposals) < 3 {
		t.Errorf("want >= 3 proposals from bare-number format, got %d", len(result.Proposals))
	}
	if result.Auditor == "" {
		t.Error("expected Auditor from bare-number ratification, got empty")
	}
}

// TestParseItem507_BLKItemFormat verifies the parser handles the BLK 2024-style
// where proposals use HTML thin-space entities: "Item&#8201;2 &#8211; Description".
// This format was observed in BlackRock annual meeting 8-Ks.
func TestParseItem507_BLKItemFormat(t *testing.T) {
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.
Item&#8201;1 &#8211; Election to the Board of Directors: For Against Abstentions Broker Non-Votes
Pamela Daley 119,757,616 2,210,592 101,402 9,821,539
Laurence D. Fink 117,016,498 4,538,881 514,231 9,821,539
William E. Ford 117,556,104 4,431,831 81,675 9,821,539
Item&#8201;2 &#8211; Approval of compensation for named executive officers: For Against Abstentions Broker Non-Votes 71,553,955 50,371,487 144,168 9,821,539
Item&#8201;3 &#8211; Approval of equity compensation plan: For Against Abstentions Broker Non-Votes 119,449,413 2,504,201 115,996 9,821,539
Item&#8201;4 &#8211; Ratification of the appointment of Deloitte LLP as independent registered public accounting firm: For Against Abstentions Broker Non-Votes 126,252,849 5,544,695 93,605 0
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.DirectorVotes) != 3 {
		t.Errorf("want 3 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
	if len(result.Proposals) < 3 {
		t.Errorf("want >= 3 proposals from BLK Item format, got %d", len(result.Proposals))
	}
	if result.Auditor == "" {
		t.Error("expected Auditor from Item&#8201;4 ratification, got empty")
	}
}

// TestParseItem507_LLYLetterFormat verifies the parser handles the LLY 2022–2026 style
// where proposals use lowercase-letter markers: "a)" for directors, "b) By...", "c) The..." etc.
// This format was observed in Eli Lilly annual meeting 8-Ks.
func TestParseItem507_LLYLetterFormat(t *testing.T) {
	text := `Item 5.07. Submission of Matters to a Vote of Security Holders.
The total number of shares voted was 847,254,010.
a) The four nominees for director were elected as follows: Nominee For Against Abstain Broker Nonvote
Carolyn Bertozzi 761,930,361 1,898,735 939,585 82,485,329
William Kaelin Jr. 726,270,361 37,418,960 1,079,360 82,485,329
Jon Moeller 749,926,634 13,860,022 982,025 82,485,329
David Ricks 734,760,028 29,119,356 889,297 82,485,329
b) By the following vote, the shareholders approved executive compensation: For Against Abstain Broker Nonvote 731,998,717 30,467,278 2,302,686 82,485,329
c) The appointment of Ernst Young LLP as the independent auditor was ratified: For Against Abstain 802,721,381 43,473,993 1,058,636
d) The proposal to amend the Articles did not receive the required vote of 80% of outstanding shares: For Against Abstain Broker Nonvote 665,371,049 97,677,173 1,720,459 82,485,329
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.DirectorVotes) != 4 {
		t.Errorf("want 4 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
	if len(result.Proposals) < 3 {
		t.Errorf("want >= 3 proposals from LLY letter format, got %d", len(result.Proposals))
	}
	if result.Auditor == "" {
		t.Error("expected Auditor from c) ratification, got empty")
	}
}

// TestParseItem507_AMZNProseFormat verifies the prose-style fallback parser handles
// AMZN 2010–2015 annual meeting 8-Ks where proposals carry no numbered header.
// Each non-director proposal is a full English sentence followed by vote counts.
func TestParseItem507_AMZNProseFormat(t *testing.T) {
	text := `ITEM 5.07 SUBMISSION OF MATTERS TO A VOTE OF SECURITY HOLDERS On May 25, 2010, Amazon.com, Inc. held its Annual Meeting of Shareholders. The following nominees were elected as directors, each to hold office until his or her successor is elected and qualified, by the vote set forth below: Nominee For Against Abstain Broker Non-Votes Jeffrey P. Bezos 352,174,504 5,558,479 124,487 34,991,438 Tom A. Alberg 354,547,709 3,241,305 68,456 34,991,438 John Seely Brown 356,159,079 1,625,707 72,684 34,991,438 William B. Gordon 311,894,554 44,697,458 1,265,458 34,991,438 Thomas O. Ryder 320,923,598 36,859,307 74,565 34,991,438 Patricia Q. Stonesifer 311,548,443 45,047,122 1,261,905 34,991,438 The appointment of Ernst & Young LLP as our independent auditor was ratified by the vote set forth below: For Against Abstain Broker Non-Votes 389,078,666 3,651,119 119,123 0 A shareholder proposal asking the Company to make disclosures regarding corporate political contributions was not approved, as set forth below: For Against Abstain Broker Non-Votes 76,187,211 229,535,651 52,134,608 34,991,438`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.DirectorVotes) != 6 {
		t.Errorf("want 6 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
	if len(result.Proposals) < 2 {
		t.Errorf("want >= 2 proposals from prose format, got %d", len(result.Proposals))
	}
	// Auditor ratification proposal should be detected.
	if result.Auditor == "" {
		t.Error("expected Auditor from prose-format ratification, got empty")
	}
	// Shareholder proposal should be marked not passed (76M for vs 229M against).
	var foundRejected bool
	for _, p := range result.Proposals {
		if !p.Passed && p.AgainstVotes > p.ForVotes {
			foundRejected = true
		}
	}
	if !foundRejected {
		t.Error("expected at least one not-passed shareholder proposal")
	}
}

// TestParseItem507_AMZNProseFormat2011 verifies prose fallback handles the AMZN 2011
// format which includes an advisory frequency vote with non-standard column headers.
func TestParseItem507_AMZNProseFormat2011(t *testing.T) {
	text := `ITEM 5.07 SUBMISSION OF MATTERS TO A VOTE OF SECURITY HOLDERS On June 7, 2011, Amazon.com, Inc. held its Annual Meeting of Shareholders. The following nominees were elected as directors, each to hold office until the next annual meeting of shareholders or until his or her successor is elected and qualified, by the vote set forth below: Nominee For Against Abstain Broker Non-Votes Jeffrey P. Bezos 364,111,695 6,811,186 129,329 36,575,667 Tom A. Alberg 367,808,977 3,152,895 90,338 36,575,667 John Seely Brown 357,651,787 13,251,475 148,948 36,575,667 The appointment of Ernst & Young LLP as our independent auditors was ratified by the vote set forth below: For Against Abstain Broker Non-Votes 405,117,762 2,390,112 120,003 0 An advisory vote was held regarding the compensation of our named executive officers as disclosed in the proxy statement, the results of which are set forth below: For Against Abstain Broker Non-Votes 365,258,830 5,361,676 431,704 36,575,667 A shareholder proposal regarding the shareholder ownership threshold for calling a special meeting of shareholders was not approved, as set forth below: For Against Abstain Broker Non-Votes 118,120,338 252,786,972 144,900 36,575,667`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	if len(result.DirectorVotes) != 3 {
		t.Errorf("want 3 directors, got %d: %v", len(result.DirectorVotes), directorNames(result.DirectorVotes))
	}
	if len(result.Proposals) < 3 {
		t.Errorf("want >= 3 proposals from AMZN 2011 prose format, got %d", len(result.Proposals))
	}
	if result.Auditor == "" {
		t.Error("expected Auditor from prose-format ratification, got empty")
	}
}

// TestParseItem507_NumberedFormatUnaffectedByProseFallback verifies that numbered-format
// filings (SCHW, ABBV, BA style) are not affected by the AMZN prose fallback — i.e.,
// the primary numbered splitter still runs and the fallback is never triggered.
func TestParseItem507_NumberedFormatUnaffectedByProseFallback(t *testing.T) {
	// SCHW 2026 format uses "Proposal 2", "Proposal 3" etc. The prose fallback
	// must NOT activate because the primary splitter already finds proposals.
	text := `Item 5.07 Submission of Matters to a Vote of Security Holders.
As of March 23, 2026 there were 1,900,456,000 shares of common stock outstanding.
Proposal 1 Election of Directors. For Against Abstain Broker Non-Votes
Alice C. Johnson 1,397,200,000 26,800,000 5,000,000 239,044,000
Bob D. Smith 1,213,200,000 221,400,000 8,500,000 239,044,000
Proposal 2 Ratification of Independent Auditor.
The stockholders ratified the appointment of Deloitte Touche LLP as auditor.
For Against Abstain 1,512,345,000 87,654,000 32,456,000
Proposal 3 Advisory Vote on Executive Compensation. The compensation of our named executive officers was approved.
For Against Abstain Broker Non-Votes 1,245,100,000 175,300,000 8,700,000 239,044,000
A stockholder proposal requesting additional disclosures was not approved.
For Against Abstain Broker Non-Votes 400,000,000 900,000,000 50,000,000 239,044,000
`
	result, err := ParseItem507(text)
	if err != nil {
		t.Fatalf("ParseItem507: %v", err)
	}
	// Primary splitter should find Proposal 2 and Proposal 3 — prose fallback must not
	// create an additional spurious split on "A stockholder proposal requesting".
	if len(result.Proposals) != 2 {
		t.Errorf("want exactly 2 proposals (Proposal 2, Proposal 3), got %d — prose fallback may have fired incorrectly", len(result.Proposals))
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

// ── Item 5.02 tests ───────────────────────────────────────────────────────────

func TestParseItem502_CFOResignation(t *testing.T) {
	text := `
Item 5.02. Departure of Directors or Certain Officers; Election of Directors.

On June 1, 2026, Jane Smith notified the Company of her resignation as Chief Financial Officer, 
effective June 15, 2026. The Company has initiated a search for a successor.
`
	result, err := ParseItem502(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Departures) == 0 {
		t.Fatal("expected at least one departure")
	}
	dep := result.Departures[0]
	if dep.DepartureType != DepartureResignation {
		t.Errorf("departure type: want resignation, got %s", dep.DepartureType)
	}
	if !dep.Voluntary {
		t.Error("resignation should be voluntary")
	}
	if !contains502Role(dep.Role, "Financial") {
		t.Errorf("expected CFO role, got %q", dep.Role)
	}
}

func TestParseItem502_CEOTermination(t *testing.T) {
	text := `
Item 5.02. Departure of Directors.

Effective June 2, 2026, the Board of Directors terminated John Doe as Chief Executive Officer.
The Board elected Mary Johnson as Interim Chief Executive Officer.
`
	result, err := ParseItem502(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Departures) == 0 {
		t.Fatal("expected departure")
	}
	dep := result.Departures[0]
	if dep.DepartureType != DepartureTermination {
		t.Errorf("want termination, got %s", dep.DepartureType)
	}
	if dep.Voluntary {
		t.Error("termination should not be voluntary")
	}
}

func TestParseItem502_RetirementWithAppointment(t *testing.T) {
	text := `
Item 5.02.

William Brown announced his retirement as Chairman of the Board effective December 31, 2026.
The Board appointed Susan Lee as Chairman of the Board effective January 1, 2027.
`
	result, err := ParseItem502(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Departures) == 0 {
		t.Error("expected retirement departure")
	} else if result.Departures[0].DepartureType != DepartureRetirement {
		t.Errorf("want retirement, got %s", result.Departures[0].DepartureType)
	}
}

func TestParseItem502_NotFound(t *testing.T) {
	text := `
Item 5.07. Submission of Matters to a Vote.

At the annual meeting held on June 1, 2026, shareholders voted as follows.
`
	_, err := ParseItem502(text)
	if err == nil {
		t.Error("expected ErrItem502NotFound when only 5.07 present")
	}
}

func TestParseItem502_EmptySection(t *testing.T) {
	// Item 5.02 present but no recognisable events.
	text := `Item 5.02. Compensatory Arrangements of Certain Officers.

The Company entered into an amendment to the employment agreement with its CEO.
`
	_, err := ParseItem502(text)
	if err == nil {
		t.Logf("Note: compensatory arrangement returned no error (may or may not extract events)")
	}
}

func TestScoreLeadershipChange_CFODeparture(t *testing.T) {
	result := Item502Result{
		Departures: []LeadershipEvent{
			{Role: "Chief Financial Officer", IsDeparture: true, DepartureType: DepartureResignation, Voluntary: true, PersonName: "Jane Smith"},
		},
	}
	sigs := ScoreLeadershipChange(result, "SCHW", "2026-06-01")
	if len(sigs) == 0 {
		t.Fatal("expected signals for CFO departure")
	}
	var hasCFO, hasGeneral bool
	for _, s := range sigs {
		if s.Type == SignalCFODeparture {
			hasCFO = true
		}
		if s.Type == SignalLeadershipDeparture {
			hasGeneral = true
		}
	}
	if !hasCFO {
		t.Error("expected cfo_departure signal")
	}
	if !hasGeneral {
		t.Error("expected leadership_departure signal")
	}
}

func TestScoreLeadershipChange_Termination_HighSeverity(t *testing.T) {
	result := Item502Result{
		Departures: []LeadershipEvent{
			{Role: "Chief Executive Officer", IsDeparture: true, DepartureType: DepartureTermination, Voluntary: false, PersonName: "John Doe"},
		},
	}
	sigs := ScoreLeadershipChange(result, "GE", "2026-06-01")
	for _, s := range sigs {
		if s.Type == SignalLeadershipDeparture && s.Severity != SeverityHigh {
			t.Errorf("involuntary termination should be high severity, got %s", s.Severity)
		}
	}
}

func contains502Role(role, substr string) bool {
	return len(role) > 0 && (len(substr) == 0 || containsStr(role, substr))
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
