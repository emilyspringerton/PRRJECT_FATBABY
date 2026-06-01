// Package guidanceread loads guidance.Article records from var/guidance/articles.ndjson
// and exposes typed lookups with background refresh. It is the newssite read
// model for guidance articles produced by cmd/guidance-watcher.
package guidanceread

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/internal/guidance"
)

// Store holds an in-memory snapshot of guidance articles, refreshed periodically.
type Store struct {
	mu       sync.RWMutex
	articles []*guidance.Article
	byTicker map[string][]*guidance.Article
	dir      string
}

// NewStore creates a Store that reads from dir (e.g. "var/guidance").
func NewStore(dir string) *Store {
	return &Store{dir: dir, byTicker: make(map[string][]*guidance.Article)}
}

// Refresh reloads articles.ndjson from disk. Safe to call concurrently.
func (s *Store) Refresh() error {
	path := filepath.Join(s.dir, "articles.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	var all []*guidance.Article
	byTicker := make(map[string][]*guidance.Article)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var a guidance.Article
		if err := json.Unmarshal(sc.Bytes(), &a); err != nil {
			continue
		}
		if a.Headline == "" {
			continue
		}
		cp := a
		all = append(all, &cp)
		t := strings.ToUpper(strings.TrimSpace(a.Ticker))
		if t != "" {
			byTicker[t] = append(byTicker[t], &cp)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].PublishAt > all[j].PublishAt
	})
	for t := range byTicker {
		sort.Slice(byTicker[t], func(i, j int) bool {
			return byTicker[t][i].PublishAt > byTicker[t][j].PublishAt
		})
	}

	s.mu.Lock()
	s.articles = all
	s.byTicker = byTicker
	s.mu.Unlock()
	return nil
}

// Recent returns the N most recent articles across all tickers.
func (s *Store) Recent(n int) []*guidance.Article {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || len(s.articles) == 0 {
		return nil
	}
	if n > len(s.articles) {
		n = len(s.articles)
	}
	out := make([]*guidance.Article, n)
	copy(out, s.articles[:n])
	return out
}

// ForTicker returns all articles for the given ticker, newest-first.
func (s *Store) ForTicker(ticker string) []*guidance.Article {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := strings.ToUpper(strings.TrimSpace(ticker))
	return append([]*guidance.Article{}, s.byTicker[t]...)
}

// Count returns total article count.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.articles)
}

// StartRefresh launches a background goroutine that calls Refresh on the given
// interval. Send to done to stop.
func (s *Store) StartRefresh(interval time.Duration, logf func(string, ...any), done <-chan struct{}) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := s.Refresh(); err != nil {
					logf("guidanceread refresh err: %v", err)
				}
			}
		}
	}()
}

// ArticleToView converts a guidance.Article to a display-ready view.
func ArticleToView(a *guidance.Article) GuidanceItemView {
	return GuidanceItemView{
		Ticker:    strings.ToUpper(strings.TrimSpace(a.Ticker)),
		Headline:  a.Headline,
		Body:      a.Body,
		Action:    string(a.Action),
		Metric:    string(a.Metric),
		Period:    formatPeriodStr(a.Period),
		PublishAt: a.PublishAt,
		Link:      "/section/guidance",
	}
}

// GuidanceItemView is a view-ready representation of one guidance article.
type GuidanceItemView struct {
	Ticker    string
	Headline  string
	Body      string
	Action    string // raised|lowered|maintained|initiated|withdrawn|updated
	Metric    string // eps|revenue|both
	Period    string // formatted period string
	PublishAt string
	Link      string
}

func formatPeriodStr(p guidance.Period) string {
	switch {
	case p.FiscalQuarter != "" && p.FiscalYear > 0:
		return fmt.Sprintf("%s %d", p.FiscalQuarter, p.FiscalYear)
	case p.FiscalQuarter != "":
		return p.FiscalQuarter
	case p.FiscalYear > 0:
		return fmt.Sprintf("FY%d", p.FiscalYear)
	default:
		return ""
	}
}

var _ = log.Printf // satisfies the logger import
