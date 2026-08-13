package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/prwatch"
)

func mustEvent(t *testing.T, id, typ string, v any) eventstore.Event {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return eventstore.Event{ID: id, Type: typ, Data: b}
}

func testLogger() *log.Logger {
	return log.New(os.Stderr, "test ", 0)
}

// TestRunBatch_PersistsSourceDocumentWithTicker covers the real gap this
// binary exists to close (2026-08-13): nothing in the pipeline ever wrote a
// source_document_persisted event for a press release, so newssite's
// docindex never saw any -- confirmed live via `grep -rl
// source_document_persisted var/*` finding only var/secwatch, none in
// var/prwatch-body, ever. This proves the core join (pr_discovered's
// PrimaryTicker -> pr_body_fetched's body -> a real source_document_persisted
// event) actually works end to end.
func TestRunBatch_PersistsSourceDocumentWithTicker(t *testing.T) {
	bodyStore, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new body store: %v", err)
	}
	defer bodyStore.Close()

	outStore, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new out store: %v", err)
	}
	defer outStore.Close()

	body := make([]byte, 500)
	for i := range body {
		body[i] = 'x'
	}
	ev := prwatch.BodyFetchedEvent{
		PRDiscoveryID: "pr-1",
		Headline:      "Acme Corp Reports Record Quarter (NYSE:ACME)",
		Company:       "Acme Corp",
		URL:           "https://example.com/pr-1",
		Body:          string(body),
		FetchedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := bodyStore.Append(context.Background(), mustEvent(t, "pr_body_fetched:pr-1", "pr_body_fetched", ev)); err != nil {
		t.Fatalf("append body event: %v", err)
	}

	tickerByID := map[string]string{"pr-1": "ACME"}
	cursorPath := t.TempDir() + "/.cursor"

	newCursor := runBatch(context.Background(), bodyStore, outStore, tickerByID, testLogger(), batchConfig{
		cursorPath: cursorPath,
		batchSize:  256,
		cursor:     1,
	})
	if newCursor <= 1 {
		t.Fatalf("cursor did not advance: %d", newCursor)
	}

	recs, err := outStore.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("read out store: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("out store has %d records, want 1", len(recs))
	}
	if recs[0].Event.Type != "source_document_persisted" {
		t.Fatalf("event type = %q, want source_document_persisted", recs[0].Event.Type)
	}

	var doc struct {
		Identity   string `json:"identity"`
		Ticker     string `json:"ticker"`
		SourceType string `json:"source_type"`
	}
	if err := json.Unmarshal(recs[0].Event.Data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Ticker != "ACME" {
		t.Errorf("ticker = %q, want ACME", doc.Ticker)
	}
	if doc.SourceType != "press_release" {
		t.Errorf("source_type = %q, want press_release", doc.SourceType)
	}
	if doc.Identity != "press_release:pr-1" {
		t.Errorf("identity = %q, want press_release:pr-1", doc.Identity)
	}
}

// TestRunBatch_SkipsWhenNoTickerFound covers the real, separate gap flagged
// in runBatch's own doc comment: a press release with no (EXCHANGE:TICKER)
// mention anywhere prwatch's discovery scan caught can't be indexed at all
// today. This asserts that's a clean skip, not a crash or a bad write.
func TestRunBatch_SkipsWhenNoTickerFound(t *testing.T) {
	bodyStore, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new body store: %v", err)
	}
	defer bodyStore.Close()

	outStore, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new out store: %v", err)
	}
	defer outStore.Close()

	body := make([]byte, 500)
	for i := range body {
		body[i] = 'x'
	}
	ev := prwatch.BodyFetchedEvent{PRDiscoveryID: "pr-no-ticker", Headline: "Mystery Announcement", Body: string(body)}
	if _, err := bodyStore.Append(context.Background(), mustEvent(t, "pr_body_fetched:pr-no-ticker", "pr_body_fetched", ev)); err != nil {
		t.Fatalf("append body event: %v", err)
	}

	runBatch(context.Background(), bodyStore, outStore, map[string]string{}, testLogger(), batchConfig{
		cursorPath: t.TempDir() + "/.cursor",
		batchSize:  256,
		cursor:     1,
	})

	recs, err := outStore.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("read out store: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("out store has %d records, want 0 (no ticker -> skip)", len(recs))
	}
}

// TestRunBatch_SkipsShortBody covers the same "junk in, nothing out" guard
// dividend/guidance-watcher already share (< 200 chars is treated as not a
// real body -- often a paywall/loading placeholder, not real content).
func TestRunBatch_SkipsShortBody(t *testing.T) {
	bodyStore, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new body store: %v", err)
	}
	defer bodyStore.Close()

	outStore, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new out store: %v", err)
	}
	defer outStore.Close()

	ev := prwatch.BodyFetchedEvent{PRDiscoveryID: "pr-short", Headline: "Too Short", Body: "short"}
	if _, err := bodyStore.Append(context.Background(), mustEvent(t, "pr_body_fetched:pr-short", "pr_body_fetched", ev)); err != nil {
		t.Fatalf("append body event: %v", err)
	}

	runBatch(context.Background(), bodyStore, outStore, map[string]string{"pr-short": "ACME"}, testLogger(), batchConfig{
		cursorPath: t.TempDir() + "/.cursor",
		batchSize:  256,
		cursor:     1,
	})

	recs, err := outStore.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("read out store: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("out store has %d records, want 0 (short body -> skip)", len(recs))
	}
}
