package graphread

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// writeNDJSON writes records as NDJSON to dir/filename.
func writeNDJSON(t *testing.T, dir, filename string, records []any) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefresh_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh on empty dir: %v", err)
	}
	if got := s.AllSignals(); len(got) != 0 {
		t.Errorf("AllSignals = %d, want 0", len(got))
	}
	if got := s.AllNodes(); len(got) != 0 {
		t.Errorf("AllNodes = %d, want 0", len(got))
	}
	if got := s.AllTickers(); len(got) != 0 {
		t.Errorf("AllTickers = %d, want 0", len(got))
	}
}

func TestRefresh_LoadsSignals(t *testing.T) {
	dir := t.TempDir()
	sigs := []any{
		entitygraph.Signal{SignalID: "s1", Type: "director_friction", Ticker: "AAPL", FilingDate: "2023-01-10", ValidThrough: "2024-01-10"},
		entitygraph.Signal{SignalID: "s2", Type: "governance_entrenchment", Ticker: "SCHW", FilingDate: "2023-06-01", ValidThrough: "2024-06-01"},
	}
	writeNDJSON(t, dir, "signals.ndjson", sigs)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	all := s.AllSignals()
	if len(all) != 2 {
		t.Fatalf("AllSignals = %d, want 2", len(all))
	}
}

func TestLiveSignals_FiltersExpired(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")

	sigs := []any{
		entitygraph.Signal{SignalID: "live", Type: "director_friction", Ticker: "AAPL", ValidThrough: tomorrow},
		entitygraph.Signal{SignalID: "expired", Type: "director_friction", Ticker: "AAPL", ValidThrough: yesterday},
		entitygraph.Signal{SignalID: "no-expiry", Type: "high_trust_director", Ticker: "AAPL"},
	}
	writeNDJSON(t, dir, "signals.ndjson", sigs)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	live := s.LiveSignals("AAPL", today)
	if len(live) != 2 {
		t.Fatalf("LiveSignals = %d, want 2 (live + no-expiry)", len(live))
	}
	for _, sig := range live {
		if sig.SignalID == "expired" {
			t.Error("expired signal should not appear in LiveSignals")
		}
	}
}

func TestLiveSignals_FiltersByTicker(t *testing.T) {
	dir := t.TempDir()
	sigs := []any{
		entitygraph.Signal{SignalID: "a1", Ticker: "AAPL"},
		entitygraph.Signal{SignalID: "s1", Ticker: "SCHW"},
	}
	writeNDJSON(t, dir, "signals.ndjson", sigs)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	got := s.LiveSignals("AAPL", "9999-12-31")
	if len(got) != 1 || got[0].SignalID != "a1" {
		t.Errorf("LiveSignals(AAPL) = %v, want [a1]", got)
	}
}

func TestLiveSignals_EmptyTickerReturnsAll(t *testing.T) {
	dir := t.TempDir()
	sigs := []any{
		entitygraph.Signal{SignalID: "a1", Ticker: "AAPL"},
		entitygraph.Signal{SignalID: "s1", Ticker: "SCHW"},
	}
	writeNDJSON(t, dir, "signals.ndjson", sigs)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	got := s.LiveSignals("", "9999-12-31")
	if len(got) != 2 {
		t.Errorf("LiveSignals('') = %d, want 2", len(got))
	}
}

func TestRefresh_LoadsNodes(t *testing.T) {
	dir := t.TempDir()
	nodes := []any{
		entitygraph.PersonNode{
			CanonicalID: "jane-doe",
			Name:        "Jane Doe",
			Type:        entitygraph.NodeDirector,
			Filings: []entitygraph.FilingAppearance{
				{Ticker: "AAPL", FilingDate: "2023-03-15"},
			},
		},
	}
	writeNDJSON(t, dir, "nodes.ndjson", nodes)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	node, ok := s.Node("jane-doe")
	if !ok {
		t.Fatal("Node(jane-doe) not found")
	}
	if node.Name != "Jane Doe" {
		t.Errorf("node.Name = %q, want %q", node.Name, "Jane Doe")
	}
	dirs := s.DirectorsFor("AAPL")
	if len(dirs) != 1 || dirs[0].CanonicalID != "jane-doe" {
		t.Errorf("DirectorsFor(AAPL) = %v, want [jane-doe]", dirs)
	}
}

func TestRefresh_LoadsAuditors(t *testing.T) {
	dir := t.TempDir()
	auditors := []any{
		entitygraph.AuditorRecord{Ticker: "AAPL", Auditor: "Deloitte", FilingDate: "2023-01-01"},
	}
	writeNDJSON(t, dir, "auditors.ndjson", auditors)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	rec, ok := s.AuditorFor("AAPL")
	if !ok {
		t.Fatal("AuditorFor(AAPL) not found")
	}
	if rec.Auditor != "Deloitte" {
		t.Errorf("Auditor = %q, want Deloitte", rec.Auditor)
	}
}

func TestHealthTrendFor_WithPreviousSnapshot(t *testing.T) {
	dir := t.TempDir()
	history := []any{
		entitygraph.HealthSnapshot{Ticker: "SCHW", Score: 0.50, RecordedAt: "2026-05-01"},
		entitygraph.HealthSnapshot{Ticker: "SCHW", Score: 0.35, RecordedAt: "2026-06-01"},
	}
	writeNDJSON(t, dir, "health_history.ndjson", history)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	current, previous, ok := s.HealthTrendFor("schw")
	if !ok {
		t.Fatal("HealthTrendFor(schw) not found")
	}
	if current.Score != 0.35 {
		t.Errorf("current.Score = %.2f, want 0.35", current.Score)
	}
	if previous == nil || previous.Score != 0.50 {
		t.Errorf("previous = %+v, want Score 0.50", previous)
	}
}

func TestHealthTrendFor_OnlyOneSnapshotHasNoPrevious(t *testing.T) {
	dir := t.TempDir()
	writeNDJSON(t, dir, "health_history.ndjson", []any{
		entitygraph.HealthSnapshot{Ticker: "AAPL", Score: 0.90, RecordedAt: "2026-06-01"},
	})
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	current, previous, ok := s.HealthTrendFor("AAPL")
	if !ok {
		t.Fatal("HealthTrendFor(AAPL) not found")
	}
	if current.Score != 0.90 {
		t.Errorf("current.Score = %.2f, want 0.90", current.Score)
	}
	if previous != nil {
		t.Errorf("previous = %+v, want nil (only one snapshot ever recorded)", previous)
	}
}

func TestHealthTrendFor_UnknownTicker(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	_, _, ok := s.HealthTrendFor("NOPE")
	if ok {
		t.Error("expected ok=false for a ticker with no health data")
	}
}

func TestRefresh_LoadsEdges(t *testing.T) {
	dir := t.TempDir()
	edges := []any{
		entitygraph.Edge{Source: "alice", Target: "bob", EdgeType: entitygraph.EdgeBoardCoMember, Companies: []string{"AAPL"}, Strength: 3},
	}
	writeNDJSON(t, dir, "edges.ndjson", edges)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	aliceEdges := s.EdgesFor("alice")
	if len(aliceEdges) != 1 {
		t.Fatalf("EdgesFor(alice) = %d, want 1", len(aliceEdges))
	}
	if aliceEdges[0].Target != "bob" {
		t.Errorf("edge target = %q, want bob", aliceEdges[0].Target)
	}
	// EdgesFor is bidirectional: bob should also see the edge
	bobEdges := s.EdgesFor("bob")
	if len(bobEdges) != 1 {
		t.Fatalf("EdgesFor(bob) = %d, want 1", len(bobEdges))
	}
}

func TestSignalsForPerson_FiltersByCanonicalID(t *testing.T) {
	dir := t.TempDir()
	sigs := []any{
		entitygraph.Signal{SignalID: "s1", Ticker: "AAPL", Entity: "Jane Doe"},
		entitygraph.Signal{SignalID: "s2", Ticker: "SCHW", Entity: "John Smith"},
	}
	writeNDJSON(t, dir, "signals.ndjson", sigs)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	// "Jane Doe" canonicalizes to "jane-doe"
	got := s.SignalsForPerson("jane-doe")
	if len(got) != 1 || got[0].SignalID != "s1" {
		t.Errorf("SignalsForPerson = %v, want [s1]", got)
	}
}

func TestUpdates_ClosedOnRefresh(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	ch := s.Updates()
	// Channel should not be closed yet
	select {
	case <-ch:
		t.Fatal("updates channel closed before Refresh")
	default:
	}
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	// Channel should now be closed
	select {
	case <-ch:
		// expected
	default:
		t.Fatal("updates channel not closed after Refresh")
	}
}

func TestAllTickers_ReturnsKnownTickers(t *testing.T) {
	dir := t.TempDir()
	nodes := []any{
		entitygraph.PersonNode{
			CanonicalID: "alice",
			Name:        "Alice",
			Type:        entitygraph.NodeDirector,
			Filings: []entitygraph.FilingAppearance{
				{Ticker: "MSFT", FilingDate: "2023-01-01"},
				{Ticker: "AAPL", FilingDate: "2023-02-01"},
			},
		},
	}
	writeNDJSON(t, dir, "nodes.ndjson", nodes)
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	tickers := s.AllTickers()
	if len(tickers) != 2 {
		t.Errorf("AllTickers = %v, want 2 tickers", tickers)
	}
}

func TestRulesUpdatedAt_ZeroWhenNotConfigured(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatal(err)
	}
	if !s.RulesUpdatedAt().IsZero() {
		t.Error("RulesUpdatedAt should be zero when no rules file configured")
	}
}
