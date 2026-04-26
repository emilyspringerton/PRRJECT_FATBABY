package secwatch

import (
	"context"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientFetchSubmissions_RetryOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("missing user agent")
		}
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cik":"320193","filings":{"recent":{"accessionNumber":[],"form":[],"filingDate":[],"primaryDocument":[]}}}`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{
		BaseURL:      srv.URL,
		MaxRetries:   2,
		BackoffBase:  5 * time.Millisecond,
		BackoffCap:   10 * time.Millisecond,
		RateLimitRPS: 1000,
		Timeout:      2 * time.Second,
		Random:       rand.New(rand.NewSource(1)),
	})
	if _, err := c.FetchSubmissions(context.Background(), "320193"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected retry, calls=%d", calls)
	}
}

func TestBackoffForBounded(t *testing.T) {
	c := NewClient(ClientConfig{BackoffBase: 100 * time.Millisecond, BackoffCap: 300 * time.Millisecond, Random: rand.New(rand.NewSource(2))})
	for i := 0; i < 6; i++ {
		d := c.backoffFor(i)
		if d <= 0 || d > 300*time.Millisecond {
			t.Fatalf("bad backoff %v", d)
		}
	}
}
