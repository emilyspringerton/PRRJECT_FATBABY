package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

// secBaseURLForTest points secCompanyConceptBaseURL at a test server for the
// duration of one test, returning a restore func. Mirrors internal/movers'
// screenerBaseURLForTest helper.
func secBaseURLForTest(url string) func() {
	orig := secCompanyConceptBaseURL
	secCompanyConceptBaseURL = url
	return func() { secCompanyConceptBaseURL = orig }
}

func TestNormalizeCIKForSEC(t *testing.T) {
	cases := []struct{ in, want string }{
		{"320193", "0000320193"},
		{"0000320193", "0000320193"},
		{"  320193  ", "0000320193"},
		{"", ""},
		{"CIK0000320193", "0000320193"},
	}
	for _, tc := range cases {
		got := normalizeCIKForSEC(tc.in)
		if got != tc.want {
			t.Errorf("normalizeCIKForSEC(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSharesCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aapl.shares.json")

	if _, err := loadSharesCache(path); err == nil {
		t.Fatal("expected error loading a cache file that doesn't exist yet")
	}

	want := sharesCache{SharesOutstanding: 14_594_180_000, AsOfDate: "2026-07-17", FetchedAt: time.Now().UTC().Truncate(time.Second)}
	if err := saveSharesCache(path, want); err != nil {
		t.Fatalf("saveSharesCache: %v", err)
	}

	got, err := loadSharesCache(path)
	if err != nil {
		t.Fatalf("loadSharesCache: %v", err)
	}
	if got.SharesOutstanding != want.SharesOutstanding || got.AsOfDate != want.AsOfDate || !got.FetchedAt.Equal(want.FetchedAt) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", *got, want)
	}
}

func fakeSECConceptServer(t *testing.T, shares []struct {
	End string
	Val int64
}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("request missing User-Agent header (SEC requires a descriptive UA)")
		}
		resp := secCompanyConceptResponse{}
		for _, s := range shares {
			resp.Units.Shares = append(resp.Units.Shares, struct {
				End string `json:"end"`
				Val int64  `json:"val"`
			}{End: s.End, Val: s.Val})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestFetchSharesOutstandingSEC_ReturnsLatestEntry(t *testing.T) {
	ts := fakeSECConceptServer(t, []struct {
		End string
		Val int64
	}{
		{End: "2026-01-16", Val: 14_681_140_000},
		{End: "2026-04-17", Val: 14_687_356_000},
		{End: "2026-07-17", Val: 14_594_180_000}, // most recent -- must win
	})
	defer ts.Close()
	defer secBaseURLForTest(ts.URL)()

	val, asOf, err := fetchSharesOutstandingSEC(context.Background(), ts.Client(), "0000320193")
	if err != nil {
		t.Fatalf("fetchSharesOutstandingSEC: %v", err)
	}
	if val != 14_594_180_000 {
		t.Errorf("val = %d, want the last (most recent) entry 14594180000", val)
	}
	if asOf != "2026-07-17" {
		t.Errorf("asOf = %q, want 2026-07-17", asOf)
	}
}

func TestFetchSharesOutstandingSEC_EmptyCIK(t *testing.T) {
	if _, _, err := fetchSharesOutstandingSEC(context.Background(), http.DefaultClient, ""); err == nil {
		t.Error("expected error for empty CIK")
	}
}

func TestFetchSharesOutstandingSEC_NoFacts(t *testing.T) {
	ts := fakeSECConceptServer(t, nil)
	defer ts.Close()
	defer secBaseURLForTest(ts.URL)()

	if _, _, err := fetchSharesOutstandingSEC(context.Background(), ts.Client(), "0000320193"); err == nil {
		t.Error("expected error when SEC returns no shares facts")
	}
}

func TestRefreshSharesOutstanding_UsesFreshCacheWithoutNetworkCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aapl.shares.json")
	if err := saveSharesCache(path, sharesCache{SharesOutstanding: 999, FetchedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"units":{"shares":[{"end":"2026-01-01","val":1}]}}`))
	}))
	defer ts.Close()
	defer secBaseURLForTest(ts.URL)()

	got := refreshSharesOutstanding(context.Background(), ts.Client(), log.Default(), "AAPL", "320193", path, 7*24*time.Hour, false)
	if got != 999 {
		t.Errorf("got %d, want cached value 999", got)
	}
	if called {
		t.Error("refreshSharesOutstanding hit the network despite a fresh cache -- pacing guarantee broken")
	}
}

func TestRefreshSharesOutstanding_RefetchesWhenStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aapl.shares.json")
	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if err := saveSharesCache(path, sharesCache{SharesOutstanding: 999, FetchedAt: stale}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"units":{"shares":[{"end":"2026-07-17","val":14594180000}]}}`))
	}))
	defer ts.Close()
	defer secBaseURLForTest(ts.URL)()

	got := refreshSharesOutstanding(context.Background(), ts.Client(), log.Default(), "AAPL", "320193", path, 7*24*time.Hour, false)
	if got != 14_594_180_000 {
		t.Errorf("got %d, want fresh SEC value 14594180000", got)
	}

	// The refreshed value should now be persisted to disk.
	cached, err := loadSharesCache(path)
	if err != nil {
		t.Fatalf("loadSharesCache after refresh: %v", err)
	}
	if cached.SharesOutstanding != 14_594_180_000 {
		t.Errorf("cache not updated: got %d", cached.SharesOutstanding)
	}
}

func TestRefreshSharesOutstanding_NoCIKReturnsCachedOrZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nocik.shares.json")

	// No cache, no CIK: must return 0 without attempting a network call.
	got := refreshSharesOutstanding(context.Background(), http.DefaultClient, log.Default(), "NOCIK", "", path, 7*24*time.Hour, false)
	if got != 0 {
		t.Errorf("got %d, want 0 for ticker with no CIK and no cache", got)
	}
}

func TestRefreshSharesOutstanding_SECErrorFallsBackToCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aapl.shares.json")
	stale := time.Now().UTC().Add(-30 * 24 * time.Hour)
	if err := saveSharesCache(path, sharesCache{SharesOutstanding: 777, FetchedAt: stale}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	defer secBaseURLForTest(ts.URL)()

	got := refreshSharesOutstanding(context.Background(), ts.Client(), log.Default(), "AAPL", "320193", path, 7*24*time.Hour, false)
	if got != 777 {
		t.Errorf("got %d, want stale-but-known cached value 777 on SEC failure", got)
	}
}

func TestRefreshSharesOutstanding_DryRunDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aapl.shares.json")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"units":{"shares":[{"end":"2026-07-17","val":14594180000}]}}`))
	}))
	defer ts.Close()
	defer secBaseURLForTest(ts.URL)()

	got := refreshSharesOutstanding(context.Background(), ts.Client(), log.Default(), "AAPL", "320193", path, 7*24*time.Hour, true /* dryRun */)
	if got != 14_594_180_000 {
		t.Errorf("got %d, want SEC value returned even in dry-run", got)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("dry-run should not write a shares cache file to disk")
	}
}

// yahooBaseURLForTest points yahooBaseURL at a test server for the duration
// of one test, returning a restore func. Mirrors secBaseURLForTest.
func yahooBaseURLForTest(url string) func() {
	orig := yahooBaseURL
	yahooBaseURL = url
	return func() { yahooBaseURL = orig }
}

// fakeYahooChartServer returns one OHLCV bar (date/close as given) shaped
// exactly like Yahoo's real v8/finance/chart payload.
func fakeYahooChartServer(t *testing.T, date string, closePrice float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Fatalf("bad test date %q: %v", date, err)
		}

		close_ := closePrice
		vol := int64(1000)
		resp := yahooChartResponse{}
		resp.Chart.Result = []struct {
			Meta struct {
				Symbol string `json:"symbol"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*int64   `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []*float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		}{{}}
		resp.Chart.Result[0].Meta.Symbol = "AAPL"
		resp.Chart.Result[0].Timestamp = []int64{ts.Unix()}
		resp.Chart.Result[0].Indicators.Quote = []struct {
			Open   []*float64 `json:"open"`
			High   []*float64 `json:"high"`
			Low    []*float64 `json:"low"`
			Close  []*float64 `json:"close"`
			Volume []*int64   `json:"volume"`
		}{{Open: []*float64{&close_}, High: []*float64{&close_}, Low: []*float64{&close_}, Close: []*float64{&close_}, Volume: []*int64{&vol}}}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestFetchTicker_ComputesMarketCapFromPriceAndSharesCache(t *testing.T) {
	dir := t.TempDir()

	yahoo := fakeYahooChartServer(t, "2026-08-01", 300.0)
	defer yahoo.Close()
	defer yahooBaseURLForTest(yahoo.URL)()

	sec := fakeSECConceptServer(t, []struct {
		End string
		Val int64
	}{{End: "2026-07-17", Val: 14_594_180_000}})
	defer sec.Close()
	defer secBaseURLForTest(sec.URL)()

	storeDir := t.TempDir()
	store, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("open eventstore: %v", err)
	}
	defer store.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	n, err := fetchTicker(context.Background(), client, log.Default(), store,
		"AAPL", "320193", dir, "1y", 7*24*time.Hour, false)
	if err != nil {
		t.Fatalf("fetchTicker: %v", err)
	}
	if n != 1 {
		t.Fatalf("fetchTicker emitted %d events, want 1", n)
	}

	recs, err := store.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	var bar OHLCVBar
	if err := json.Unmarshal(recs[0].Event.Data, &bar); err != nil {
		t.Fatalf("unmarshal bar: %v", err)
	}
	wantMC := int64(300.0 * 14_594_180_000)
	if bar.MarketCap != wantMC {
		t.Errorf("MarketCap = %d, want %d (close=300 * shares=14594180000)", bar.MarketCap, wantMC)
	}

	// The shares cache should now exist on disk for next cycle's freshness check.
	sharesPath := filepath.Join(dir, cursorKey("AAPL")+".shares.json")
	if _, err := os.Stat(sharesPath); err != nil {
		t.Errorf("expected shares cache file at %s: %v", sharesPath, err)
	}
}

func TestFetchTicker_NoCIKMeansNoMarketCap(t *testing.T) {
	dir := t.TempDir()

	yahoo := fakeYahooChartServer(t, "2026-08-01", 300.0)
	defer yahoo.Close()
	defer yahooBaseURLForTest(yahoo.URL)()

	storeDir := t.TempDir()
	store, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("open eventstore: %v", err)
	}
	defer store.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	n, err := fetchTicker(context.Background(), client, log.Default(), store,
		"NOCIK", "", dir, "1y", 7*24*time.Hour, false)
	if err != nil {
		t.Fatalf("fetchTicker: %v", err)
	}
	if n != 1 {
		t.Fatalf("fetchTicker emitted %d events, want 1", n)
	}

	recs, err := store.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	var bar OHLCVBar
	if err := json.Unmarshal(recs[0].Event.Data, &bar); err != nil {
		t.Fatalf("unmarshal bar: %v", err)
	}
	if bar.MarketCap != 0 {
		t.Errorf("MarketCap = %d, want 0 for a ticker with no CIK", bar.MarketCap)
	}
}
