package secwatch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/identity"
	"github.com/example/prrject-fatbaby/internal/issuerregistry"
)

func TestLoadSeenIdentities(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	data, _ := json.Marshal(FilingDiscoveredEvent{CIK: "320193", AccessionNumber: "0001"})
	_, err = store.Append(context.Background(), eventstore.Event{ID: "1", Type: "filing_discovered", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	seen, err := LoadSeenIdentities(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := seen[FilingIdentity("320193", "0001")]; !ok {
		t.Fatal("expected seen identity")
	}
}

func TestRunDiscovery_DryRunAndRealMode(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cik":"320193","filings":{"recent":{"accessionNumber":["0001"],"form":["8-K"],"filingDate":["2026-04-25"],"primaryDocument":["x.htm"],"acceptanceDateTime":["2026-04-25T12:00:00.000Z"]}}}`))
	}))
	defer srv.Close()

	watchlistPath := filepath.Join(t.TempDir(), "watchlist.json")
	if err := os.WriteFile(watchlistPath, []byte(`{"entries":[{"ticker":"AAPL","cik":"320193","allowed_forms":["8-K"],"enabled":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	storeDir := t.TempDir()
	logger := testingLogger{t: t}
	client := NewClient(ClientConfig{BaseURL: srv.URL, MaxRetries: 1, RateLimitRPS: 1000, Timeout: 2 * time.Second})

	summary, err := RunDiscovery(context.Background(), RunnerConfig{WatchlistPath: watchlistPath, StoreRoot: storeDir, DryRun: true, Client: client, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Discovered != 1 {
		t.Fatalf("dry-run expected discovered=1 got=%d", summary.Discovered)
	}

	summary, err = RunDiscovery(context.Background(), RunnerConfig{WatchlistPath: watchlistPath, StoreRoot: storeDir, DryRun: false, Client: client, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Discovered != 1 {
		t.Fatalf("real mode expected discovered=1 got=%d", summary.Discovered)
	}

	summary, err = RunDiscovery(context.Background(), RunnerConfig{WatchlistPath: watchlistPath, StoreRoot: storeDir, DryRun: false, Client: client, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Discovered != 0 || summary.SeenSkipped == 0 {
		t.Fatalf("expected dedupe skip, got discovered=%d skipped=%d", summary.Discovered, summary.SeenSkipped)
	}
	if hits < 3 {
		t.Fatalf("expected server hit for each run, got=%d", hits)
	}
}

func TestDiscoveryEventData_TickerPopulated(t *testing.T) {
	reg := issuerregistry.New(map[string][]identity.SecurityRef{})
	f := Filing{
		Ticker:          "NVDA",
		CIK:             "0001045810",
		AccessionNumber: "0001045810-26-000001",
		Form:            "8-K",
		FilingDate:      "2026-05-01",
		PrimaryDocument: "https://www.sec.gov/Archives/edgar/data/1045810/000104581026000001/nvda8k.htm",
		SubmissionsURL:  SubmissionsURL("0001045810"),
	}
	data := discoveryEventData(f, time.Now().UTC(), reg)
	if data.Ticker != "NVDA" {
		t.Errorf("Ticker got %q want NVDA", data.Ticker)
	}
}

type testingLogger struct{ t *testing.T }

func (l testingLogger) Printf(format string, args ...any) {
	l.t.Logf(strings.TrimSpace(format), args...)
}

func TestBuildIssuerRegistry_MintsSkuldmarkIDWhenExchangeKnown(t *testing.T) {
	entries := []WatchEntry{
		{Ticker: "AAPL", CIK: "320193", Exchange: "Nasdaq", Enabled: true},
	}
	reg := buildIssuerRegistry(entries, testingLogger{t})
	refs := reg.ResolveByCIK("320193")
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	want := "EINXNASAAPLXXX0000320193Y"
	if refs[0].SkuldmarkID != want {
		t.Errorf("SkuldmarkID = %q, want %q", refs[0].SkuldmarkID, want)
	}
	if refs[0].Exchange != "Nasdaq" {
		t.Errorf("Exchange = %q, want %q", refs[0].Exchange, "Nasdaq")
	}
}

func TestBuildIssuerRegistry_SkipsMintWhenExchangeUnknown(t *testing.T) {
	entries := []WatchEntry{
		// Mirrors the real BLK/XOM case found 2026-08-14: CIK on file doesn't match
		// SEC's own record of the actual trading entity, so Exchange is left empty
		// rather than guessed -- must not mint a wrong-but-plausible-looking ID.
		{Ticker: "XOM", CIK: "34088", Exchange: "", Enabled: true},
	}
	reg := buildIssuerRegistry(entries, testingLogger{t})
	refs := reg.ResolveByCIK("34088")
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	if refs[0].SkuldmarkID != "" {
		t.Errorf("expected no SkuldmarkID minted, got %q", refs[0].SkuldmarkID)
	}
}

func TestDiscoveryEventData_CarriesSkuldmarkIDFromRegistry(t *testing.T) {
	reg := buildIssuerRegistry([]WatchEntry{
		{Ticker: "AAPL", CIK: "320193", Exchange: "Nasdaq", Enabled: true},
	}, testingLogger{t})
	f := Filing{
		Ticker:          "AAPL",
		CIK:             "320193",
		AccessionNumber: "0000320193-26-000001",
		Form:            "8-K",
		FilingDate:      "2026-05-01",
		SubmissionsURL:  SubmissionsURL("320193"),
	}
	data := discoveryEventData(f, time.Now().UTC(), reg)
	if data.Identity.PrimaryTicker == nil {
		t.Fatal("expected a PrimaryTicker")
	}
	want := "EINXNASAAPLXXX0000320193Y"
	if data.Identity.PrimaryTicker.SkuldmarkID != want {
		t.Errorf("PrimaryTicker.SkuldmarkID = %q, want %q", data.Identity.PrimaryTicker.SkuldmarkID, want)
	}
}

// TestFilingDiscoveredEvent_RoundTripsSkuldmarkID is a regression test for a
// real bug found while wiring S175-02: FilingDiscovered (write side, this
// package) has always carried Identity.PrimaryTicker.SkuldmarkID once
// S175-01 landed, but FilingDiscoveredEvent (read side -- what
// internal/processor's worker.go actually unmarshals real event JSON into)
// had no Identity field to receive it, so json.Unmarshal silently dropped
// it. This proves the round trip now survives.
func TestFilingDiscoveredEvent_RoundTripsSkuldmarkID(t *testing.T) {
	reg := buildIssuerRegistry([]WatchEntry{
		{Ticker: "AAPL", CIK: "320193", Exchange: "Nasdaq", Enabled: true},
	}, testingLogger{t})
	f := Filing{
		Ticker:          "AAPL",
		CIK:             "320193",
		AccessionNumber: "0000320193-26-000001",
		Form:            "8-K",
		FilingDate:      "2026-05-01",
		SubmissionsURL:  SubmissionsURL("320193"),
	}
	written := discoveryEventData(f, time.Now().UTC(), reg)

	raw, err := json.Marshal(written)
	if err != nil {
		t.Fatalf("marshal FilingDiscovered: %v", err)
	}

	var read FilingDiscoveredEvent
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatalf("unmarshal into FilingDiscoveredEvent: %v", err)
	}

	want := "EINXNASAAPLXXX0000320193Y"
	if got := read.SkuldmarkID(); got != want {
		t.Errorf("read.SkuldmarkID() = %q, want %q", got, want)
	}
}

// TestFilingDiscoveredEvent_SkuldmarkIDEmptyWhenUnminted confirms the
// helper never guesses: an event with no PrimaryTicker (or an unminted
// one) returns "", not a zero-value panic or a fabricated ID.
func TestFilingDiscoveredEvent_SkuldmarkIDEmptyWhenUnminted(t *testing.T) {
	var e FilingDiscoveredEvent
	if got := e.SkuldmarkID(); got != "" {
		t.Errorf("SkuldmarkID() on empty event = %q, want empty", got)
	}
}
