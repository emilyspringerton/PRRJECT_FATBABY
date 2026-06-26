package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/spider"
	"github.com/example/prrject-fatbaby/internal/spider/reddit"
	"github.com/example/prrject-fatbaby/internal/streamlog"
)

func TestResearchEmptyDomains(t *testing.T) {
	e := NewEngine(streamlog.Discard())
	e.WithSources(nil)
	result, err := e.Research(context.Background(), "test query", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Query != "test query" {
		t.Errorf("expected query set, got %q", result.Query)
	}
	if !result.FinishedAt.After(result.StartedAt) {
		t.Error("FinishedAt should be after StartedAt")
	}
}

func TestResearchFetchesStaticSources(t *testing.T) {
	var fetchCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Write([]byte("<html><head><title>Supply Chain News</title></head><body>supply chain update</body></html>"))
	}))
	defer srv.Close()

	e := NewEngine(streamlog.Discard())
	e.WithSources([]Source{
		{Name: "test-source", URL: srv.URL, Domain: DomainSupplyChain},
	})
	// Replace spider to use minimal rate limit.
	e.spider = &spider.Spider{Log: streamlog.Discard(), RateLimit: time.Millisecond}

	result, err := e.Research(context.Background(), "semiconductor shortage", []string{DomainSupplyChain})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Pages) == 0 {
		t.Error("expected at least one page result")
	}
	if result.Pages[0].Title != "Supply Chain News" {
		t.Errorf("unexpected title: %q", result.Pages[0].Title)
	}
}

func TestResearchSkipsNonMatchingDomains(t *testing.T) {
	var fetchCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Write([]byte("<html><body>financial data</body></html>"))
	}))
	defer srv.Close()

	e := NewEngine(streamlog.Discard())
	e.WithSources([]Source{
		{Name: "financial", URL: srv.URL, Domain: DomainFinancial},
		{Name: "supply", URL: srv.URL, Domain: DomainSupplyChain},
	})
	e.spider = &spider.Spider{Log: streamlog.Discard(), RateLimit: time.Millisecond}

	// Only request AI domain — no sources should match.
	result, err := e.Research(context.Background(), "neural networks", []string{DomainAI})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchCount != 0 {
		t.Errorf("expected no fetches for non-matching domain, got %d", fetchCount)
	}
	_ = result
}

func TestRedditSubsForDomains(t *testing.T) {
	subs := redditSubsForDomains(map[string]bool{DomainFinancial: true})
	if len(subs) == 0 {
		t.Error("expected financial subs")
	}
	for _, s := range subs {
		found := false
		for _, fs := range reddit.FinancialSubs {
			if s == fs {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected sub %q not in FinancialSubs", s)
		}
	}
}

func TestResearchSupplyChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>supply chain logistics</body></html>"))
	}))
	defer srv.Close()

	e := NewEngine(streamlog.Discard())
	e.WithSources([]Source{
		{Name: "sc", URL: srv.URL, Domain: DomainSupplyChain},
	})
	e.spider = &spider.Spider{Log: streamlog.Discard(), RateLimit: time.Millisecond}

	result, err := e.ResearchSupplyChain(context.Background(), "steel")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Domains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(result.Domains))
	}
}
