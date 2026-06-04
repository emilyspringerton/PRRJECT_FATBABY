package buyback_test

import (
	"testing"

	"github.com/example/prrject-fatbaby/internal/buyback"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

func TestClassify_Authorization(t *testing.T) {
	ev := buyback.Classify(
		"AAPL Announces $90 Billion Share Repurchase Program",
		"Apple Inc. announced today that the Board of Directors has authorized a new share repurchase program of up to $90 billion.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != buyback.EventAuthorization {
		t.Errorf("want authorization, got %s", ev.EventType)
	}
	if ev.AuthorizedUSD != 90e9 {
		t.Errorf("authorized USD: want 90B, got %.0f", ev.AuthorizedUSD)
	}
}

func TestClassify_Suspension(t *testing.T) {
	ev := buyback.Classify(
		"Company Suspends Share Repurchase Program",
		"In light of current market conditions, management has decided to suspend its share repurchase program.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != buyback.EventSuspension {
		t.Errorf("want suspension, got %s", ev.EventType)
	}
}

func TestClassify_Completion(t *testing.T) {
	ev := buyback.Classify(
		"MSFT Completes $60 Billion Buyback Program",
		"Microsoft announced it has completed its previously authorized $60 billion share repurchase program.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != buyback.EventCompletion {
		t.Errorf("want completion, got %s", ev.EventType)
	}
}

func TestClassify_Update_NoSignal(t *testing.T) {
	ev := buyback.Classify(
		"Q1 Repurchase Activity Update",
		"During Q1, the Company repurchased 1.5 million shares under its existing buyback program.",
	)
	if ev == nil {
		t.Fatal("expected event (update)")
	}
	if ev.EventType != buyback.EventUpdate {
		t.Errorf("want update, got %s", ev.EventType)
	}
	sigs := buyback.Score(ev)
	if len(sigs) != 0 {
		t.Errorf("update should produce no signals, got %d", len(sigs))
	}
}

func TestClassify_NoBuyback(t *testing.T) {
	ev := buyback.Classify(
		"Company Reports Record Revenue",
		"Revenue grew 25% year over year driven by strong demand across all segments.",
	)
	if ev != nil {
		t.Errorf("expected nil for non-buyback PR, got %+v", ev)
	}
}

func TestScore_Authorization(t *testing.T) {
	ev := &buyback.BuybackEvent{
		Ticker:        "AAPL",
		EventType:     buyback.EventAuthorization,
		AuthorizedUSD: 90e9,
		PublishedAt:   "2026-06-01",
	}
	sigs := buyback.Score(ev)
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(sigs))
	}
	if sigs[0].Type != entitygraph.SignalBuybackAuthorization {
		t.Errorf("type: want buyback_authorization, got %s", sigs[0].Type)
	}
}

func TestScore_Suspension(t *testing.T) {
	ev := &buyback.BuybackEvent{
		Ticker:      "GE",
		EventType:   buyback.EventSuspension,
		PublishedAt: "2026-06-01",
	}
	sigs := buyback.Score(ev)
	if len(sigs) == 0 {
		t.Fatal("expected signal")
	}
	if sigs[0].Type != entitygraph.SignalBuybackSuspension {
		t.Errorf("type: want buyback_suspension, got %s", sigs[0].Type)
	}
	if sigs[0].Severity != entitygraph.SeverityMedium {
		t.Errorf("suspension should be medium, got %s", sigs[0].Severity)
	}
}

func TestScore_NoTicker(t *testing.T) {
	ev := &buyback.BuybackEvent{Ticker: "", EventType: buyback.EventAuthorization}
	if sigs := buyback.Score(ev); len(sigs) != 0 {
		t.Errorf("no-ticker: expected 0 signals, got %d", len(sigs))
	}
}

func TestExtractUSD_Billion(t *testing.T) {
	ev := buyback.Classify("X", "Board approved a $2.5 billion share repurchase program.")
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.AuthorizedUSD != 2.5e9 {
		t.Errorf("USD: want 2.5B, got %.0f", ev.AuthorizedUSD)
	}
}

func TestExtractUSD_Million(t *testing.T) {
	ev := buyback.Classify("X", "The company authorized a $500 million repurchase program.")
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.AuthorizedUSD != 500e6 {
		t.Errorf("USD: want 500M, got %.0f", ev.AuthorizedUSD)
	}
}
