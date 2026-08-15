package commentary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeArticle(t *testing.T, dir string, a Article) {
	t.Helper()
	if err := Append(dir, a); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func TestStore_Refresh_LoadsArticles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeArticle(t, dir, Article{
		ID: "art-1", Ticker: "AAPL", Headline: "Apple governance concern",
		Body: "Board has failed majority vote.", PublishedAt: now,
	})
	writeArticle(t, dir, Article{
		ID: "art-2", Ticker: "MSFT", Headline: "Microsoft board stable",
		Body: "High trust across all directors.", PublishedAt: now.Add(-time.Hour),
	})

	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := len(s.Recent(10)); got != 2 {
		t.Errorf("Recent(10) = %d, want 2", got)
	}
}

func TestStore_Recent_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeArticle(t, dir, Article{ID: "old", Ticker: "X", Headline: "Old", PublishedAt: now.Add(-2 * time.Hour)})
	writeArticle(t, dir, Article{ID: "new", Ticker: "X", Headline: "New", PublishedAt: now})

	s := NewStore(dir)
	_ = s.Refresh()
	recent := s.Recent(2)
	if recent[0].ID != "new" {
		t.Errorf("first recent = %q, want new (newest-first ordering)", recent[0].ID)
	}
}

func TestStore_Recent_LimitRespected(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		writeArticle(t, dir, Article{
			ID: string(rune('A' + i)), Headline: "h", PublishedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	s := NewStore(dir)
	_ = s.Refresh()
	if got := len(s.Recent(3)); got != 3 {
		t.Errorf("Recent(3) = %d, want 3", got)
	}
}

func TestStore_ForTicker_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeArticle(t, dir, Article{ID: "a1", Ticker: "schw", Headline: "SCHW article", PublishedAt: now})
	writeArticle(t, dir, Article{ID: "a2", Ticker: "AAPL", Headline: "AAPL article", PublishedAt: now})

	s := NewStore(dir)
	_ = s.Refresh()

	// Both "SCHW" and "schw" should resolve to the same article.
	for _, q := range []string{"SCHW", "schw", "Schw"} {
		arts := s.ForTicker(q)
		if len(arts) != 1 {
			t.Errorf("ForTicker(%q) = %d, want 1", q, len(arts))
		}
	}
	if len(s.ForTicker("AAPL")) != 1 {
		t.Error("ForTicker(AAPL) should return 1 article")
	}
	if len(s.ForTicker("NVDA")) != 0 {
		t.Error("ForTicker(NVDA) should return 0 articles")
	}
}

func TestStore_Refresh_SkipsInvalidRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "articles.ndjson")

	valid, _ := json.Marshal(Article{ID: "v1", Headline: "Valid", PublishedAt: time.Now().UTC()})
	lines := append(valid, '\n')
	lines = append(lines, []byte("not json\n")...)
	lines = append(lines, []byte(`{"id":"","headline":"no-id-skipped"}`+"\n")...)

	if err := os.WriteFile(path, lines, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := len(s.Recent(10)); got != 1 {
		t.Errorf("expected 1 valid article, got %d", got)
	}
}

func TestStore_EmptyDir_NoError(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Errorf("Refresh on empty dir should not error: %v", err)
	}
	if got := len(s.Recent(10)); got != 0 {
		t.Errorf("empty store should have 0 articles, got %d", got)
	}
}

func TestStore_ByID(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeArticle(t, dir, Article{ID: "movers-2026-07-20", Headline: "Stocks on the Move", Body: "...", PublishedAt: now})

	s := NewStore(dir)
	_ = s.Refresh()

	art, ok := s.ByID("movers-2026-07-20")
	if !ok {
		t.Fatal("expected to find article by ID")
	}
	if art.Headline != "Stocks on the Move" {
		t.Errorf("Headline = %q", art.Headline)
	}
	if _, ok := s.ByID("does-not-exist"); ok {
		t.Error("expected ByID to report not-found for an unknown ID")
	}
}

// TestStore_Refresh_DedupsByID_LastWriteWins is the regression test for
// S167-05 (EMILY/BACKLOG.md SECTION 167): articles.ndjson is append-only, so
// a same-day re-run or retry of a writer (movers-watcher and similar,
// S167-03's own finding) could append a second row with an ID already
// present. Before this fix Refresh() kept every row unconditionally, so
// Recent()/ForTicker() surfaced duplicate cards until someone manually
// cleaned up the NDJSON file.
func TestStore_Refresh_DedupsByID_LastWriteWins(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeArticle(t, dir, Article{
		ID: "movers-2026-08-15", Ticker: "AAPL", Headline: "Stale headline",
		Body: "first pass", PublishedAt: now,
	})
	writeArticle(t, dir, Article{
		ID: "movers-2026-08-15", Ticker: "AAPL", Headline: "Corrected headline",
		Body: "retry after failure", PublishedAt: now.Add(time.Minute),
	})

	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := len(s.Recent(10)); got != 1 {
		t.Fatalf("Recent(10) = %d, want 1 (duplicate ID should collapse)", got)
	}
	art, ok := s.ByID("movers-2026-08-15")
	if !ok {
		t.Fatal("expected to find article by ID")
	}
	if art.Headline != "Corrected headline" {
		t.Errorf("Headline = %q, want last-write-wins to keep the later row", art.Headline)
	}

	byTicker := s.ForTicker("AAPL")
	if len(byTicker) != 1 {
		t.Errorf("ForTicker(AAPL) = %d, want 1 (dedup must apply to the per-ticker index too)", len(byTicker))
	}
}

func TestStore_ByKind(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeArticle(t, dir, Article{ID: "m1", Kind: "market_movers", Headline: "Movers 1", PublishedAt: now.Add(-time.Hour)})
	writeArticle(t, dir, Article{ID: "m2", Kind: "market_movers", Headline: "Movers 2", PublishedAt: now})
	writeArticle(t, dir, Article{ID: "g1", Kind: "governance_alert", Headline: "Governance", PublishedAt: now})

	s := NewStore(dir)
	_ = s.Refresh()

	movers := s.ByKind("market_movers", 10)
	if len(movers) != 2 {
		t.Fatalf("ByKind(market_movers) = %d articles, want 2", len(movers))
	}
	if movers[0].ID != "m2" {
		t.Errorf("expected newest-first: first = %q, want m2", movers[0].ID)
	}

	limited := s.ByKind("market_movers", 1)
	if len(limited) != 1 {
		t.Errorf("ByKind with n=1 should cap at 1, got %d", len(limited))
	}

	if got := s.ByKind("nonexistent_kind", 10); len(got) != 0 {
		t.Errorf("ByKind for unknown kind should return empty, got %d", len(got))
	}
}
