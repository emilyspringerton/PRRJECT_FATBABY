package prwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
	identitypkg "github.com/example/prrject-fatbaby/internal/identity"
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

func TestMintSkuldmarkIDs_MintsForWatchlistedTicker(t *testing.T) {
	refs := []identitypkg.SecurityRef{
		{Exchange: "NASDAQ", Symbol: "AAPL", Source: "regex", Confidence: 0.91},
	}
	watchlist := map[string]WatchlistRef{
		"AAPL": {CIK: "320193", Exchange: "Nasdaq"},
	}
	mintSkuldmarkIDs(refs, watchlist, nil)

	want := "EINXNASAAPLXXX0000320193Y"
	if refs[0].SkuldmarkID != want {
		t.Errorf("SkuldmarkID = %q, want %q", refs[0].SkuldmarkID, want)
	}
	if refs[0].CIK != "320193" {
		t.Errorf("CIK = %q, want %q (should be filled from watchlist)", refs[0].CIK, "320193")
	}
}

func TestMintSkuldmarkIDs_LeavesUnmintedWhenTickerNotOnWatchlist(t *testing.T) {
	refs := []identitypkg.SecurityRef{
		{Exchange: "NASDAQ", Symbol: "SOMERANDOMCO", Source: "regex", Confidence: 0.91},
	}
	watchlist := map[string]WatchlistRef{
		"AAPL": {CIK: "320193", Exchange: "Nasdaq"},
	}
	mintSkuldmarkIDs(refs, watchlist, nil)

	if refs[0].SkuldmarkID != "" {
		t.Errorf("expected no SkuldmarkID for a ticker not on the watchlist, got %q", refs[0].SkuldmarkID)
	}
}

func TestMintSkuldmarkIDs_NilWatchlistIsNoOp(t *testing.T) {
	refs := []identitypkg.SecurityRef{
		{Exchange: "NASDAQ", Symbol: "AAPL", Source: "regex", Confidence: 0.91},
	}
	mintSkuldmarkIDs(refs, nil, nil)

	if refs[0].SkuldmarkID != "" {
		t.Errorf("expected no-op with a nil watchlist, got SkuldmarkID %q", refs[0].SkuldmarkID)
	}
}

// FB-12343 ("as soon as we tickerize a press release we want to publish a signal for TICKER
// mentioned in a press release") -- tickerMentionSignals is the real, narrow function this
// feature lives in: only a ref with a real, already-minted SkuldmarkID (a watched ticker) gets a
// signal, matching mintSkuldmarkIDs' own "unminted record is honest, not a guess" discipline.

func TestTickerMentionSignals_WatchedTickerProducesASignal(t *testing.T) {
	e := PressReleaseDiscovered{
		Headline: "Example Corp announces real news",
		Identity: identitypkg.DiscoveryIdentity{
			AllTickers: []identitypkg.SecurityRef{
				{Symbol: "AAPL", Exchange: "NASDAQ", CIK: "320193", SkuldmarkID: "EINXNASAAPLXXX0000320193Y", Confidence: 0.91},
			},
		},
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	sigs := tickerMentionSignals(e, "https://example.com/pr/123", now)

	if len(sigs) != 1 {
		t.Fatalf("expected exactly 1 signal, got %d", len(sigs))
	}
	sig := sigs[0]
	if sig.Type != entitygraph.SignalTickerMentionedInPR {
		t.Errorf("Type = %q, want %q", sig.Type, entitygraph.SignalTickerMentionedInPR)
	}
	if sig.Ticker != "AAPL" {
		t.Errorf("Ticker = %q, want AAPL", sig.Ticker)
	}
	if sig.Severity != entitygraph.SeverityLow {
		t.Errorf("Severity = %q, want low -- a mention alone is not an investment-relevant event", sig.Severity)
	}
	// ref.Confidence is a real float32 (identitypkg.SecurityRef's own field type); comparing
	// against float64(float32(0.91)), not the float64 literal 0.91, since the float32->float64
	// widening doesn't produce an exact 0.91 (a real, well-known float precision fact, not a bug
	// in tickerMentionSignals -- confirmed by converting the same way here as the real code does).
	if want := float64(float32(0.91)); sig.Confidence != want {
		t.Errorf("Confidence = %v, want %v (carried through from the real ref)", sig.Confidence, want)
	}
	if sig.Metadata["source_url"] != "https://example.com/pr/123" {
		t.Errorf("Metadata[source_url] = %q, want the real PR URL", sig.Metadata["source_url"])
	}
	if sig.Metadata["skuldmark_id"] != "EINXNASAAPLXXX0000320193Y" {
		t.Errorf("Metadata[skuldmark_id] missing/wrong: %q", sig.Metadata["skuldmark_id"])
	}
}

func TestTickerMentionSignals_UnwatchedTickerProducesNoSignal(t *testing.T) {
	e := PressReleaseDiscovered{
		Identity: identitypkg.DiscoveryIdentity{
			AllTickers: []identitypkg.SecurityRef{
				{Symbol: "SOMERANDOMCO", Exchange: "NASDAQ", Confidence: 0.7}, // no SkuldmarkID -- not on the watchlist
			},
		},
	}
	sigs := tickerMentionSignals(e, "https://example.com/pr/456", time.Now())
	if len(sigs) != 0 {
		t.Fatalf("expected zero signals for an unwatched ticker, got %d", len(sigs))
	}
}

func TestTickerMentionSignals_NoTickersProducesNoSignal(t *testing.T) {
	sigs := tickerMentionSignals(PressReleaseDiscovered{}, "https://example.com/pr/789", time.Now())
	if len(sigs) != 0 {
		t.Fatalf("expected zero signals when no tickers were identified at all, got %d", len(sigs))
	}
}

func TestTickerMentionSignals_MultipleWatchedTickersEachGetTheirOwnSignal(t *testing.T) {
	e := PressReleaseDiscovered{
		Identity: identitypkg.DiscoveryIdentity{
			AllTickers: []identitypkg.SecurityRef{
				{Symbol: "AAPL", SkuldmarkID: "EINXNASAAPLXXX0000320193Y", Confidence: 0.9},
				{Symbol: "MSFT", SkuldmarkID: "EINXNASMSFTXXX0000789019Z", Confidence: 0.85},
			},
		},
	}
	sigs := tickerMentionSignals(e, "https://example.com/pr/999", time.Now())
	if len(sigs) != 2 {
		t.Fatalf("expected 2 signals (one per watched ticker), got %d", len(sigs))
	}
	if sigs[0].SignalID == sigs[1].SignalID {
		t.Error("expected two distinct SignalIDs for two different tickers")
	}
}
