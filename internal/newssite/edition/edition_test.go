package edition

import (
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

func TestGenerateHeadline(t *testing.T) {
	tests := []struct {
		name string
		sig  entitygraph.Signal
		want string
	}{
		{
			name: "nomination rejection with entity",
			sig:  entitygraph.Signal{Type: entitygraph.SignalNominationRejection, Ticker: "schw", Entity: "Julia Herringer", Score: 0.48},
			want: "Julia Herringer fails to win majority at SCHW",
		},
		{
			name: "nomination rejection without entity",
			sig:  entitygraph.Signal{Type: entitygraph.SignalNominationRejection, Ticker: "SCHW"},
			want: "Director fails to win majority at SCHW",
		},
		{
			name: "activist risk",
			sig:  entitygraph.Signal{Type: entitygraph.SignalActivistRisk, Ticker: "aapl"},
			want: "AAPL shows activist setup: entrenchment meets friction",
		},
		{
			name: "director friction with entity",
			sig:  entitygraph.Signal{Type: entitygraph.SignalDirectorFriction, Ticker: "MSFT", Entity: "John Smith", Score: 0.843},
			want: "John Smith draws 16% opposition at MSFT",
		},
		{
			name: "director friction without entity",
			sig:  entitygraph.Signal{Type: entitygraph.SignalDirectorFriction, Ticker: "MSFT", Score: 0.80},
			want: "Director draws 20% opposition at MSFT",
		},
		{
			name: "governance entrenchment",
			sig:  entitygraph.Signal{Type: entitygraph.SignalGovernanceEntrenchment, Ticker: "TSLA"},
			want: "TSLA board defense blocks majority-backed proposal",
		},
		{
			name: "director decay with entity",
			sig:  entitygraph.Signal{Type: entitygraph.SignalDirectorDecay, Ticker: "META", Entity: "Bob Jones"},
			want: "Support for Bob Jones keeps sliding at META",
		},
		{
			name: "auditor change with metadata",
			sig:  entitygraph.Signal{Type: entitygraph.SignalAuditorChange, Ticker: "NFLX", Metadata: map[string]string{"new_auditor": "KPMG", "prev_auditor": "Deloitte"}},
			want: "NFLX switches auditors to KPMG",
		},
		{
			name: "auditor change without metadata",
			sig:  entitygraph.Signal{Type: entitygraph.SignalAuditorChange, Ticker: "AMZN"},
			want: "AMZN auditor change detected",
		},
		{
			name: "compensation concern",
			sig:  entitygraph.Signal{Type: entitygraph.SignalCompensationConcern, Ticker: "GS"},
			want: "Investors push back on GS executive pay",
		},
		{
			name: "broker nonvote anomaly",
			sig:  entitygraph.Signal{Type: entitygraph.SignalBrokerNonVoteAnomaly, Ticker: "WFC"},
			want: "Unusual broker non-vote levels at WFC",
		},
		{
			name: "abstention spike",
			sig:  entitygraph.Signal{Type: entitygraph.SignalAbstentionSpike, Ticker: "BAC"},
			want: "Abstentions spike on BAC proposal",
		},
		{
			name: "family control",
			sig:  entitygraph.Signal{Type: entitygraph.SignalFamilyControl, Ticker: "META"},
			want: "Founder-family grip persists on META board",
		},
		{
			name: "high trust director with entity",
			sig:  entitygraph.Signal{Type: entitygraph.SignalHighTrustDirector, Ticker: "JNJ", Entity: "Alice Chen"},
			want: "Alice Chen re-elected with strong backing at JNJ",
		},
		{
			name: "director link with entity",
			sig:  entitygraph.Signal{Type: entitygraph.SignalDirectorLink, Ticker: "GE", Entity: "Frank Lee"},
			want: "Frank Lee's friction spans multiple boards",
		},
		{
			name: "governance health A",
			sig:  entitygraph.Signal{Type: entitygraph.SignalGovernanceHealth, Ticker: "MSFT", Score: 0.92},
			want: "MSFT governance health: A (excellent)",
		},
		{
			name: "governance health F",
			sig:  entitygraph.Signal{Type: entitygraph.SignalGovernanceHealth, Ticker: "ENRN", Score: 0.20},
			want: "ENRN governance health: F (severe)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateHeadline(tt.sig)
			if got != tt.want {
				t.Errorf("GenerateHeadline() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRank(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	sigs := []entitygraph.Signal{
		{SignalID: "a", Ticker: "A", Type: entitygraph.SignalNominationRejection, Severity: entitygraph.SeverityCritical, Confidence: 0.95, DetectedAt: today, ValidThrough: tomorrow},
		{SignalID: "b", Ticker: "B", Type: entitygraph.SignalDirectorFriction, Severity: entitygraph.SeverityHigh, Confidence: 0.85, DetectedAt: today, ValidThrough: tomorrow},
		{SignalID: "c", Ticker: "C", Type: entitygraph.SignalHighTrustDirector, Severity: entitygraph.SeverityLow, Confidence: 0.70, DetectedAt: today, ValidThrough: tomorrow},
		{SignalID: "d", Ticker: "D", Type: entitygraph.SignalActivistRisk, Severity: entitygraph.SeverityCritical, Confidence: 0.82, DetectedAt: today, ValidThrough: yesterday}, // expired
	}

	ranked := Rank(sigs, today)

	// Expired signal must not appear.
	for _, r := range ranked {
		if r.Signal.SignalID == "d" {
			t.Errorf("expired signal 'd' appeared in ranked output")
		}
	}

	if len(ranked) != 3 {
		t.Fatalf("expected 3 ranked signals, got %d", len(ranked))
	}

	// Critical must outrank high must outrank low (all detected today, same recency).
	if ranked[0].Signal.SignalID != "a" {
		t.Errorf("expected critical signal 'a' to be top-ranked, got %q", ranked[0].Signal.SignalID)
	}
	if ranked[1].Signal.SignalID != "b" {
		t.Errorf("expected high signal 'b' to be second, got %q", ranked[1].Signal.SignalID)
	}
	if ranked[2].Signal.SignalID != "c" {
		t.Errorf("expected low signal 'c' to be third, got %q", ranked[2].Signal.SignalID)
	}
}

func TestRankDeduplicates(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")

	sigs := []entitygraph.Signal{
		{SignalID: "dup", Ticker: "X", Type: entitygraph.SignalActivistRisk, Severity: entitygraph.SeverityHigh, Confidence: 0.8, DetectedAt: today, ValidThrough: tomorrow},
		{SignalID: "dup", Ticker: "X", Type: entitygraph.SignalActivistRisk, Severity: entitygraph.SeverityHigh, Confidence: 0.8, DetectedAt: today, ValidThrough: tomorrow},
	}

	ranked := Rank(sigs, today)
	if len(ranked) != 1 {
		t.Errorf("expected 1 deduplicated signal, got %d", len(ranked))
	}
}

func TestSectionFor(t *testing.T) {
	cases := map[entitygraph.SignalType]string{
		entitygraph.SignalActivistRisk:           "activism",
		entitygraph.SignalNominationRejection:    "activism",
		entitygraph.SignalDirectorFriction:       "activism",
		entitygraph.SignalDirectorLink:           "activism",
		entitygraph.SignalGovernanceEntrenchment: "governance",
		entitygraph.SignalGovernanceHealth:       "governance",
		entitygraph.SignalFamilyControl:          "governance",
		entitygraph.SignalDirectorDecay:          "boardroom",
		entitygraph.SignalHighTrustDirector:      "boardroom",
		entitygraph.SignalAbstentionSpike:        "boardroom",
		entitygraph.SignalAuditorChange:          "auditor",
		entitygraph.SignalCompensationConcern:    "pay",
		entitygraph.SignalBrokerNonVoteAnomaly:   "pay",
	}
	for sig, want := range cases {
		got := SectionFor(sig)
		if got != want {
			t.Errorf("SectionFor(%q) = %q, want %q", sig, got, want)
		}
	}
}
