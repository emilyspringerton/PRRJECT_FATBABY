package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

// TestRunBatch_WritesSignalsEvenWithoutANew8K covers the real gap found live
// 2026-08-05 diagnosing "almost all newssite header pages stale, ~3 live
// feeds none work, governance stuff stale": WriteSignals used to sit inside
// `if processed > 0`, but `processed` only counts genuinely new 8-K filings
// parsed this batch. ScoreLongTenure ("runs against the full graph, not just
// this batch") and everything cascading from it compute and log real signals
// every batch regardless -- a batch whose only new record is a non-8-K (a
// press release, a market-data event, ...) used to silently discard all of
// that output. This reproduces exactly that shape: one new record that is
// NOT an 8-K, plus a graph with one long-tenured director (so ScoreLongTenure
// fires unconditionally), and asserts signals.ndjson actually gets written.
func TestRunBatch_WritesSignalsEvenWithoutANew8K(t *testing.T) {
	storeDir := t.TempDir()
	evStore, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer evStore.Close()

	// A real, non-8-K source document -- press releases and other filing
	// types take this same event type but never set is8K.
	doc := intelligence.SourceDocument{
		Identity:         "1:non-8k-doc",
		Ticker:           "TEST",
		SourceType:       "press_release",
		Form:             "",
		CleanedText:      "quarterly results press release, nothing 8-K about it",
		CleanedCharCount: 55,
	}
	if _, err := evStore.Append(context.Background(), mustEvent(t, "ev-1", "source_document_persisted", doc)); err != nil {
		t.Fatalf("append: %v", err)
	}

	graphDir := t.TempDir()
	filingDB, err := openFilingIndexDB(filepath.Join(t.TempDir(), "filings.db"))
	if err != nil {
		t.Fatalf("openFilingIndexDB: %v", err)
	}
	defer filingDB.Close()
	accuracyDB, err := openAccuracyIndexDB(filepath.Join(t.TempDir(), "accuracy.db"))
	if err != nil {
		t.Fatalf("openAccuracyIndexDB: %v", err)
	}
	defer accuracyDB.Close()

	// One long-tenured director at TEST -- first appearance 13 years ago,
	// past DefaultRules' own 12-year LongTenureYearsThreshold, so
	// ScoreLongTenure (called unconditionally in runBatch, not gated on this
	// batch's own new records) produces at least one real signal.
	graph := entitygraph.NewGraph()
	oldDate := time.Now().UTC().AddDate(-13, 0, 0).Format("2006-01-02")
	graph.Nodes["director-1"] = &entitygraph.PersonNode{
		CanonicalID: "director-1",
		Name:        "Longtime Director",
		Type:        entitygraph.NodeDirector,
		Filings: []entitygraph.FilingAppearance{
			{Ticker: "TEST", FilingDate: oldDate, Form: "8-K"},
		},
	}

	cfg := runConfig{
		graphDir:   graphDir,
		obsDir:     t.TempDir(),
		cursorPath: filepath.Join(t.TempDir(), "cursor"),
		batchSize:  100,
		cursor:     0,
		filingDB:      filingDB,
		accuracyDB:    accuracyDB,
		graph:         graph,
		healthHistory: map[string]entitygraph.HealthSnapshot{},
	}

	newCursor, _ := runBatch(context.Background(), evStore, discardLogger(), cfg)
	if newCursor == 0 {
		t.Fatalf("cursor should have advanced past the one appended record")
	}

	signalsPath := filepath.Join(graphDir, "signals.ndjson")
	data, err := os.ReadFile(signalsPath)
	if err != nil {
		t.Fatalf("signals.ndjson should have been written even though no new 8-K was processed: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("signals.ndjson exists but is empty -- ScoreLongTenure's unconditional signals were not persisted")
	}
}
