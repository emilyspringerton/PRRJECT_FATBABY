// Package commentary loads Emily-authored governance articles from
// var/commentary/articles.ndjson into memory for newssite rendering.
// Articles are written by cmd/emily-agent via fatbaby_publish_commentary and
// appear in the newssite alongside pipeline-sourced filings and signals.
package commentary

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Article is one Emily-authored governance commentary piece.
type Article struct {
	ID          string    `json:"id"`
	Ticker      string    `json:"ticker"`
	Headline    string    `json:"headline"`
	Body        string    `json:"body"`
	Preview     string    `json:"preview"`
	Byline      string    `json:"byline"`
	Kind        string    `json:"kind"`       // signal_commentary|governance_alert|eps_reconciliation|cross_ticker_summary
	FilingDate  string    `json:"filing_date,omitempty"` // SEC filing date being discussed
	PublishedAt time.Time `json:"published_at"`
	SignalIDs   []string  `json:"signal_ids,omitempty"`
}

// Store holds an in-memory snapshot of commentary articles.
type Store struct {
	mu       sync.RWMutex
	articles []*Article
	byTicker map[string][]*Article
	dir      string
}

// NewStore creates a Store that reads from dir (e.g. "var/commentary").
func NewStore(dir string) *Store {
	return &Store{dir: dir, byTicker: make(map[string][]*Article)}
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

	var all []*Article
	byTicker := make(map[string][]*Article)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var a Article
		if err := json.Unmarshal(sc.Bytes(), &a); err != nil {
			continue
		}
		if a.ID == "" || a.Headline == "" {
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

	// Sort newest-first.
	sort.Slice(all, func(i, j int) bool {
		return all[i].PublishedAt.After(all[j].PublishedAt)
	})
	for t := range byTicker {
		sort.Slice(byTicker[t], func(i, j int) bool {
			return byTicker[t][i].PublishedAt.After(byTicker[t][j].PublishedAt)
		})
	}

	s.mu.Lock()
	s.articles = all
	s.byTicker = byTicker
	s.mu.Unlock()
	return nil
}

// Recent returns the N most recent articles across all tickers.
func (s *Store) Recent(n int) []*Article {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || len(s.articles) == 0 {
		return nil
	}
	if n > len(s.articles) {
		n = len(s.articles)
	}
	out := make([]*Article, n)
	copy(out, s.articles[:n])
	return out
}

// ForTicker returns all articles for the given ticker, newest-first.
func (s *Store) ForTicker(ticker string) []*Article {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := strings.ToUpper(strings.TrimSpace(ticker))
	return append([]*Article{}, s.byTicker[t]...)
}

// Append writes one article to articles.ndjson (append-only).
// Used by the newssite HTTP ingest endpoint.
func Append(dir string, a Article) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "articles.ndjson")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(a)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}
