package cooccurrence_test

import (
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/cooccurrence"
)

var now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestRecordEdgeCreated(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("AAPL", "MSFT", now)
	if s.EdgeCount() != 1 {
		t.Errorf("want 1 edge, got %d", s.EdgeCount())
	}
}

func TestRecordStrengthens(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("AAPL", "MSFT", now)
	s.Record("AAPL", "MSFT", now.Add(time.Hour))
	edges := s.AllEdges()
	if len(edges) != 1 || edges[0].Weight != 2 {
		t.Errorf("want 1 edge with weight 2, got %v", edges)
	}
}

func TestRecordSymmetry(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("MSFT", "AAPL", now)
	s.Record("AAPL", "MSFT", now.Add(time.Hour))
	if s.EdgeCount() != 1 {
		t.Errorf("want 1 edge (symmetric), got %d", s.EdgeCount())
	}
}

func TestRecordIgnoresSameTicker(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("AAPL", "AAPL", now)
	if s.EdgeCount() != 0 {
		t.Errorf("want 0 edges for same ticker, got %d", s.EdgeCount())
	}
}

func TestRecordIgnoresEmpty(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("", "AAPL", now)
	s.Record("AAPL", "", now)
	if s.EdgeCount() != 0 {
		t.Errorf("want 0 edges for empty ticker, got %d", s.EdgeCount())
	}
}

func TestRecordGroup(t *testing.T) {
	s := cooccurrence.NewStore()
	s.RecordGroup([]string{"AAPL", "MSFT", "GOOG"}, now)
	if s.EdgeCount() != 3 {
		t.Errorf("want 3 edges for 3-way group, got %d", s.EdgeCount())
	}
}

func TestRelatedTopByWeight(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("AAPL", "MSFT", now)
	s.Record("AAPL", "MSFT", now.Add(time.Hour))
	s.Record("AAPL", "GOOG", now)
	related := s.Related("AAPL")
	if len(related) < 2 {
		t.Fatalf("want ≥2 related, got %d", len(related))
	}
	if related[0].Ticker != "MSFT" {
		t.Errorf("want MSFT first (higher weight), got %s", related[0].Ticker)
	}
}

func TestRelatedEmpty(t *testing.T) {
	s := cooccurrence.NewStore()
	if r := s.Related("NOBODY"); len(r) != 0 {
		t.Errorf("want empty for unknown ticker, got %v", r)
	}
}

func TestRelatedMaxCap(t *testing.T) {
	s := cooccurrence.NewStore()
	tickers := []string{"T1","T2","T3","T4","T5","T6","T7","T8","T9","T10","T11","T12"}
	for _, t2 := range tickers[1:] {
		s.Record("T1", t2, now)
	}
	related := s.Related("T1")
	if len(related) > cooccurrence.MaxRelated {
		t.Errorf("want at most %d, got %d", cooccurrence.MaxRelated, len(related))
	}
}

func TestAllEdgesOrdered(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("A", "B", now)
	s.Record("A", "B", now)
	s.Record("C", "D", now)
	edges := s.AllEdges()
	if len(edges) < 2 {
		t.Fatal("expected 2 edges")
	}
	if edges[0].Weight < edges[1].Weight {
		t.Error("edges should be sorted by weight descending")
	}
}

func TestEdgeCountZero(t *testing.T) {
	s := cooccurrence.NewStore()
	if s.EdgeCount() != 0 {
		t.Errorf("want 0, got %d", s.EdgeCount())
	}
}

func TestRelatedEntryFields(t *testing.T) {
	s := cooccurrence.NewStore()
	s.Record("AAPL", "NVDA", now)
	related := s.Related("AAPL")
	if len(related) != 1 {
		t.Fatalf("want 1, got %d", len(related))
	}
	if related[0].Ticker != "NVDA" {
		t.Errorf("ticker: want NVDA, got %s", related[0].Ticker)
	}
	if related[0].Weight != 1 {
		t.Errorf("weight: want 1, got %d", related[0].Weight)
	}
	if related[0].LastSeen.IsZero() {
		t.Error("last_seen should not be zero")
	}
}
