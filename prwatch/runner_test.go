package prwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// S160-01 (EMILY/BACKLOG.md): discoverTickers was silently returning empty on live discovery --
// the same URLs, fetched moments later by prwatch-body's separate crawler, contained real ticker
// text. Leading hypothesis: a timing race, discovery fires before the page's ticker text is
// reliably live. These tests exercise the bounded-retry fix directly, using a fake server rather
// than real network traffic (which can't be reproduced on demand in a test).

func withFastRetryDelay(t *testing.T) {
	old := discoverTickerRetryDelay
	discoverTickerRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { discoverTickerRetryDelay = old })
}

func TestDiscoverTickers_SucceedsOnFirstFetch(t *testing.T) {
	withFastRetryDelay(t)
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Write([]byte(`<html><body>Shares trade under (NASDAQ: ABCD).</body></html>`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{HTTPClient: srv.Client()})

	refs, _ := discoverTickers(context.Background(), c, &testLogger{t}, srv.URL)

	if len(refs) != 1 || refs[0].Symbol != "ABCD" {
		t.Fatalf("expected exactly one ABCD ref, got %+v", refs)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("expected exactly 1 request when the first fetch already has tickers, got %d", got)
	}
}

func TestDiscoverTickers_RetriesOnceOnEmptyFirstFetch(t *testing.T) {
	withFastRetryDelay(t)
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		if n == 1 {
			w.Write([]byte(`<html><body>Page still warming up, no ticker text yet.</body></html>`))
			return
		}
		w.Write([]byte(`<html><body>Shares trade under (NYSE: ZTS).</body></html>`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{HTTPClient: srv.Client()})

	refs, _ := discoverTickers(context.Background(), c, &testLogger{t}, srv.URL)

	if len(refs) != 1 || refs[0].Symbol != "ZTS" {
		t.Fatalf("expected the retry to pick up ZTS once the page has ticker text, got %+v", refs)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("expected exactly 2 requests (first fetch + one retry), got %d", got)
	}
}

func TestDiscoverTickers_GivesUpAfterOneRetryStillEmpty(t *testing.T) {
	withFastRetryDelay(t)
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Write([]byte(`<html><body>Never has any ticker text at all.</body></html>`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{HTTPClient: srv.Client()})

	refs, _ := discoverTickers(context.Background(), c, &testLogger{t}, srv.URL)

	if len(refs) != 0 {
		t.Fatalf("expected no refs when the page genuinely never has ticker text, got %+v", refs)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("expected exactly 2 requests (first fetch + one retry, then give up), got %d", got)
	}
}

func TestDiscoverTickers_NoRetryOnFetchFailure(t *testing.T) {
	withFastRetryDelay(t)
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{HTTPClient: srv.Client()})

	refs, _ := discoverTickers(context.Background(), c, &testLogger{t}, srv.URL)

	if len(refs) != 0 {
		t.Fatalf("expected no refs on a fetch failure, got %+v", refs)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("a genuine fetch failure (non-200) should not retry -- retrying won't fix a broken fetch, got %d requests", got)
	}
}

func TestDiscoverTickers_NilLoggerDoesNotPanic(t *testing.T) {
	withFastRetryDelay(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>(NASDAQ: ABCD)</body></html>`))
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{HTTPClient: srv.Client()})

	refs, _ := discoverTickers(context.Background(), c, nil, srv.URL)

	if len(refs) != 1 {
		t.Fatalf("expected discoverTickers to work fine with a nil logger, got %+v", refs)
	}
}
