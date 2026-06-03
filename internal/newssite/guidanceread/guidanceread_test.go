package guidanceread

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/internal/guidance"
)

func writeGuidanceArticle(t *testing.T, dir string, a guidance.Article) {
	t.Helper()
	path := filepath.Join(dir, "articles.ndjson")
	line, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

func TestGuidanceStore_LoadsArticles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	writeGuidanceArticle(t, dir, guidance.Article{
		ID: "g1", Ticker: "AAPL", Headline: "Apple raises guidance",
		Action: guidance.ActionRaised, Metric: guidance.MetricEPS, PublishAt: now,
	})
	writeGuidanceArticle(t, dir, guidance.Article{
		ID: "g2", Ticker: "MSFT", Headline: "Microsoft lowers forecast",
		Action: guidance.ActionLowered, Metric: guidance.MetricRevenue, PublishAt: now,
	})

	s := NewStore(dir)
	if err := s.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if s.Count() != 2 {
		t.Errorf("Count = %d, want 2", s.Count())
	}
}

func TestGuidanceStore_Recent_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	fresh := time.Now().UTC().Format(time.RFC3339)
	writeGuidanceArticle(t, dir, guidance.Article{ID: "old", Headline: "Old", PublishAt: old})
	writeGuidanceArticle(t, dir, guidance.Article{ID: "new", Headline: "New", PublishAt: fresh})

	s := NewStore(dir)
	_ = s.Refresh()
	recent := s.Recent(2)
	if recent[0].ID != "new" {
		t.Errorf("first = %q, want new (newest-first)", recent[0].ID)
	}
}

func TestGuidanceStore_ForTicker(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	writeGuidanceArticle(t, dir, guidance.Article{ID: "a1", Ticker: "schw", Headline: "SCHW guidance", PublishAt: now})
	writeGuidanceArticle(t, dir, guidance.Article{ID: "a2", Ticker: "AAPL", Headline: "AAPL guidance", PublishAt: now})

	s := NewStore(dir)
	_ = s.Refresh()

	for _, q := range []string{"SCHW", "schw"} {
		arts := s.ForTicker(q)
		if len(arts) != 1 {
			t.Errorf("ForTicker(%q) = %d, want 1", q, len(arts))
		}
	}
	if len(s.ForTicker("NVDA")) != 0 {
		t.Error("ForTicker(NVDA) should return 0 articles")
	}
}

func TestGuidanceStore_SkipsEmptyHeadline(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	writeGuidanceArticle(t, dir, guidance.Article{ID: "ok", Ticker: "AAPL", Headline: "Valid", PublishAt: now})
	writeGuidanceArticle(t, dir, guidance.Article{ID: "bad", Ticker: "AAPL", Headline: "", PublishAt: now}) // no headline

	s := NewStore(dir)
	_ = s.Refresh()
	if s.Count() != 1 {
		t.Errorf("expected 1 (skipped empty headline), got %d", s.Count())
	}
}

func TestGuidanceStore_EmptyDir_NoError(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Refresh(); err != nil {
		t.Errorf("Refresh on empty dir: %v", err)
	}
	if s.Count() != 0 {
		t.Errorf("empty store should have Count=0, got %d", s.Count())
	}
}

func TestFormatPeriodStr(t *testing.T) {
	cases := []struct {
		p    guidance.Period
		want string
	}{
		{guidance.Period{FiscalQuarter: "Q3", FiscalYear: 2026}, "Q3 2026"},
		{guidance.Period{FiscalQuarter: "Q1"}, "Q1"},
		{guidance.Period{FiscalYear: 2027}, "FY2027"},
		{guidance.Period{}, ""},
	}
	for _, tc := range cases {
		got := formatPeriodStr(tc.p)
		if got != tc.want {
			t.Errorf("formatPeriodStr(%v) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestArticleToView_FieldMapping(t *testing.T) {
	a := &guidance.Article{
		ID:      "g1",
		Ticker:  "aapl",
		Headline: "Apple raises EPS guidance",
		Action:  guidance.ActionRaised,
		Metric:  guidance.MetricEPS,
		Period:  guidance.Period{FiscalQuarter: "Q3", FiscalYear: 2026},
	}
	v := ArticleToView(a)
	if v.Ticker != "AAPL" {
		t.Errorf("Ticker = %q, want AAPL (uppercased)", v.Ticker)
	}
	if v.Headline != "Apple raises EPS guidance" {
		t.Errorf("Headline = %q", v.Headline)
	}
	if v.Action != "raised" {
		t.Errorf("Action = %q, want raised", v.Action)
	}
	if v.Period != "Q3 2026" {
		t.Errorf("Period = %q, want Q3 2026", v.Period)
	}
	if v.Link == "" {
		t.Error("Link should not be empty")
	}
}
