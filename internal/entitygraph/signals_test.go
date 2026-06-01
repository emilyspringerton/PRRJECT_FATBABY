package entitygraph

import (
	"testing"
	"time"
)

func TestScoreDirectorVotes_Friction(t *testing.T) {
	r := DefaultRules()
	votes := []VoteResult{
		{Name: "Frank C. Herringer", ForVotes: 1_213_200_000, AgainstVotes: 221_400_000, AbstainVotes: 8_500_000, ApprovalPct: 0.843},
	}
	sigs := ScoreDirectorVotes(votes, "SCHW", "", r)
	if len(sigs) == 0 {
		t.Fatal("expected signals, got none")
	}
	frictionCount := 0
	for _, s := range sigs {
		if s.Type == SignalDirectorFriction {
			frictionCount++
			if s.Severity != SeverityMedium {
				t.Errorf("Herringer friction severity = %s, want medium", s.Severity)
			}
		}
	}
	if frictionCount == 0 {
		t.Error("expected director_friction signal for Herringer at 84.3%, got none")
	}
}

func TestScoreDirectorVotes_HighTrust(t *testing.T) {
	r := DefaultRules()
	votes := []VoteResult{
		{Name: "Marianne C. Brown", ForVotes: 1_397_200_000, AgainstVotes: 26_800_000, AbstainVotes: 5_000_000, ApprovalPct: 0.979},
	}
	sigs := ScoreDirectorVotes(votes, "SCHW", "", r)
	found := false
	for _, s := range sigs {
		if s.Type == SignalHighTrustDirector {
			found = true
		}
	}
	if !found {
		t.Error("expected high_trust_director signal for Brown at 97.9%, got none")
	}
}

func TestScoreDirectorVotes_FamilyControl(t *testing.T) {
	r := DefaultRules()
	votes := []VoteResult{
		{Name: "Carolyn Schwab-Pomerantz", ApprovalPct: 0.963, ForVotes: 1_380_000_000, AgainstVotes: 38_000_000, AbstainVotes: 11_000_000},
	}
	sigs := ScoreDirectorVotes(votes, "SCHW", "", r)
	found := false
	for _, s := range sigs {
		if s.Type == SignalFamilyControl {
			found = true
		}
	}
	if !found {
		t.Error("expected family_control signal for Schwab-Pomerantz, got none")
	}
}

func TestScoreDirectorDecay(t *testing.T) {
	r := DefaultRules()

	// 3-year decline: 89.1% → 86.5% → 84.3%
	history := []float64{0.891, 0.865, 0.843}
	sig := ScoreDirectorDecay("Frank C. Herringer", "SCHW", history, r)
	if sig == nil {
		t.Fatal("expected decay signal for consistently declining director, got nil")
	}
	if sig.Type != SignalDirectorDecay {
		t.Errorf("signal type = %s, want director_decay", sig.Type)
	}

	// Insufficient data (1 point).
	sig2 := ScoreDirectorDecay("Someone Else", "SCHW", []float64{0.95}, r)
	if sig2 != nil {
		t.Error("expected nil signal for single data point, got non-nil")
	}

	// Flat trend — no decay.
	sig3 := ScoreDirectorDecay("Stable Director", "SCHW", []float64{0.95, 0.95, 0.96}, r)
	if sig3 != nil {
		t.Error("expected nil for flat trend, got non-nil")
	}
}

func TestScoreProposals_Entrenchment(t *testing.T) {
	r := DefaultRules()
	outstanding := int64(1_900_456_000)
	proposals := []ProposalResult{
		{
			Description:      "Declassify the Board of Directors",
			ForVotes:         1_319_800_000,
			AgainstVotes:     116_300_000,
			AbstainVotes:     9_100_000,
			TotalOutstanding: outstanding,
			RequiredPct:      0.80,
			Passed:           false,
		},
	}
	sigs := ScoreProposals(proposals, "SCHW", "", r)
	found := false
	for _, s := range sigs {
		if s.Type == SignalGovernanceEntrenchment {
			found = true
			if s.Severity != SeverityHigh {
				t.Errorf("entrenchment severity = %s, want high", s.Severity)
			}
		}
	}
	if !found {
		t.Error("expected governance_entrenchment signal for failed supermajority vote")
	}
}

func TestScoreProposals_CompConcern(t *testing.T) {
	r := DefaultRules()
	proposals := []ProposalResult{
		{
			Description:  "Advisory Vote on Executive Compensation",
			ForVotes:     1_000_000_000,
			AgainstVotes: 450_000_000,
			AbstainVotes: 50_000_000,
			Passed:       true,
		},
	}
	sigs := ScoreProposals(proposals, "TEST", "", r)
	found := false
	for _, s := range sigs {
		if s.Type == SignalCompensationConcern {
			found = true
		}
	}
	if !found {
		t.Error("expected compensation_concern signal for high opposition vote")
	}
}

func TestScoreCompositeActivistRisk_Fires(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	signals := []Signal{
		{Type: SignalGovernanceEntrenchment, Ticker: "SCHW", DetectedAt: today, Score: 0.913},
		{Type: SignalDirectorFriction, Ticker: "SCHW", DetectedAt: today, Score: 0.843, Entity: "Frank C. Herringer"},
	}
	sig := ScoreCompositeActivistRisk("SCHW", signals, 365)
	if sig == nil {
		t.Fatal("expected activist_risk signal when both entrenchment and friction present, got nil")
	}
	if sig.Type != SignalActivistRisk {
		t.Errorf("type = %s, want activist_risk", sig.Type)
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("severity = %s, want high", sig.Severity)
	}
	// Score should be 1 - worst_friction_approval (1 - 0.843 = 0.157)
	if sig.Score < 0.15 || sig.Score > 0.16 {
		t.Errorf("score = %.4f, want ~0.157 (1 - Herringer approval)", sig.Score)
	}
}

func TestScoreCompositeActivistRisk_NoFire_OnlyEntrenchment(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	signals := []Signal{
		{Type: SignalGovernanceEntrenchment, Ticker: "SCHW", DetectedAt: today, Score: 0.913},
	}
	if sig := ScoreCompositeActivistRisk("SCHW", signals, 365); sig != nil {
		t.Error("expected nil when only entrenchment (no friction), got signal")
	}
}

func TestScoreCompositeActivistRisk_NoFire_StaleSignals(t *testing.T) {
	old := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")
	signals := []Signal{
		{Type: SignalGovernanceEntrenchment, Ticker: "SCHW", DetectedAt: old, Score: 0.91},
		{Type: SignalDirectorFriction, Ticker: "SCHW", DetectedAt: old, Score: 0.84, Entity: "Someone"},
	}
	// 30-day window; signals are 2 years old — should not fire.
	if sig := ScoreCompositeActivistRisk("SCHW", signals, 30); sig != nil {
		t.Error("expected nil for signals outside the lookback window, got signal")
	}
}

func TestScoreDirectorLinks_NoLinksWithSingleTicker(t *testing.T) {
	g := NewGraph()
	// Herringer appears only at SCHW — no other tickers to link.
	g.UpsertPerson("Frank C. Herringer", NodeDirector, FilingAppearance{Ticker: "SCHW", FilingDate: "2026-05-21", ApprovalPct: 0.843})
	frictionSigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "SCHW", Entity: "Frank C. Herringer", Score: 0.843},
	}
	links := ScoreDirectorLinks(g, frictionSigs)
	if len(links) != 0 {
		t.Errorf("expected 0 director_link signals with single-ticker director, got %d", len(links))
	}
}

func TestScoreDirectorLinks_FiresForSharedDirector(t *testing.T) {
	g := NewGraph()
	// Herringer sits on two boards.
	g.UpsertPerson("Frank C. Herringer", NodeDirector, FilingAppearance{Ticker: "SCHW", FilingDate: "2026-05-21", ApprovalPct: 0.843})
	g.UpsertPerson("Frank C. Herringer", NodeDirector, FilingAppearance{Ticker: "IBKR", FilingDate: "2026-04-10", ApprovalPct: 0.900})
	frictionSigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "SCHW", Entity: "Frank C. Herringer", Score: 0.843},
	}
	links := ScoreDirectorLinks(g, frictionSigs)
	if len(links) != 1 {
		t.Fatalf("expected 1 director_link signal for IBKR, got %d", len(links))
	}
	if links[0].Ticker != "IBKR" {
		t.Errorf("link ticker = %s, want IBKR", links[0].Ticker)
	}
	if links[0].Type != SignalDirectorLink {
		t.Errorf("link type = %s, want director_link", links[0].Type)
	}
}

func TestScoreAuditorChange(t *testing.T) {
	sig := ScoreAuditorChange("SCHW", "Deloitte Touche LLP", "KPMG LLP", "")
	if sig.Type != SignalAuditorChange {
		t.Errorf("signal type = %s, want auditor_change", sig.Type)
	}
	if sig.Severity != SeverityMedium {
		t.Errorf("severity = %s, want medium", sig.Severity)
	}
	if sig.Metadata["prev_auditor"] != "Deloitte Touche LLP" {
		t.Errorf("prev_auditor metadata = %q, want %q", sig.Metadata["prev_auditor"], "Deloitte Touche LLP")
	}
	if sig.Metadata["new_auditor"] != "KPMG LLP" {
		t.Errorf("new_auditor metadata = %q, want %q", sig.Metadata["new_auditor"], "KPMG LLP")
	}
}

func TestGraphTrackAuditor_FirstRecord(t *testing.T) {
	g := NewGraph()
	changed, prev := g.TrackAuditor("SCHW", "Deloitte Touche LLP", "2026-05-21")
	if changed {
		t.Error("first auditor record should not trigger change, got changed=true")
	}
	if prev != "" {
		t.Errorf("prev should be empty for first record, got %q", prev)
	}
	if g.Auditors["SCHW"] == nil || g.Auditors["SCHW"].Auditor != "Deloitte Touche LLP" {
		t.Error("auditor not stored correctly")
	}
}

func TestGraphTrackAuditor_Change(t *testing.T) {
	g := NewGraph()
	g.TrackAuditor("SCHW", "Deloitte Touche LLP", "2025-05-15")
	changed, prev := g.TrackAuditor("SCHW", "KPMG LLP", "2026-05-21")
	if !changed {
		t.Error("expected changed=true when auditor switches firms")
	}
	if prev != "Deloitte Touche LLP" {
		t.Errorf("prev = %q, want %q", prev, "Deloitte Touche LLP")
	}
}

func TestGraphTrackAuditor_NoChange(t *testing.T) {
	g := NewGraph()
	g.TrackAuditor("SCHW", "Deloitte Touche LLP", "2025-05-15")
	changed, _ := g.TrackAuditor("SCHW", "Deloitte Touche LLP", "2026-05-21")
	if changed {
		t.Error("expected changed=false when same auditor repeated")
	}
}

func TestScoreDirectorVotes_NominationRejection(t *testing.T) {
	r := DefaultRules()
	votes := []VoteResult{
		{Name: "Failed Director", ForVotes: 450_000_000, AgainstVotes: 600_000_000, AbstainVotes: 10_000_000, ApprovalPct: 0.425},
	}
	sigs := ScoreDirectorVotes(votes, "TEST", "", r)
	found := false
	for _, s := range sigs {
		if s.Type == SignalNominationRejection {
			found = true
			if s.Severity != SeverityCritical {
				t.Errorf("nomination_rejection severity = %s, want critical", s.Severity)
			}
		}
		// Rejection and friction must not both fire for the same director.
		if s.Type == SignalDirectorFriction {
			t.Error("unexpected director_friction alongside nomination_rejection (should be mutually exclusive)")
		}
	}
	if !found {
		t.Error("expected nomination_rejection signal for 42.5% approval, got none")
	}
}

func TestScoreProposals_AbstentionSpike(t *testing.T) {
	r := DefaultRules()
	proposals := []ProposalResult{
		{
			Description:  "Ratification of Auditor",
			ForVotes:     1_400_000_000,
			AgainstVotes: 50_000_000,
			AbstainVotes: 250_000_000, // ~14.7% abstention — above 10% threshold
			Passed:       true,
		},
	}
	sigs := ScoreProposals(proposals, "TEST", "", r)
	found := false
	for _, s := range sigs {
		if s.Type == SignalAbstentionSpike {
			found = true
			if s.Score < 0.14 || s.Score > 0.16 {
				t.Errorf("abstention_spike score = %.3f, want ~0.147", s.Score)
			}
		}
	}
	if !found {
		t.Error("expected abstention_spike signal for 14.7% abstention rate, got none")
	}
}

func TestScoreProposals_NoAbstentionSpike_BelowThreshold(t *testing.T) {
	r := DefaultRules()
	proposals := []ProposalResult{
		{
			Description:  "Advisory Vote on Executive Compensation",
			ForVotes:     1_500_000_000,
			AgainstVotes: 100_000_000,
			AbstainVotes: 50_000_000, // ~3% abstention — below 10% threshold
			Passed:       true,
		},
	}
	sigs := ScoreProposals(proposals, "TEST", "", r)
	for _, s := range sigs {
		if s.Type == SignalAbstentionSpike {
			t.Error("unexpected abstention_spike signal for 3% abstention rate")
		}
	}
}

func TestCanonicalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Frank C. Herringer", "frank-c-herringer"},
		{"Carolyn Schwab-Pomerantz", "carolyn-schwab-pomerantz"},
		{"Walter W. Bettinger II", "walter-w-bettinger-ii"},
		{"Marianne C. Brown", "marianne-c-brown"},
	}
	for _, tc := range cases {
		got := Canonicalize(tc.in)
		if got != tc.want {
			t.Errorf("Canonicalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestScoreGovernanceHealth_HealthyBoard(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	// Five high-trust directors, no adverse signals.
	var sigs []Signal
	for i := 0; i < 5; i++ {
		sigs = append(sigs, Signal{Type: SignalHighTrustDirector, Ticker: "GS", DetectedAt: today, Score: 0.97})
	}
	sig := ScoreGovernanceHealth("GS", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal, got nil")
	}
	if sig.Type != SignalGovernanceHealth {
		t.Errorf("type = %s, want governance_health_index", sig.Type)
	}
	if sig.Score < 0.80 {
		t.Errorf("healthy board score = %.2f, want >= 0.80", sig.Score)
	}
	if sig.Severity != SeverityLow {
		t.Errorf("severity = %s, want low for healthy board", sig.Severity)
	}
}

func TestScoreGovernanceHealth_SCHWLikeBoard(t *testing.T) {
	// SCHW 2026 pattern: friction + entrenchment + activist_risk + family + BNV + 4x high-trust.
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "SCHW", DetectedAt: today, Score: 0.843},
		{Type: SignalGovernanceEntrenchment, Ticker: "SCHW", DetectedAt: today, Score: 0.913},
		{Type: SignalActivistRisk, Ticker: "SCHW", DetectedAt: today, Score: 0.157},
		{Type: SignalFamilyControl, Ticker: "SCHW", DetectedAt: today, Score: 0.963},
		{Type: SignalBrokerNonVoteAnomaly, Ticker: "SCHW", DetectedAt: today, Score: 0.142},
		{Type: SignalHighTrustDirector, Ticker: "SCHW", DetectedAt: today, Score: 0.979},
		{Type: SignalHighTrustDirector, Ticker: "SCHW", DetectedAt: today, Score: 0.972},
		{Type: SignalHighTrustDirector, Ticker: "SCHW", DetectedAt: today, Score: 0.968},
		{Type: SignalHighTrustDirector, Ticker: "SCHW", DetectedAt: today, Score: 0.963},
	}
	sig := ScoreGovernanceHealth("SCHW", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal, got nil")
	}
	// Penalties: friction(-0.20) + entrenchment(-0.30) + activist_risk(-0.25) +
	//            family(-0.10) + BNV(-0.05) = -0.90
	// Bonuses: 4x high_trust = +0.20 (capped)
	// Expected score: 1.0 - 0.90 + 0.20 = 0.30
	if sig.Score > 0.45 {
		t.Errorf("SCHW-like board score = %.2f, want <= 0.45 given multiple adverse signals", sig.Score)
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("severity = %s, want high for SCHW-like adverse signal combination", sig.Severity)
	}
}

func TestScoreGovernanceHealth_NoSignals_ReturnsNil(t *testing.T) {
	if sig := ScoreGovernanceHealth("EMPTY", []Signal{}, 365); sig != nil {
		t.Error("expected nil for ticker with no signals, got non-nil")
	}
}

func TestScoreGovernanceHealth_IgnoresOtherTickers(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "OTHER", DetectedAt: today, Score: 0.75},
		{Type: SignalHighTrustDirector, Ticker: "MS", DetectedAt: today, Score: 0.96},
	}
	sig := ScoreGovernanceHealth("MS", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal for MS, got nil")
	}
	// Only the MS high_trust signal should count; OTHER's friction should be ignored.
	if sig.Score < 0.80 {
		t.Errorf("MS score = %.2f, want >= 0.80 (OTHER's friction should be excluded)", sig.Score)
	}
}

func TestScoreGovernanceHealth_NomRejection_Critical(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalNominationRejection, Ticker: "TEST", DetectedAt: today, Score: 0.38},
		{Type: SignalGovernanceEntrenchment, Ticker: "TEST", DetectedAt: today, Score: 0.91},
	}
	sig := ScoreGovernanceHealth("TEST", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal, got nil")
	}
	// nomination_rejection(-0.40) + entrenchment(-0.30) = -0.70 → score 0.30
	if sig.Score > 0.40 {
		t.Errorf("critical signals score = %.2f, want <= 0.40", sig.Score)
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("severity = %s, want high", sig.Severity)
	}
}

func TestScoreGovernanceHealth_CriticalSeverity(t *testing.T) {
	// score < 0.20 → critical severity
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalNominationRejection, Ticker: "CRIT", DetectedAt: today, Score: 0.30},    // -0.40
		{Type: SignalGovernanceEntrenchment, Ticker: "CRIT", DetectedAt: today, Score: 0.92}, // -0.30
		{Type: SignalActivistRisk, Ticker: "CRIT", DetectedAt: today, Score: 0.70},           // -0.25
		{Type: SignalAuditorChange, Ticker: "CRIT", DetectedAt: today, Score: 1.0},           // -0.20
	}
	// Total penalty: 0.40+0.30+0.25+0.20 = 1.15 → score = 0.0 (floored)
	sig := ScoreGovernanceHealth("CRIT", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal, got nil")
	}
	if sig.Score > 0.20 {
		t.Errorf("multi-critical score = %.2f, want <= 0.20", sig.Score)
	}
	if sig.Severity != SeverityCritical {
		t.Errorf("severity = %s, want critical for score < 0.20", sig.Severity)
	}
}

func TestScoreDirectorDecay_AccelerationPath(t *testing.T) {
	r := DefaultRules()
	// Only 1 data point of history (n < DecayMinYears=2) but a 5pp drop.
	// Should still fire via acceleration path.
	sig := ScoreDirectorDecay("Frank C. Herringer", "SCHW", []float64{0.891, 0.843}, r)
	if sig == nil {
		t.Fatal("expected decay signal for 4.8pp single-year drop, got nil")
	}
	if sig.Type != SignalDirectorDecay {
		t.Errorf("type = %s, want director_decay", sig.Type)
	}
	// 4.8pp > 4pp acceleration threshold → should fire even with 2 data points
	if sig.Severity != SeverityMedium {
		t.Errorf("acceleration path severity = %s, want medium", sig.Severity)
	}
}

func TestScoreDirectorDecay_LargeDropHighSeverity(t *testing.T) {
	r := DefaultRules()
	// 3 data points, 6pp avg drop → high severity
	sig := ScoreDirectorDecay("Test Director", "TEST", []float64{0.95, 0.89, 0.83}, r)
	if sig == nil {
		t.Fatal("expected decay signal for 6pp avg drop, got nil")
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("severity = %s, want high for >5pp avg drop", sig.Severity)
	}
}

func TestScoreCompositeActivistRisk_CriticalOnRejection(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	// nomination_rejection + governance_entrenchment → critical
	sigs := []Signal{
		{Type: SignalNominationRejection, Ticker: "TEST", DetectedAt: today, Score: 0.38},
		{Type: SignalGovernanceEntrenchment, Ticker: "TEST", DetectedAt: today, Score: 0.92},
	}
	sig := ScoreCompositeActivistRisk("TEST", sigs, 365)
	if sig == nil {
		t.Fatal("expected activist_risk signal, got nil")
	}
	if sig.Severity != SeverityCritical {
		t.Errorf("severity = %s, want critical when nomination_rejection + entrenchment co-occur", sig.Severity)
	}
}

func TestScoreCompositeActivistRisk_HighOnFriction(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	// director_friction + governance_entrenchment → high (not critical)
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "SCHW", DetectedAt: today, Score: 0.843},
		{Type: SignalGovernanceEntrenchment, Ticker: "SCHW", DetectedAt: today, Score: 0.913},
	}
	sig := ScoreCompositeActivistRisk("SCHW", sigs, 365)
	if sig == nil {
		t.Fatal("expected activist_risk signal, got nil")
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("severity = %s, want high for friction+entrenchment (not rejection)", sig.Severity)
	}
}

func TestScoreCompositeActivistRisk_FilingDateWindow(t *testing.T) {
	// Signal has old DetectedAt but recent FilingDate — should still be in window.
	recentFiling := time.Now().UTC().AddDate(0, -2, 0).Format("2006-01-02")
	oldDetect := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "FD", DetectedAt: oldDetect, FilingDate: recentFiling, Score: 0.80},
		{Type: SignalGovernanceEntrenchment, Ticker: "FD", DetectedAt: oldDetect, FilingDate: recentFiling, Score: 0.90},
	}
	sig := ScoreCompositeActivistRisk("FD", sigs, 365)
	if sig == nil {
		t.Fatal("expected activist_risk signal when filing date is recent, got nil")
	}
}

// TestScoreGovernanceHealth_FilingDateWindow verifies that ScoreGovernanceHealth uses
// FilingDate (not DetectedAt) when determining whether a signal is in the window.
// This prevents backfilled historical signals from contaminating the current health score.
func TestScoreGovernanceHealth_FilingDateWindow(t *testing.T) {
	// Old filing (5 years ago) detected recently due to backfill — should be EXCLUDED.
	oldFiling := time.Now().UTC().AddDate(-5, 0, 0).Format("2006-01-02")
	recentDetect := time.Now().UTC().Format("2006-01-02")
	// Recent filing and detection — should be INCLUDED.
	recentFiling := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")

	sigs := []Signal{
		// Old filing — heavy penalty but outside 365-day FilingDate window; should be excluded.
		{Type: SignalNominationRejection, Ticker: "WH", DetectedAt: recentDetect, FilingDate: oldFiling, Score: 0.30},
		// Recent filing — minor penalty; should be included.
		{Type: SignalBrokerNonVoteAnomaly, Ticker: "WH", DetectedAt: recentDetect, FilingDate: recentFiling, Score: 0.20},
	}
	sig := ScoreGovernanceHealth("WH", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal, got nil")
	}
	// Only the BNV anomaly penalty (-0.05) should apply; score should be near 0.95.
	// If the old nomination_rejection (-0.40) were included, score would drop to ~0.55.
	if sig.Score < 0.80 {
		t.Errorf("score = %.2f: old-filing nomination_rejection should be excluded by FilingDate window; only BNV penalty (-0.05) should apply", sig.Score)
	}
}

// TestScoreGovernanceHealth_FilingDateFallback verifies that when FilingDate is empty,
// DetectedAt is used as the window anchor (backward-compatible behavior).
func TestScoreGovernanceHealth_FilingDateFallback(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	oldDetect := time.Now().UTC().AddDate(-2, 0, 0).Format("2006-01-02")
	sigs := []Signal{
		// No FilingDate — DetectedAt is old; should be excluded from 365-day window.
		{Type: SignalNominationRejection, Ticker: "FB", DetectedAt: oldDetect, Score: 0.30},
		// No FilingDate — DetectedAt is recent; should be included.
		{Type: SignalHighTrustDirector, Ticker: "FB", DetectedAt: today, Score: 0.96},
	}
	sig := ScoreGovernanceHealth("FB", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal, got nil")
	}
	// Only the high_trust bonus (+0.05) applies; old signal excluded via DetectedAt fallback.
	if sig.Score < 0.80 {
		t.Errorf("score = %.2f: old DetectedAt signal should be excluded; only high_trust bonus applies", sig.Score)
	}
}
