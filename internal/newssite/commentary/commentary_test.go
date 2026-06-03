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
