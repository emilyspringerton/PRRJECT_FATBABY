package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/signalindex"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func add(t *testing.T, idx *signalindex.Index, seq uint64, ticker, typ string, imp int, ts time.Time) {
	t.Helper()
	s := intelligence.Signal{ID: fmt.Sprintf("id-%d", seq), Ticker: ticker, Timestamp: ts, SignalType: typ, Importance: imp}
	b, _ := json.Marshal(s)
	if err := idx.Ingest(eventstore.Record{Sequence: seq, AppendedAt: ts, Event: eventstore.Event{Type: "signal_generated", PartitionKey: "1:abc", Data: b}}); err != nil {
		t.Fatal(err)
	}
}
func req(s *http.Server, path, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, r)
	return w
}

func TestHandleSignalsByTicker_OK(t *testing.T) {
	idx := signalindex.NewIndex()
	now := time.Now()
	add(t, idx, 1, "AAPL", "Earnings", 1, now)
	add(t, idx, 2, "AAPL", "Earnings", 1, now.Add(time.Hour))
	add(t, idx, 3, "AAPL", "Earnings", 1, now.Add(2*time.Hour))
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/signals/AAPL", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestHandleSignalsByTicker_LimitParam(t *testing.T) {
	idx := signalindex.NewIndex()
	n := time.Now()
	for i := 0; i < 5; i++ {
		add(t, idx, uint64(i+1), "AAPL", "Earnings", 1, n.Add(time.Duration(i)*time.Minute))
	}
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/signals/AAPL?limit=2", "")
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["count"].(float64)) != 2 {
		t.Fatal("count")
	}
}
func TestHandleSignalsByTicker_FilterSignalType(t *testing.T) {
	idx := signalindex.NewIndex()
	n := time.Now()
	add(t, idx, 1, "AAPL", "Earnings", 1, n)
	add(t, idx, 2, "AAPL", "Legal", 1, n)
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/signals/AAPL?signal_type=earnings", "")
	var b map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	if int(b["count"].(float64)) != 1 {
		t.Fatal("count")
	}
}
func TestHandleSignalsByTicker_MinImportance(t *testing.T) {
	idx := signalindex.NewIndex()
	n := time.Now()
	add(t, idx, 1, "AAPL", "E", 3, n)
	add(t, idx, 2, "AAPL", "E", 6, n)
	add(t, idx, 3, "AAPL", "E", 9, n)
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/signals/AAPL?min_importance=6", "")
	var b map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	if int(b["count"].(float64)) != 2 {
		t.Fatal("count")
	}
}
func TestHandleSignalsByTicker_FromParam(t *testing.T) {
	idx := signalindex.NewIndex()
	n := time.Now()
	add(t, idx, 1, "AAPL", "E", 1, n.Add(-2*time.Hour))
	add(t, idx, 2, "AAPL", "E", 1, n.Add(-time.Hour))
	add(t, idx, 3, "AAPL", "E", 1, n)
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/signals/AAPL?from="+n.Add(-90*time.Minute).UTC().Format(time.RFC3339), "")
	var b map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &b)
	if int(b["count"].(float64)) != 2 {
		t.Fatal("count")
	}
}
func TestHandleSignalsByTicker_NotFound(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex()})
	w := req(s, "/v1/signals/ZZZZ", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestHandleLatestSignal_OK(t *testing.T) {
	idx := signalindex.NewIndex()
	n := time.Now()
	add(t, idx, 1, "AAPL", "E", 1, n)
	add(t, idx, 2, "AAPL", "L", 1, n.Add(time.Hour))
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/signals/AAPL/latest", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestHandleSummary_OK(t *testing.T) {
	idx := signalindex.NewIndex()
	n := time.Now()
	add(t, idx, 1, "AAPL", "E", 1, n)
	add(t, idx, 2, "AAPL", "E", 1, n)
	add(t, idx, 3, "MSFT", "E", 1, n)
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/signals", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestHandleHealth_OK(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex()})
	w := req(s, "/v1/health", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestAuth_Missing(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex(), APIKeys: []string{"k"}})
	w := req(s, "/v1/health", "")
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestAuth_Valid(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex(), APIKeys: []string{"k"}})
	w := req(s, "/v1/health", "Bearer k")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestAuth_Disabled(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex()})
	w := req(s, "/v1/health", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestHandleDataQuality_Empty(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex()})
	w := req(s, "/v1/data-quality", "")
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if int(body["tickers"].(float64)) != 0 {
		t.Errorf("want 0 tickers, got %v", body["tickers"])
	}
}

func TestHandleDataQuality_WithData(t *testing.T) {
	idx := signalindex.NewIndex()
	n := time.Now()
	// Add signals for two tickers.
	add(t, idx, 1, "AAPL", "eps_beat", 8, n)
	add(t, idx, 2, "AAPL", "guidance_raise", 7, n.Add(time.Hour))
	add(t, idx, 3, "MSFT", "dividend_raise", 6, n)
	s := New(ServerConfig{Index: idx})
	w := req(s, "/v1/data-quality", "")
	if w.Code != 200 {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if int(body["tickers"].(float64)) != 2 {
		t.Errorf("want 2 tickers, got %v", body["tickers"])
	}
	if int(body["total_signals"].(float64)) != 3 {
		t.Errorf("want 3 total signals, got %v", body["total_signals"])
	}
	if body["overall_confidence"] == nil {
		t.Error("missing overall_confidence")
	}
	if body["grade_distribution"] == nil {
		t.Error("missing grade_distribution")
	}
}

func TestHandleDataQuality_Auth(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex(), APIKeys: []string{"key1"}})
	w := req(s, "/v1/data-quality", "")
	if w.Code != 401 {
		t.Fatalf("want 401 without auth, got %d", w.Code)
	}
	w = req(s, "/v1/data-quality", "Bearer key1")
	if w.Code != 200 {
		t.Fatalf("want 200 with auth, got %d", w.Code)
	}
}
