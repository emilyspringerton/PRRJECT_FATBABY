package fedwatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testLogger struct{ t *testing.T }

func (l testLogger) Printf(format string, args ...any) { l.t.Logf(format, args...) }

func TestRunDiscovery_DryRunDoesNotWriteToStore(t *testing.T) {
	srv := testServer(t, sampleFeed, http.StatusOK)
	client := NewClient(ClientConfig{FeedURL: srv.URL})
	storeDir := t.TempDir()

	summary, err := RunDiscovery(context.Background(), RunnerConfig{
		StoreRoot: storeDir, DryRun: true, Client: client, Logger: testLogger{t},
	})
	if err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	if summary.Discovered != 2 {
		t.Fatalf("dry-run: Discovered = %d, want 2", summary.Discovered)
	}

	// A second dry-run against the same (empty) store should discover the
	// same 2 items again -- dry-run must not have persisted anything.
	summary2, err := RunDiscovery(context.Background(), RunnerConfig{
		StoreRoot: storeDir, DryRun: true, Client: client, Logger: testLogger{t},
	})
	if err != nil {
		t.Fatalf("RunDiscovery (2nd dry-run): %v", err)
	}
	if summary2.Discovered != 2 {
		t.Fatalf("second dry-run: Discovered = %d, want 2 (dry-run must not persist)", summary2.Discovered)
	}
}

func TestRunDiscovery_RealModeDedupesAcrossRuns(t *testing.T) {
	srv := testServer(t, sampleFeed, http.StatusOK)
	client := NewClient(ClientConfig{FeedURL: srv.URL})
	storeDir := t.TempDir()

	summary, err := RunDiscovery(context.Background(), RunnerConfig{
		StoreRoot: storeDir, DryRun: false, Client: client, Logger: testLogger{t},
	})
	if err != nil {
		t.Fatalf("RunDiscovery (1st real run): %v", err)
	}
	if summary.Discovered != 2 {
		t.Fatalf("1st run: Discovered = %d, want 2", summary.Discovered)
	}

	summary2, err := RunDiscovery(context.Background(), RunnerConfig{
		StoreRoot: storeDir, DryRun: false, Client: client, Logger: testLogger{t},
	})
	if err != nil {
		t.Fatalf("RunDiscovery (2nd real run): %v", err)
	}
	if summary2.Discovered != 0 {
		t.Errorf("2nd run: Discovered = %d, want 0 (both items already seen)", summary2.Discovered)
	}
	if summary2.SeenSkipped != 2 {
		t.Errorf("2nd run: SeenSkipped = %d, want 2", summary2.SeenSkipped)
	}
}

func TestRunDiscovery_EmptyFeedNoError(t *testing.T) {
	srv := testServer(t, `<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`, http.StatusOK)
	client := NewClient(ClientConfig{FeedURL: srv.URL})

	summary, err := RunDiscovery(context.Background(), RunnerConfig{
		StoreRoot: t.TempDir(), DryRun: false, Client: client, Logger: testLogger{t},
	})
	if err != nil {
		t.Fatalf("RunDiscovery: %v", err)
	}
	if summary.Discovered != 0 {
		t.Errorf("Discovered = %d, want 0", summary.Discovered)
	}
}

func TestRunDiscovery_FetchErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := NewClient(ClientConfig{FeedURL: srv.URL})

	_, err := RunDiscovery(context.Background(), RunnerConfig{
		StoreRoot: t.TempDir(), DryRun: true, Client: client, Logger: testLogger{t},
	})
	if err == nil {
		t.Fatal("expected an error when the feed fetch fails")
	}
}
