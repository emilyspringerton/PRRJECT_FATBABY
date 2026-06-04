package dividend_test

import (
	"testing"

	"github.com/example/prrject-fatbaby/internal/dividend"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

func TestClassify_Cut(t *testing.T) {
	ev := dividend.Classify(
		"XYZ Corp Reduces Quarterly Dividend",
		"The Board of Directors of XYZ Corp announced today that it has reduced its quarterly cash dividend from $0.50 to $0.25 per share.",
	)
	if ev == nil {
		t.Fatal("expected DividendEvent, got nil")
	}
	if ev.EventType != dividend.EventCut {
		t.Errorf("EventType: want cut, got %s", ev.EventType)
	}
}

func TestClassify_Suspension(t *testing.T) {
	ev := dividend.Classify(
		"Company Suspends Quarterly Dividend",
		"Due to current market conditions, the Board has decided to suspend its quarterly cash dividend indefinitely.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != dividend.EventSuspension {
		t.Errorf("want suspension, got %s", ev.EventType)
	}
}

func TestClassify_Raise(t *testing.T) {
	ev := dividend.Classify(
		"AAPL Increases Quarterly Dividend by 5%",
		"Apple Inc announced today that it has increased its quarterly cash dividend from $0.24 to $0.25 per share.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != dividend.EventRaise {
		t.Errorf("want raise, got %s", ev.EventType)
	}
	if ev.AmountPerShare != 0.25 {
		t.Errorf("amount: want 0.25, got %.4f", ev.AmountPerShare)
	}
}

func TestClassify_Special(t *testing.T) {
	ev := dividend.Classify(
		"MSFT Declares Special Cash Dividend of $3.00 Per Share",
		"Microsoft Corporation announced a special cash dividend of $3.00 per share payable to shareholders of record as of June 30, 2026.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != dividend.EventSpecial {
		t.Errorf("want special, got %s", ev.EventType)
	}
	if ev.AmountPerShare != 3.0 {
		t.Errorf("amount: want 3.00, got %.4f", ev.AmountPerShare)
	}
}

func TestClassify_Regular(t *testing.T) {
	ev := dividend.Classify(
		"Company Declares Regular Quarterly Dividend",
		"The Board declared its regular quarterly cash dividend of $0.35 per share. Record date: June 15, 2026. Payment date: July 1, 2026.",
	)
	if ev == nil {
		t.Fatal("expected event (regular)")
	}
	if ev.EventType != dividend.EventRegular {
		t.Errorf("want regular, got %s", ev.EventType)
	}
}

func TestClassify_NoDividend(t *testing.T) {
	ev := dividend.Classify(
		"Company Reports Q1 Earnings",
		"Revenue increased 10% year over year. EPS was $1.25.",
	)
	if ev != nil {
		t.Errorf("expected nil for non-dividend PR, got %+v", ev)
	}
}

func TestClassify_DateExtraction(t *testing.T) {
	ev := dividend.Classify(
		"Co Declares Quarterly Dividend",
		"Record date: June 15, 2026. Payment date: July 1, 2026. Dividend of $0.40 per share.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.RecordDate == "" {
		t.Error("expected record date to be extracted")
	}
	if ev.PaymentDate == "" {
		t.Error("expected payment date to be extracted")
	}
}

func TestScore_CutSignal(t *testing.T) {
	ev := &dividend.DividendEvent{
		Ticker:         "SCHW",
		EventType:      dividend.EventCut,
		AmountPerShare: 0.25,
		PublishedAt:    "2026-06-01",
	}
	sigs := dividend.Score(ev)
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(sigs))
	}
	if sigs[0].Type != entitygraph.SignalDividendCut {
		t.Errorf("type: want dividend_cut, got %s", sigs[0].Type)
	}
	if sigs[0].Severity != entitygraph.SeverityHigh {
		t.Errorf("severity: want high, got %s", sigs[0].Severity)
	}
}

func TestScore_SuspensionIsCritical(t *testing.T) {
	ev := &dividend.DividendEvent{
		Ticker:      "GE",
		EventType:   dividend.EventSuspension,
		PublishedAt: "2026-06-01",
	}
	sigs := dividend.Score(ev)
	if len(sigs) == 0 {
		t.Fatal("expected signal")
	}
	if sigs[0].Severity != entitygraph.SeverityCritical {
		t.Errorf("suspension should be critical, got %s", sigs[0].Severity)
	}
}

func TestScore_Regular_NoSignal(t *testing.T) {
	ev := &dividend.DividendEvent{
		Ticker:    "AAPL",
		EventType: dividend.EventRegular,
	}
	sigs := dividend.Score(ev)
	if len(sigs) != 0 {
		t.Errorf("regular dividend should produce no signals, got %d", len(sigs))
	}
}

func TestScore_NoTicker_NoSignal(t *testing.T) {
	ev := &dividend.DividendEvent{
		Ticker:    "",
		EventType: dividend.EventCut,
	}
	sigs := dividend.Score(ev)
	if len(sigs) != 0 {
		t.Errorf("no-ticker cut should produce no signals, got %d", len(sigs))
	}
}
