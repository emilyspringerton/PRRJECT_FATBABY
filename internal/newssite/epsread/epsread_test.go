package epsread

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/prrject-fatbaby/internal/eps"
)

func writeArticles(t *testing.T, dir string, articles []eps.Article) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, "articles.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := range articles {
		if err := enc.Encode(articles[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRefresh_Empty(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh on empty dir: %v", err)
	}
	if s.Count() != 0 {
		t.Errorf("count = %d, want 0", s.Count())
	}
	if got := s.Recent(10); len(got) != 0 {
		t.Errorf("Recent(10) = %d items, want 0", len(got))
	}
}

func TestRefresh_LoadsAndSortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	articles := []eps.Article{
		{SourceIdentity: "a:1", Ticker: "AAPL", Headline: "AAPL Q1", PublishAt: "2026-01-15T10:00:00Z", EPSValue: 1.23, IsGAAP: true},
		{SourceIdentity: "a:2", Ticker: "AAPL", Headline: "AAPL Q2", PublishAt: "2026-04-15T10:00:00Z", EPSValue: 1.45, IsGAAP: true},
		{SourceIdentity: "b:1", Ticker: "MSFT", Headline: "MSFT Q1", PublishAt: "2026-01-20T10:00:00Z", EPSValue: 2.10, IsGAAP: true},
	}
	writeArticles(t, dir, articles)

	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if s.Count() != 3 {
		t.Fatalf("count = %d, want 3", s.Count())
	}

	recent := s.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("Recent(3) len = %d, want 3", len(recent))
	}
	// Newest first: AAPL Q2 (April) > MSFT Q1 (Jan 20) > AAPL Q1 (Jan 15)
	if recent[0].Headline != "AAPL Q2" {
		t.Errorf("recent[0] = %q, want AAPL Q2", recent[0].Headline)
	}
	if recent[len(recent)-1].Headline != "AAPL Q1" {
		t.Errorf("recent[last] = %q, want AAPL Q1", recent[len(recent)-1].Headline)
	}
}

func TestArticlesFor_FiltersByTicker(t *testing.T) {
	dir := t.TempDir()
	articles := []eps.Article{
		{SourceIdentity: "a:1", Ticker: "AAPL", PublishAt: "2026-01-15T10:00:00Z"},
		{SourceIdentity: "a:2", Ticker: "AAPL", PublishAt: "2026-04-15T10:00:00Z"},
		{SourceIdentity: "b:1", Ticker: "MSFT", PublishAt: "2026-01-20T10:00:00Z"},
	}
	writeArticles(t, dir, articles)

	s := NewStore(dir)
	_ = s.Refresh()

	aapl := s.ArticlesFor("AAPL")
	if len(aapl) != 2 {
		t.Errorf("ArticlesFor(AAPL) = %d, want 2", len(aapl))
	}
	// case-insensitive
	aapl2 := s.ArticlesFor("aapl")
	if len(aapl2) != 2 {
		t.Errorf("ArticlesFor(aapl) = %d, want 2 (case-insensitive)", len(aapl2))
	}
	msft := s.ArticlesFor("MSFT")
	if len(msft) != 1 {
		t.Errorf("ArticlesFor(MSFT) = %d, want 1", len(msft))
	}
	unknown := s.ArticlesFor("TSLA")
	if len(unknown) != 0 {
		t.Errorf("ArticlesFor(TSLA) = %d, want 0", len(unknown))
	}
}

func TestRecent_CapAtN(t *testing.T) {
	dir := t.TempDir()
	var articles []eps.Article
	for i := 0; i < 10; i++ {
		articles = append(articles, eps.Article{
			SourceIdentity: "a:1",
			Ticker:         "X",
			PublishAt:      "2026-01-01T00:00:00Z",
		})
	}
	writeArticles(t, dir, articles)
	s := NewStore(dir)
	_ = s.Refresh()
	if got := s.Recent(3); len(got) != 3 {
		t.Errorf("Recent(3) from 10 = %d, want 3", len(got))
	}
	if got := s.Recent(0); len(got) != 0 {
		t.Errorf("Recent(0) = %d, want 0", len(got))
	}
}
