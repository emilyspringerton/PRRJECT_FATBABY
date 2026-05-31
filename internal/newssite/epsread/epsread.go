// Package epsread loads eps.Article records from var/eps/articles.ndjson into
// memory and exposes typed lookups with background refresh. It is the newssite's
// read model for EPS earnings articles produced by cmd/eps-processor.
package epsread

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/internal/eps"
)

// Store holds an in-memory snapshot of EPS articles, refreshed periodically.
type Store struct {
	mu       sync.RWMutex
	articles []*eps.Article // all articles, newest-first by PublishAt
	byTicker map[string][]*eps.Article
	dir      string
}

// NewStore creates a Store that reads from dir (e.g. "var/eps").
func NewStore(dir string) *Store {
	return &Store{
		dir:      dir,
		byTicker: make(map[string][]*eps.Article),
	}
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

	var all []*eps.Article
	byTicker := make(map[string][]*eps.Article)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var a eps.Article
		if err := json.Unmarshal(sc.Bytes(), &a); err != nil {
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

	// Sort newest-first. PublishAt is RFC3339; lexicographic sort works.
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

// Recent returns up to n articles, newest-first.
func (s *Store) Recent(n int) []*eps.Article {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || len(s.articles) == 0 {
		return nil
	}
	end := n
	if end > len(s.articles) {
		end = len(s.articles)
	}
	cp := make([]*eps.Article, end)
	copy(cp, s.articles[:end])
	return cp
}

// ArticlesFor returns all articles for ticker (upper-cased), newest-first.
func (s *Store) ArticlesFor(ticker string) []*eps.Article {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	s.mu.RLock()
	src := s.byTicker[ticker]
	cp := make([]*eps.Article, len(src))
	copy(cp, src)
	s.mu.RUnlock()
	return cp
}

// Count returns the total number of articles loaded.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.articles)
}

// StartRefresh launches a background goroutine that calls Refresh every interval.
// Stops when done is closed.
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
					logf("epsread refresh: %v", err)
				}
			}
		}
	}()
}
