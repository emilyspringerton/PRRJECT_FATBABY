package replay

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/secwatch"
)

// ── stubs ────────────────────────────────────────────────────────────────────

type memStore struct {
	records []eventstore.Record
}

func (m *memStore) Append(ctx context.Context, events ...eventstore.Event) ([]eventstore.Record, error) {
	var out []eventstore.Record
	for _, ev := range events {
		rec := eventstore.Record{
			Sequence:   uint64(len(m.records)),
			Event:      ev,
			AppendedAt: time.Now(),
		}
		m.records = append(m.records, rec)
		out = append(out, rec)
	}
	return out, nil
}

func (m *memStore) ReadFrom(_ context.Context, fromSeq uint64, limit int) ([]eventstore.Record, error) {
	var out []eventstore.Record
	for _, r := range m.records {
		if r.Sequence >= fromSeq {
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *memStore) LatestSequence(_ context.Context) (uint64, error) {
	if len(m.records) == 0 {
		return 0, nil
	}
	return m.records[len(m.records)-1].Sequence, nil
}

func (m *memStore) Close() error { return nil }

type stubProvider struct {
	signal *intelligence.Signal
	err    error
}

func (s *stubProvider) AnalyzeText(_ context.Context, _ string) (*intelligence.Signal, error) {
	return s.signal, s.err
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeFilingEvent(ticker, form string, ts time.Time) eventstore.Event {
	filing := secwatch.FilingDiscoveredEvent{
		Ticker:          ticker,
		Form:            form,
		AccessionNumber: "000-" + ticker,
	}
	data, _ := json.Marshal(filing)
	return eventstore.Event{
		ID:          "e-" + ticker,
		Type:        "filing_discovered",
		Source:      "test",
		OccurredAt:  ts,
		IngestedAt:  ts,
		Data:        data,
	}
}

func makeOtherEvent(ts time.Time) eventstore.Event {
	return eventstore.Event{
		ID:         "other-1",
		Type:       "signal_generated",
		Source:     "test",
		OccurredAt: ts,
		IngestedAt: ts,
		Data:       json.RawMessage(`{"x":1}`),
	}
}

var okSignal = &intelligence.Signal{Ticker: "AAPL", SignalType: "Earnings", Importance: 8}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestReplayEmptyStore(t *testing.T) {
	store := &memStore{}
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	stats, err := e.Replay(context.Background(), time.Now().Add(-24*time.Hour), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Total != 0 {
		t.Errorf("expected 0 total, got %d", stats.Total)
	}
}

func TestReplaySkipsNonFilingEvents(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(), makeOtherEvent(now))
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	stats, _ := e.Replay(context.Background(), now.Add(-time.Minute), now.Add(time.Minute))
	if stats.Total != 0 {
		t.Errorf("non-filing event should not count as total, got %d", stats.Total)
	}
	if stats.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", stats.Skipped)
	}
}

func TestReplaySkipsOutOfWindow(t *testing.T) {
	store := &memStore{}
	old := time.Now().Add(-48 * time.Hour)
	store.Append(context.Background(), makeFilingEvent("AAPL", "8-K", old))
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	// Window is yesterday onwards.
	from := time.Now().Add(-24 * time.Hour)
	stats, _ := e.Replay(context.Background(), from, time.Now())
	if stats.Total != 0 {
		t.Errorf("out-of-window event should not count as total, got %d", stats.Total)
	}
	if stats.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", stats.Skipped)
	}
}

func TestReplaySingleSuccess(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(), makeFilingEvent("AAPL", "8-K", now))
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	stats, err := e.Replay(context.Background(), now.Add(-time.Second), now.Add(time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Total != 1 || stats.Success != 1 || stats.Failed != 0 {
		t.Errorf("expected 1 success, got %+v", stats)
	}
}

func TestReplayMultipleEvents(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(),
		makeFilingEvent("AAPL", "8-K", now),
		makeFilingEvent("MSFT", "10-K", now),
		makeFilingEvent("GOOG", "DEF14A", now),
	)
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	stats, _ := e.Replay(context.Background(), now.Add(-time.Second), now.Add(time.Second))
	if stats.Total != 3 || stats.Success != 3 {
		t.Errorf("expected 3 success, got %+v", stats)
	}
}

func TestReplayProviderError(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(), makeFilingEvent("ERR", "8-K", now))
	provErr := errors.New("provider down")
	e := New(store, &stubProvider{err: provErr}, 10, nil)
	stats, _ := e.Replay(context.Background(), now.Add(-time.Second), now.Add(time.Second))
	if stats.Failed != 1 || stats.Success != 0 {
		t.Errorf("expected 1 failed, got %+v", stats)
	}
}

func TestReplayResultContainsTicker(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(), makeFilingEvent("NVDA", "8-K", now))
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var results []Result
	done := make(chan struct{})
	go func() {
		for r := range e.Out {
			results = append(results, r)
		}
		close(done)
	}()
	e.Replay(ctx, now.Add(-time.Second), now.Add(time.Second))
	<-done
	if len(results) != 1 || results[0].Ticker != "NVDA" {
		t.Errorf("expected NVDA result, got %v", results)
	}
}

func TestReplayResultContainsForm(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(), makeFilingEvent("TSLA", "10-K", now))
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	ctx := context.Background()
	var results []Result
	done := make(chan struct{})
	go func() {
		for r := range e.Out {
			results = append(results, r)
		}
		close(done)
	}()
	e.Replay(ctx, now.Add(-time.Second), now.Add(time.Second))
	<-done
	if len(results) == 0 || results[0].Form != "10-K" {
		t.Errorf("expected 10-K form, got %v", results)
	}
}

func TestReplayMixedSuccessAndFailure(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(),
		makeFilingEvent("OK1", "8-K", now),
		makeFilingEvent("OK2", "8-K", now),
	)
	callCount := 0
	prov := &callCountProvider{callCount: &callCount, signal: okSignal, failOnCall: 2}
	e := New(store, prov, 10, nil)
	stats, _ := e.Replay(context.Background(), now.Add(-time.Second), now.Add(time.Second))
	if stats.Total != 2 {
		t.Errorf("expected 2 total, got %d", stats.Total)
	}
	if stats.Success != 1 || stats.Failed != 1 {
		t.Errorf("expected 1 success 1 failure, got %+v", stats)
	}
}

func TestReplayCancelledContext(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(), makeFilingEvent("AAPL", "8-K", now))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	_, err := e.Replay(ctx, now.Add(-time.Second), now.Add(time.Second))
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestReplayDurationPositive(t *testing.T) {
	store := &memStore{}
	now := time.Now()
	store.Append(context.Background(), makeFilingEvent("AAPL", "8-K", now))
	e := New(store, &stubProvider{signal: okSignal}, 10, nil)
	stats, _ := e.Replay(context.Background(), now.Add(-time.Second), now.Add(time.Second))
	if stats.Duration <= 0 {
		t.Error("duration should be positive")
	}
}

// callCountProvider fails on the Nth call.
type callCountProvider struct {
	callCount  *int
	signal     *intelligence.Signal
	failOnCall int
}

func (p *callCountProvider) AnalyzeText(_ context.Context, _ string) (*intelligence.Signal, error) {
	*p.callCount++
	if *p.callCount == p.failOnCall {
		return nil, errors.New("forced failure")
	}
	return p.signal, nil
}
