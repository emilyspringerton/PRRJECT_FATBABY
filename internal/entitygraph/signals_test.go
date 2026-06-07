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

// TestScoreDirectorVotes_BNVOncePerFiling verifies that broker_nonvote_anomaly fires
// exactly once per filing regardless of how many directors are in the election.
// Previously the signal was emitted per-director, causing N identical signals per filing.
func TestScoreDirectorVotes_BNVOncePerFiling(t *testing.T) {
	r := DefaultRules()
	// 5 directors, all with the same large BNV count (typical for Apple-style elections).
	bnv := int64(1_402_346_727)
	votes := []VoteResult{
		{Name: "James Bell", ForVotes: 2_664_386_155, AgainstVotes: 40_229_756, AbstainVotes: 5_182_628, BrokerNonVotes: bnv, ApprovalPct: 0.983},
		{Name: "Tim Cook", ForVotes: 2_681_116_075, AgainstVotes: 25_363_575, AbstainVotes: 3_318_889, BrokerNonVotes: bnv, ApprovalPct: 0.989},
		{Name: "Al Gore", ForVotes: 2_599_605_229, AgainstVotes: 105_339_035, AbstainVotes: 4_854_275, BrokerNonVotes: bnv, ApprovalPct: 0.959},
		{Name: "Andrea Jung", ForVotes: 2_588_984_846, AgainstVotes: 116_206_940, AbstainVotes: 4_606_753, BrokerNonVotes: bnv, ApprovalPct: 0.955},
		{Name: "Ron Sugar", ForVotes: 2_668_301_042, AgainstVotes: 34_730_214, AbstainVotes: 6_767_283, BrokerNonVotes: bnv, ApprovalPct: 0.985},
	}
	sigs := ScoreDirectorVotes(votes, "AAPL", "2019-03-04", r)
	bnvCount := 0
	for _, s := range sigs {
		if s.Type == SignalBrokerNonVoteAnomaly {
			bnvCount++
		}
	}
	if bnvCount != 1 {
		t.Errorf("expected exactly 1 bnv_anomaly signal per filing, got %d", bnvCount)
	}
	// Verify it has no entity (filing-level, not director-level).
	for _, s := range sigs {
		if s.Type == SignalBrokerNonVoteAnomaly && s.Entity != "" {
			t.Errorf("bnv_anomaly should have no entity (filing-level), got entity=%q", s.Entity)
		}
	}
}

// TestScoreAbstentionOutliers_FiresOnOutlier verifies that a director with an abstain
// rate well above the filing median triggers an abstention_outlier signal.
func TestScoreAbstentionOutliers_FiresOnOutlier(t *testing.T) {
	r := DefaultRules()
	votes := []VoteResult{
		{Name: "Alice Normal", ForVotes: 10_000_000, AgainstVotes: 100_000, AbstainVotes: 20_000, ApprovalPct: 0.99},   // ~0.2%
		{Name: "Bob Normal", ForVotes: 10_000_000, AgainstVotes: 120_000, AbstainVotes: 18_000, ApprovalPct: 0.988},     // ~0.18%
		{Name: "Carol Outlier", ForVotes: 9_500_000, AgainstVotes: 100_000, AbstainVotes: 500_000, ApprovalPct: 0.99},   // ~4.9% — >2.5x peer median
		{Name: "David Normal", ForVotes: 10_000_000, AgainstVotes: 110_000, AbstainVotes: 22_000, ApprovalPct: 0.989},   // ~0.22%
		{Name: "Eve Normal", ForVotes: 10_000_000, AgainstVotes: 90_000, AbstainVotes: 19_000, ApprovalPct: 0.991},      // ~0.19%
	}
	sigs := ScoreAbstentionOutliers(votes, "TEST", "2026-01-01", r)
	if len(sigs) == 0 {
		t.Fatal("expected abstention_outlier signal for Carol Outlier, got none")
	}
	found := false
	for _, s := range sigs {
		if s.Entity == "Carol Outlier" {
			found = true
			if s.Type != SignalAbstentionOutlier {
				t.Errorf("signal type = %s, want abstention_outlier", s.Type)
			}
		}
	}
	if !found {
		t.Error("expected signal for Carol Outlier specifically, not found")
	}
}

// TestScoreAbstentionOutliers_NoFireBelowMultiplier verifies that directors with
// uniformly similar abstain rates do not trigger the outlier signal.
func TestScoreAbstentionOutliers_NoFireBelowMultiplier(t *testing.T) {
	r := DefaultRules()
	votes := []VoteResult{
		{Name: "Alice Normal", ForVotes: 10_000_000, AgainstVotes: 100_000, AbstainVotes: 200_000},  // 2.0%
		{Name: "Bob Normal", ForVotes: 10_000_000, AgainstVotes: 100_000, AbstainVotes: 210_000},    // 2.1%
		{Name: "Carol Normal", ForVotes: 10_000_000, AgainstVotes: 100_000, AbstainVotes: 220_000},  // 2.2%
		{Name: "David Normal", ForVotes: 10_000_000, AgainstVotes: 100_000, AbstainVotes: 195_000},  // 1.95%
		{Name: "Eve Normal", ForVotes: 10_000_000, AgainstVotes: 100_000, AbstainVotes: 205_000},    // 2.05%
	}
	sigs := ScoreAbstentionOutliers(votes, "TEST", "2026-01-01", r)
	if len(sigs) != 0 {
		t.Errorf("expected no abstention_outlier signals for uniform abstention rates, got %d", len(sigs))
	}
}

// TestScoreBoardDecayConcern_FiresAtThreshold verifies that the board_decay_concern
// composite fires when >= MinBoardDecayCount distinct directors have decay signals.
func TestScoreBoardDecayConcern_FiresAtThreshold(t *testing.T) {
	r := DefaultRules() // MinBoardDecayCount = 3
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Alice Director", DetectedAt: today},
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Bob Director", DetectedAt: today},
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Carol Director", DetectedAt: today},
	}
	sig := ScoreBoardDecayConcern("TEST", sigs, r)
	if sig == nil {
		t.Fatal("expected board_decay_concern when 3 directors decaying, got nil")
	}
	if sig.Type != SignalBoardDecayConcern {
		t.Errorf("type = %s, want board_decay_concern", sig.Type)
	}
	if sig.Severity != SeverityMedium {
		t.Errorf("severity = %s, want medium (count=3 == threshold)", sig.Severity)
	}
}

// TestScoreBoardDecayConcern_HighSeverityAtDoubleThreshold verifies severity escalation
// to high when >= 2x the minimum count of directors are decaying.
func TestScoreBoardDecayConcern_HighSeverityAtDoubleThreshold(t *testing.T) {
	r := DefaultRules() // MinBoardDecayCount = 3; 2x = 6
	today := time.Now().UTC().Format("2006-01-02")
	var sigs []Signal
	for _, name := range []string{"Alice", "Bob", "Carol", "Dave", "Eve", "Frank"} {
		sigs = append(sigs, Signal{Type: SignalDirectorDecay, Ticker: "TEST", Entity: name + " Director", DetectedAt: today})
	}
	sig := ScoreBoardDecayConcern("TEST", sigs, r)
	if sig == nil {
		t.Fatal("expected board_decay_concern for 6 decaying directors, got nil")
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("severity = %s, want high for 6 >= 2×3 threshold", sig.Severity)
	}
}

// TestScoreBoardDecayConcern_NoFireBelowThreshold verifies no signal when fewer than
// MinBoardDecayCount directors are decaying.
func TestScoreBoardDecayConcern_NoFireBelowThreshold(t *testing.T) {
	r := DefaultRules() // threshold = 3
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Alice Director", DetectedAt: today},
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Bob Director", DetectedAt: today},
	}
	if sig := ScoreBoardDecayConcern("TEST", sigs, r); sig != nil {
		t.Errorf("expected nil for 2 decaying directors (threshold=3), got %s", sig.Type)
	}
}

// TestScoreBoardDecayConcern_DedupsByDirectorName verifies that multiple decay signals
// for the same director are counted as one.
func TestScoreBoardDecayConcern_DedupsByDirectorName(t *testing.T) {
	r := DefaultRules()
	today := time.Now().UTC().Format("2006-01-02")
	// Alice appears 3 times (e.g. 3 filing batches) — should still count as 1 director.
	sigs := []Signal{
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Alice Director", DetectedAt: today},
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Alice Director", DetectedAt: today},
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Alice Director", DetectedAt: today},
	}
	if sig := ScoreBoardDecayConcern("TEST", sigs, r); sig != nil {
		t.Errorf("expected nil: 3 signals for same director counts as 1 distinct director, got signal")
	}
}

// TestScoreBoardDecayConcern_IgnoresStaleSignals verifies that decay signals outside
// the window are not counted.
func TestScoreBoardDecayConcern_IgnoresStaleSignals(t *testing.T) {
	r := DefaultRules()
	stale := time.Now().UTC().AddDate(-3, 0, 0).Format("2006-01-02") // 3 years ago, beyond 730-day window
	sigs := []Signal{
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Alice Director", DetectedAt: stale},
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Bob Director", DetectedAt: stale},
		{Type: SignalDirectorDecay, Ticker: "TEST", Entity: "Carol Director", DetectedAt: stale},
	}
	if sig := ScoreBoardDecayConcern("TEST", sigs, r); sig != nil {
		t.Errorf("expected nil for stale decay signals outside window, got signal")
	}
}

// TestIsCompVote_DefaultKeywords verifies the built-in compensation vote keyword matching.
func TestIsCompVote_DefaultKeywords(t *testing.T) {
	r := DefaultRules()
	cases := []struct {
		desc string
		want bool
	}{
		{"Advisory Vote on Executive Compensation", true},
		{"Say-on-Pay Advisory Vote", true},
		{"Ratification of Independent Auditor", false},
		{"Election of Directors", false},
		{"Advisory Vote on Remuneration", true},   // new keyword
		{"Advisory Vote on Pay", true},             // new keyword
	}
	for _, tc := range cases {
		got := isCompVote(tc.desc, r)
		if got != tc.want {
			t.Errorf("isCompVote(%q) = %v, want %v", tc.desc, got, tc.want)
		}
	}
}

// TestIsCompVote_CustomKeywords verifies that custom keywords from rules override defaults.
func TestIsCompVote_CustomKeywords(t *testing.T) {
	r := DefaultRules()
	r.CompVoteKeywords = []string{"remuneración"} // Spanish-language proxy, hypothetical
	if !isCompVote("Voto Consultivo sobre la Remuneración", r) {
		t.Error("expected isCompVote to match custom keyword 'remuneración'")
	}
	// Default keyword should NOT match when overridden.
	if isCompVote("Advisory Vote on Executive Compensation", r) {
		t.Error("expected default keyword 'compensation' to be inactive when overridden with custom keywords")
	}
}

// TestScoreGovernanceHealth_BoardDecayConcernPenalty verifies that board_decay_concern
// signals are counted in the governance health score.
func TestScoreGovernanceHealth_BoardDecayConcernPenalty(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalBoardDecayConcern, Ticker: "TEST", DetectedAt: today, Score: 1.0},
	}
	sig := ScoreGovernanceHealth("TEST", sigs, 365)
	if sig == nil {
		t.Fatal("expected governance_health signal, got nil")
	}
	// board_decay_concern penalty = 0.15; score should be 0.85.
	if sig.Score > 0.90 || sig.Score < 0.80 {
		t.Errorf("score = %.2f, want ~0.85 (1.0 - 0.15 board_decay_concern penalty)", sig.Score)
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

// ── ScoreGovernanceHealthTrend tests ─────────────────────────────────────────

func TestScoreGovernanceHealthTrend_Deteriorating(t *testing.T) {
	sig := ScoreGovernanceHealthTrend("SCHW", 0.35, 0.60, 0.10)
	if sig == nil {
		t.Fatal("expected deterioration signal, got nil")
	}
	if sig.Type != SignalGovernanceDeterioration {
		t.Errorf("type: want %s got %s", SignalGovernanceDeterioration, sig.Type)
	}
	if sig.Score < 0.24 || sig.Score > 0.26 {
		t.Errorf("score should be ~0.25 (delta magnitude), got %.3f", sig.Score)
	}
}

func TestScoreGovernanceHealthTrend_Improving(t *testing.T) {
	sig := ScoreGovernanceHealthTrend("AAPL", 0.80, 0.60, 0.10)
	if sig == nil {
		t.Fatal("expected improvement signal, got nil")
	}
	if sig.Type != SignalGovernanceImproving {
		t.Errorf("type: want %s got %s", SignalGovernanceImproving, sig.Type)
	}
}

func TestScoreGovernanceHealthTrend_BelowThreshold(t *testing.T) {
	// Delta of 0.05 < minDelta of 0.10 — should return nil.
	if sig := ScoreGovernanceHealthTrend("GE", 0.55, 0.50, 0.10); sig != nil {
		t.Errorf("expected nil for below-threshold delta, got %+v", sig)
	}
}

func TestScoreGovernanceHealthTrend_DefaultMinDelta(t *testing.T) {
	// 0 minDelta should use default 0.10.
	if sig := ScoreGovernanceHealthTrend("GE", 0.52, 0.50, 0); sig != nil {
		t.Errorf("expected nil for small delta with default threshold")
	}
	if sig := ScoreGovernanceHealthTrend("GE", 0.30, 0.55, 0); sig == nil {
		t.Error("expected signal for large delta with default threshold")
	}
}

func TestScoreGovernanceHealthTrend_ZeroScore(t *testing.T) {
	// Zero scores — no data, should return nil.
	if sig := ScoreGovernanceHealthTrend("GE", 0, 0.55, 0.10); sig != nil {
		t.Error("expected nil when current score is 0")
	}
}

func TestScoreGovernanceHealthTrend_HighSeverityDrop(t *testing.T) {
	// Drop of 0.25 (≥0.20) should be high severity.
	sig := ScoreGovernanceHealthTrend("SCHW", 0.20, 0.55, 0.10)
	if sig == nil {
		t.Fatal("expected signal")
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("want high severity for large drop, got %s", sig.Severity)
	}
}

// ── ScoreLongTenure ──────────────────────────────────────────────────────────

func TestScoreLongTenure_FiringAboveThreshold(t *testing.T) {
	g := NewGraph()
	// Simulate a director who first appeared in 2008 — >12 years ago.
	app := FilingAppearance{
		Ticker:      "SCHW",
		FilingDate:  "2008-05-01",
		ApprovalPct: 0.92,
	}
	g.UpsertPerson("Long-Tenured Director", NodeDirector, app)

	r := DefaultRules()
	sigs := ScoreLongTenure(g, r)
	if len(sigs) == 0 {
		t.Fatal("expected director_long_tenure signal, got none")
	}
	for _, s := range sigs {
		if s.Type != SignalDirectorLongTenure {
			t.Errorf("unexpected signal type %s", s.Type)
		}
		if s.Ticker != "SCHW" {
			t.Errorf("unexpected ticker %s", s.Ticker)
		}
	}
}

func TestScoreLongTenure_NoFireBelowThreshold(t *testing.T) {
	g := NewGraph()
	// Director first appeared recently — below the 12-year threshold.
	app := FilingAppearance{
		Ticker:      "AAPL",
		FilingDate:  "2022-06-01",
		ApprovalPct: 0.97,
	}
	g.UpsertPerson("New Director", NodeDirector, app)

	r := DefaultRules()
	sigs := ScoreLongTenure(g, r)
	for _, s := range sigs {
		if s.Ticker == "AAPL" && s.Entity == "New Director" {
			t.Error("expected no long_tenure signal for recently-added director")
		}
	}
}

func TestScoreLongTenure_HighSeverityAt15Years(t *testing.T) {
	g := NewGraph()
	app := FilingAppearance{
		Ticker:      "JPM",
		FilingDate:  "2008-01-01",
		ApprovalPct: 0.95,
	}
	g.UpsertPerson("Very Long Director", NodeDirector, app)

	r := DefaultRules()
	sigs := ScoreLongTenure(g, r)
	found := false
	for _, s := range sigs {
		if s.Ticker == "JPM" {
			found = true
			if s.Severity != SeverityHigh {
				t.Errorf("director with 17+ years should be high severity, got %s", s.Severity)
			}
		}
	}
	if !found {
		t.Error("expected signal for very long tenured director at JPM")
	}
}

func TestScoreLongTenure_EmptyGraph(t *testing.T) {
	g := NewGraph()
	sigs := ScoreLongTenure(g, DefaultRules())
	if len(sigs) != 0 {
		t.Errorf("expected 0 signals for empty graph, got %d", len(sigs))
	}
}

func TestScoreLongTenure_CustomThreshold(t *testing.T) {
	g := NewGraph()
	// Director from 2020 — 5+ years ago but under default 12.
	app := FilingAppearance{
		Ticker:      "MSFT",
		FilingDate:  "2018-01-01",
		ApprovalPct: 0.93,
	}
	g.UpsertPerson("Mid-Tenure Director", NodeDirector, app)

	r := DefaultRules()
	r.LongTenureYearsThreshold = 5 // lower threshold should now fire
	sigs := ScoreLongTenure(g, r)
	found := false
	for _, s := range sigs {
		if s.Ticker == "MSFT" {
			found = true
		}
	}
	if !found {
		t.Error("expected long_tenure signal with custom 5-year threshold")
	}
}

// ── ScorePeerGovernanceRank ──────────────────────────────────────────────────

func TestScorePeerGovernanceRank_FiringBelow(t *testing.T) {
	// SCHW at 0.40, others in financial sector at 0.80 — gap 0.40, above 0.15 threshold.
	scores := map[string]float64{
		"SCHW": 0.40,
		"BAC":  0.80,
		"JPM":  0.82,
		"WFC":  0.78,
	}
	sectors := map[string]string{
		"SCHW": "financial",
		"BAC":  "financial",
		"JPM":  "financial",
		"WFC":  "financial",
	}
	r := DefaultRules()
	sigs := ScorePeerGovernanceRank(scores, sectors, r)
	found := false
	for _, s := range sigs {
		if s.Ticker == "SCHW" && s.Type == SignalGovernancePeerUnderperformer {
			found = true
			if s.Score <= 0 {
				t.Error("gap score should be positive")
			}
		}
	}
	if !found {
		t.Error("expected governance_peer_underperformer signal for SCHW")
	}
}

func TestScorePeerGovernanceRank_NoFireAbove(t *testing.T) {
	// All tickers near the median — no underperformer should fire.
	scores := map[string]float64{
		"JPM": 0.78,
		"BAC": 0.80,
		"GS":  0.79,
	}
	sectors := map[string]string{
		"JPM": "financial",
		"BAC": "financial",
		"GS":  "financial",
	}
	sigs := ScorePeerGovernanceRank(scores, sectors, DefaultRules())
	if len(sigs) != 0 {
		t.Errorf("expected no signals when all peers near median, got %d", len(sigs))
	}
}

func TestScorePeerGovernanceRank_SinglePeerSkipped(t *testing.T) {
	// Only one ticker in the sector — no comparison possible.
	scores := map[string]float64{"NEE": 0.45}
	sectors := map[string]string{"NEE": "utilities"}
	sigs := ScorePeerGovernanceRank(scores, sectors, DefaultRules())
	if len(sigs) != 0 {
		t.Errorf("expected no signals for single-peer sector, got %d", len(sigs))
	}
}

func TestScorePeerGovernanceRank_NoSectorSkipped(t *testing.T) {
	// Tickers without sector assignment should be skipped entirely.
	scores := map[string]float64{
		"AAPL": 0.30,
		"MSFT": 0.90,
	}
	sectors := map[string]string{} // no sector info
	sigs := ScorePeerGovernanceRank(scores, sectors, DefaultRules())
	if len(sigs) != 0 {
		t.Errorf("expected no signals when sector map empty, got %d", len(sigs))
	}
}

func TestScorePeerGovernanceRank_HighSeverityLargeGap(t *testing.T) {
	// Gap >= 0.25 should be high severity.
	scores := map[string]float64{
		"TSLA": 0.20,
		"HD":   0.80,
		"COST": 0.82,
	}
	sectors := map[string]string{
		"TSLA": "consumer_discretionary",
		"HD":   "consumer_discretionary",
		"COST": "consumer_discretionary",
	}
	sigs := ScorePeerGovernanceRank(scores, sectors, DefaultRules())
	for _, s := range sigs {
		if s.Ticker == "TSLA" {
			if s.Severity != SeverityHigh {
				t.Errorf("gap of ~0.61 should be high severity, got %s", s.Severity)
			}
			return
		}
	}
	t.Error("expected signal for TSLA")
}

// ── Integration: board_decay_concern + health trend are wired ────────────────

// TestScoreBoardDecayConcern_WiredIntegration verifies the function is callable
// with a realistic combined signal slice matching what the entity-graph main loop produces.
func TestScoreBoardDecayConcern_WiredIntegration(t *testing.T) {
	r := DefaultRules()
	r.MinBoardDecayCount = 2

	today := "2026-06-05"
	signals := []Signal{
		{Type: SignalDirectorDecay, Ticker: "SCHW", Entity: "Director One", FilingDate: today, DetectedAt: today},
		{Type: SignalDirectorDecay, Ticker: "SCHW", Entity: "Director Two", FilingDate: today, DetectedAt: today},
		{Type: SignalDirectorFriction, Ticker: "SCHW", Entity: "Director One", FilingDate: today, DetectedAt: today},
	}
	sig := ScoreBoardDecayConcern("SCHW", signals, r)
	if sig == nil {
		t.Fatal("expected board_decay_concern signal with 2 decaying directors at threshold 2")
	}
	if sig.Type != SignalBoardDecayConcern {
		t.Errorf("signal type = %s, want board_decay_concern", sig.Type)
	}
}

// TestScoreGovernanceHealthTrend_WiredIntegration verifies the function produces
// the correct signal type when called with realistic current/previous scores.
func TestScoreGovernanceHealthTrend_WiredIntegration(t *testing.T) {
	// Simulate a deterioration: previous 0.75, current 0.55 — delta 0.20, above threshold.
	sig := ScoreGovernanceHealthTrend("SCHW", 0.55, 0.75, 0.10)
	if sig == nil {
		t.Fatal("expected governance_deteriorating signal for 0.20 drop")
	}
	if sig.Type != SignalGovernanceDeterioration {
		t.Errorf("signal type = %s, want governance_deteriorating", sig.Type)
	}
	if sig.Ticker != "SCHW" {
		t.Errorf("ticker = %s, want SCHW", sig.Ticker)
	}

	// Improvement: current 0.80, previous 0.65 — delta +0.15, above threshold.
	sigUp := ScoreGovernanceHealthTrend("JPM", 0.80, 0.65, 0.10)
	if sigUp == nil {
		t.Fatal("expected governance_improving signal for 0.15 gain")
	}
	if sigUp.Type != SignalGovernanceImproving {
		t.Errorf("signal type = %s, want governance_improving", sigUp.Type)
	}
}

// ── ScorePostFailureActivistPrediction tests ──────────────────────────────────

func TestScorePostFailureActivistPrediction_FiresOnRecentEntrenchment(t *testing.T) {
	recent := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	sigs := []Signal{{
		SignalID:  "entrench_schw",
		Type:      SignalGovernanceEntrenchment,
		Ticker:    "SCHW",
		Severity:  SeverityMedium,
		DetectedAt: recent,
		FilingDate: recent,
	}}
	sig := ScorePostFailureActivistPrediction("SCHW", sigs, 45)
	if sig == nil {
		t.Fatal("expected post_failure_activist_prediction signal, got nil")
	}
	if sig.Type != SignalPostFailureActivistPrediction {
		t.Errorf("type = %s, want post_failure_activist_prediction", sig.Type)
	}
	if sig.Ticker != "SCHW" {
		t.Errorf("ticker = %s, want SCHW", sig.Ticker)
	}
	if sig.Severity != SeverityMedium {
		t.Errorf("severity = %s, want medium (entrenchment was medium)", sig.Severity)
	}
}

func TestScorePostFailureActivistPrediction_NoFireWithoutEntrenchment(t *testing.T) {
	recent := time.Now().UTC().AddDate(0, 0, -5).Format("2006-01-02")
	sigs := []Signal{{
		Type:       SignalDirectorFriction,
		Ticker:     "SCHW",
		DetectedAt: recent,
		FilingDate: recent,
	}}
	sig := ScorePostFailureActivistPrediction("SCHW", sigs, 45)
	if sig != nil {
		t.Error("expected nil when no governance_entrenchment signal exists")
	}
}

func TestScorePostFailureActivistPrediction_NoFireOutsideWindow(t *testing.T) {
	old := time.Now().UTC().AddDate(0, 0, -90).Format("2006-01-02")
	sigs := []Signal{{
		Type:       SignalGovernanceEntrenchment,
		Ticker:     "SCHW",
		DetectedAt: old,
		FilingDate: old,
	}}
	// Window is 45 days; signal is 90 days old — should not fire.
	sig := ScorePostFailureActivistPrediction("SCHW", sigs, 45)
	if sig != nil {
		t.Error("expected nil when entrenchment signal is outside the window")
	}
}

func TestScorePostFailureActivistPrediction_HighSeverityOnHighEntrenchment(t *testing.T) {
	recent := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	sigs := []Signal{{
		Type:       SignalGovernanceEntrenchment,
		Ticker:     "GS",
		Severity:   SeverityHigh,
		DetectedAt: recent,
		FilingDate: recent,
	}}
	sig := ScorePostFailureActivistPrediction("GS", sigs, 45)
	if sig == nil {
		t.Fatal("expected signal for high-severity entrenchment")
	}
	if sig.Severity != SeverityHigh {
		t.Errorf("severity = %s, want high (entrenchment was high)", sig.Severity)
	}
	if sig.Confidence < 0.70 {
		t.Errorf("confidence = %.3f, want >= 0.70 for high-severity case", sig.Confidence)
	}
}

func TestScorePostFailureActivistPrediction_WrongTickerIgnored(t *testing.T) {
	recent := time.Now().UTC().AddDate(0, 0, -5).Format("2006-01-02")
	sigs := []Signal{{
		Type:       SignalGovernanceEntrenchment,
		Ticker:     "MS",
		DetectedAt: recent,
		FilingDate: recent,
	}}
	sig := ScorePostFailureActivistPrediction("SCHW", sigs, 45)
	if sig != nil {
		t.Error("expected nil when entrenchment is for a different ticker")
	}
}

func TestScorePostFailureActivistPrediction_ValidThroughSixMonths(t *testing.T) {
	recent := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	sigs := []Signal{{
		Type:       SignalGovernanceEntrenchment,
		Ticker:     "JPM",
		DetectedAt: recent,
		FilingDate: recent,
	}}
	sig := ScorePostFailureActivistPrediction("JPM", sigs, 45)
	if sig == nil {
		t.Fatal("expected signal")
	}
	expected := time.Now().UTC().AddDate(0, 6, 0).Format("2006-01-02")
	if sig.ValidThrough != expected {
		t.Errorf("valid_through = %s, want %s (6 months)", sig.ValidThrough, expected)
	}
}

// ── DefaultGovernanceHealthPenalties + ScoreGovernanceHealthWithPenalties ────

// TestDefaultGovernanceHealthPenalties_ContainsExpectedKeys verifies that the default
// penalty map covers all major adverse signal types.
func TestDefaultGovernanceHealthPenalties_ContainsExpectedKeys(t *testing.T) {
	p := DefaultGovernanceHealthPenalties()
	required := []SignalType{
		SignalNominationRejection,
		SignalGovernanceEntrenchment,
		SignalActivistRisk,
		SignalDirectorFriction,
		SignalCFODeparture,
		SignalDirectorLongTenure,
		SignalPostFailureActivistPrediction,
	}
	for _, st := range required {
		if _, ok := p[st]; !ok {
			t.Errorf("DefaultGovernanceHealthPenalties missing key %s", st)
		}
	}
}

// TestScoreGovernanceHealthWithPenalties_NilPenaltiesFallsBackToDefault verifies
// that passing nil penalties produces the same result as calling ScoreGovernanceHealth
// directly (backward-compatibility guarantee).
func TestScoreGovernanceHealthWithPenalties_NilPenaltiesFallsBackToDefault(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "X", DetectedAt: today, Score: 0.80},
	}
	baseline := ScoreGovernanceHealth("X", sigs, 365)
	explicit := ScoreGovernanceHealthWithPenalties("X", sigs, 365, nil)
	if baseline == nil || explicit == nil {
		t.Fatal("expected signals from both calls")
	}
	if baseline.Score != explicit.Score {
		t.Errorf("nil-penalties score %.4f != ScoreGovernanceHealth score %.4f", explicit.Score, baseline.Score)
	}
}

// TestScoreGovernanceHealthWithPenalties_ReducedPenaltyRaisesScore verifies that
// reducing a signal type's penalty weight produces a higher composite score.
func TestScoreGovernanceHealthWithPenalties_ReducedPenaltyRaisesScore(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "Y", DetectedAt: today, Score: 0.80},
	}
	// Baseline: full friction penalty = 0.20 → score 0.80
	baseline := ScoreGovernanceHealth("Y", sigs, 365)
	if baseline == nil {
		t.Fatal("expected baseline signal")
	}

	// Reduced penalty: friction = 0.10 → score 0.90
	reduced := make(map[SignalType]float64)
	for k, v := range DefaultGovernanceHealthPenalties() {
		reduced[k] = v
	}
	reduced[SignalDirectorFriction] = 0.10
	adjusted := ScoreGovernanceHealthWithPenalties("Y", sigs, 365, reduced)
	if adjusted == nil {
		t.Fatal("expected adjusted signal")
	}
	if adjusted.Score <= baseline.Score {
		t.Errorf("reduced penalty should yield higher score: adjusted=%.4f baseline=%.4f", adjusted.Score, baseline.Score)
	}
}

// TestScoreGovernanceHealthWithPenalties_ZeroPenaltyIgnoresSignal verifies that a
// zero penalty for a signal type means it contributes nothing to the health score
// (score stays at 1.0 when only zero-penalty adverse signals are present).
func TestScoreGovernanceHealthWithPenalties_ZeroPenaltyIgnoresSignal(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	sigs := []Signal{
		{Type: SignalDirectorFriction, Ticker: "Z", DetectedAt: today, Score: 0.80},
	}
	zeroed := make(map[SignalType]float64)
	for k, v := range DefaultGovernanceHealthPenalties() {
		zeroed[k] = v
	}
	zeroed[SignalDirectorFriction] = 0.0
	sig := ScoreGovernanceHealthWithPenalties("Z", sigs, 365, zeroed)
	if sig == nil {
		t.Fatal("expected signal even with zero penalty (signal still counted for signalCount)")
	}
	// score = 1.0 (no penalty applied), no trust bonus
	if sig.Score < 0.99 {
		t.Errorf("zero-penalty friction should not reduce health score, got %.4f", sig.Score)
	}
}
