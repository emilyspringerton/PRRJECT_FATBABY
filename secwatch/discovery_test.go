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

type testingLogger struct{ t *testing.T }

func (l testingLogger) Printf(format string, args ...any) {
	l.t.Logf(strings.TrimSpace(format), args...)
}
