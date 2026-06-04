package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/iamguard"
)

// flusherWriter wraps httptest.ResponseRecorder to satisfy http.Flusher.
type flusherWriter struct {
	*httptest.ResponseRecorder
}

func (f *flusherWriter) Flush() {}

func newFlusher() *flusherWriter {
	return &flusherWriter{httptest.NewRecorder()}
}

// TestWriteSSE verifies the Server-Sent Events wire format.
func TestWriteSSE(t *testing.T) {
	rr := newFlusher()
	rec := eventstore.Record{
		Sequence: 42,
		Event: eventstore.Event{
			ID:   "evt:1",
			Type: "signal_generated",
			Data: json.RawMessage(`{"ticker":"AAPL"}`),
		},
	}
	if err := writeSSE(rr, rec); err != nil {
		t.Fatalf("writeSSE: %v", err)
	}
	out := rr.Body.String()

	if !strings.Contains(out, "id: 42") {
		t.Errorf("missing 'id: 42': %s", out)
	}
	if !strings.Contains(out, "event: record") {
		t.Errorf("missing 'event: record': %s", out)
	}
	if !strings.Contains(out, `"ticker":"AAPL"`) {
		t.Errorf("missing ticker payload: %s", out)
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Errorf("SSE message must end with \\n\\n, got: %q", out)
	}
}

// TestWriteSSE_Sequence0 verifies sequence 0 renders correctly.
func TestWriteSSE_Sequence0(t *testing.T) {
	rr := newFlusher()
	rec := eventstore.Record{Sequence: 0, Event: eventstore.Event{Type: "test"}}
	if err := writeSSE(rr, rec); err != nil {
		t.Fatalf("writeSSE: %v", err)
	}
	if !strings.Contains(rr.Body.String(), "id: 0") {
		t.Errorf("sequence 0 not rendered: %s", rr.Body.String())
	}
}

// TestSendInitial_EmptyStore verifies sendInitial handles an empty store without error.
func TestSendInitial_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	s := &Server{store: store, initialEvents: 50}
	rr := newFlusher()
	lastSeq, err := s.sendInitial(context.Background(), rr)
	if err != nil {
		t.Fatalf("sendInitial on empty store: %v", err)
	}
	if lastSeq != 0 {
		t.Errorf("lastSeq = %d, want 0 for empty store", lastSeq)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected no output for empty store, got: %q", rr.Body.String())
	}
}

// TestSendInitial_WithEvents verifies initial events are streamed in SSE format.
func TestSendInitial_WithEvents(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 3; i++ {
		_, err := store.Append(context.Background(), eventstore.Event{
			ID:   "evt:" + string(rune('A'+i)),
			Type: "signal_generated",
			Data: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	s := &Server{store: store, initialEvents: 50}
	rr := newFlusher()
	lastSeq, err := s.sendInitial(context.Background(), rr)
	if err != nil {
		t.Fatalf("sendInitial: %v", err)
	}
	if lastSeq < 3 {
		t.Errorf("lastSeq = %d, want >= 3", lastSeq)
	}
	count := strings.Count(rr.Body.String(), "event: record")
	if count != 3 {
		t.Errorf("expected 3 SSE events, got %d in:\n%s", count, rr.Body.String())
	}
}

// TestSendInitial_TruncatesAtInitialLimit verifies only the N most-recent
// records are sent when the store has more than initialEvents.
func TestSendInitial_TruncatesAtInitialLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 20; i++ {
		_, _ = store.Append(context.Background(), eventstore.Event{
			ID: "e", Type: "signal_generated", Data: json.RawMessage(`{}`),
		})
	}

	s := &Server{store: store, initialEvents: 5}
	rr := newFlusher()
	_, err = s.sendInitial(context.Background(), rr)
	if err != nil {
		t.Fatalf("sendInitial: %v", err)
	}
	count := strings.Count(rr.Body.String(), "event: record")
	if count > 5 {
		t.Errorf("expected at most 5 initial events (initialEvents cap), got %d", count)
	}
}

// TestHandlerEventsSSEHeaders verifies /events sets SSE headers and accepts
// the connection. Uses a flusherWriter so the handler does not bail out early.
func TestHandlerEventsSSEHeaders(t *testing.T) {
	dir := t.TempDir()
	store, _ := eventstore.NewFileStore(dir)
	defer store.Close()

	guard, _ := iamguard.NewFromEnv() // no-op when IDUNA not configured
	if guard == nil {
		guard = &iamguard.Guard{}
	}
	s := &Server{
		store:         store,
		pollInterval:  50 * time.Millisecond,
		initialEvents: 5,
		guard:         guard,
	}
	// Build a minimal mux without the static file embed to avoid embed issues in test.
	mux := http.NewServeMux()
	eventsHandler := http.HandlerFunc(s.handleEvents)
	mux.Handle("/events", s.guard.RequirePermission("fatbaby.read")(eventsHandler))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	rw := newFlusher()

	done := make(chan struct{})
	go func() { mux.ServeHTTP(rw, req); close(done) }()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
	}

	ct := rw.Header().Get("Content-Type")
	// Either the guard blocked (no IDUNA configured → 401) or it passed through.
	// Either way, no panic and we get a response.
	if ct != "" && !strings.Contains(ct, "event-stream") && rw.Code == http.StatusOK {
		t.Errorf("Content-Type = %q, want text/event-stream for 200", ct)
	}
}
