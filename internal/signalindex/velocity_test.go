package signalindex

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func velRec(seq uint64, ticker string, importance int, ts time.Time) eventstore.Record {
	s := intelligence.Signal{
		ID:         fmt.Sprintf("vel-%d", seq),
		Ticker:     ticker,
		Timestamp:  ts,
		SignalType: "governance",
		Importance: importance,
		RawMetadata: map[string]string{"form": "8-K", "filing_date": ts.Format("2006-01-02")},
	}
	b, _ := json.Marshal(s)
	ev := eventstore.Event{
		ID:           fmt.Sprintf("ev-%d", seq),
		Type:         "signal_generated",
		OccurredAt:   ts,
		PartitionKey: fmt.Sprintf("%d:vel%d", seq, seq),
		Data:         b,
	}
	return eventstore.Record{Sequence: seq, AppendedAt: ts.Add(time.Second), Event: ev}
}

func TestCheckVelocityNoAlerts(t *testing.T) {
	idx := NewIndex()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, age := range []time.Duration{10, 20, 30} {
		if err := idx.Ingest(velRec(uint64(200+i), "AAPL", 80, now.Add(-age*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	alerts := idx.CheckVelocity(now, DefaultVelocityThreshold)
	if len(alerts) != 0 {
		t.Errorf("want 0 alerts for exactly 3 signals (not >3), got %d", len(alerts))
	}
}

func TestCheckVelocityTriggered(t *testing.T) {
	idx := NewIndex()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, age := range []time.Duration{5, 10, 20, 30} {
		if err := idx.Ingest(velRec(uint64(300+i), "AAPL", 80, now.Add(-age*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	alerts := idx.CheckVelocity(now, DefaultVelocityThreshold)
	if len(alerts) != 1 {
		t.Fatalf("want 1 alert, got %d", len(alerts))
	}
	if alerts[0].Ticker != "AAPL" {
		t.Errorf("ticker: want AAPL, got %s", alerts[0].Ticker)
	}
	if alerts[0].Count != 4 {
		t.Errorf("count: want 4, got %d", alerts[0].Count)
	}
}

func TestCheckVelocityLowImportanceExcluded(t *testing.T) {
	idx := NewIndex()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// 3 high + 2 low importance — only 3 qualify, which is NOT >3
	importances := []int{80, 80, 50, 80, 30}
	ages := []time.Duration{10, 15, 20, 25, 30}
	for i := range importances {
		if err := idx.Ingest(velRec(uint64(400+i), "MSFT", importances[i], now.Add(-ages[i]*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	alerts := idx.CheckVelocity(now, DefaultVelocityThreshold)
	if len(alerts) != 0 {
		t.Errorf("want 0 alerts (only 3 qualifying), got %d", len(alerts))
	}
}

func TestCheckVelocityOutsideWindow(t *testing.T) {
	idx := NewIndex()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ages := []time.Duration{10, 20, 30, 90} // 4th is outside 1h window
	for i, age := range ages {
		if err := idx.Ingest(velRec(uint64(500+i), "GOOG", 80, now.Add(-age*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	alerts := idx.CheckVelocity(now, DefaultVelocityThreshold)
	if len(alerts) != 0 {
		t.Errorf("want 0 alerts (4th signal outside window), got %d", len(alerts))
	}
}

func TestCheckVelocityMultipleTickers(t *testing.T) {
	idx := NewIndex()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// NVDA: 4 signals in window → alert
	// AMD: 4 signals in window → alert
	// INTC: 2 signals → no alert
	type entry struct {
		ticker string
		imp    int
		age    time.Duration
	}
	entries := []entry{
		{"NVDA", 80, 5}, {"NVDA", 80, 10}, {"NVDA", 80, 15}, {"NVDA", 80, 20},
		{"AMD", 80, 5}, {"AMD", 80, 10}, {"AMD", 80, 15}, {"AMD", 80, 20},
		{"INTC", 80, 5}, {"INTC", 80, 10},
	}
	for i, e := range entries {
		if err := idx.Ingest(velRec(uint64(600+i), e.ticker, e.imp, now.Add(-e.age*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	alerts := idx.CheckVelocity(now, DefaultVelocityThreshold)
	if len(alerts) != 2 {
		t.Errorf("want 2 alerts (NVDA + AMD), got %d", len(alerts))
	}
}
