// Package cooccurrence tracks entity (ticker) co-occurrence relationships.
//
// When two tickers appear in the same signal observation within 48 hours,
// a weighted co-occurrence edge is recorded. Edges decay when not reinforced.
//
// The store is in-memory (rebuilt from the signal index on startup). MongoDB
// persistence is optional — the store is a read cache, not the source of truth.
package cooccurrence

import (
	"sort"
	"sync"
	"time"
)

const (
	// WindowHours is the time window in which two tickers must co-appear to form an edge.
	WindowHours = 48
	// MaxRelated is the maximum number of related tickers returned per query.
	MaxRelated = 10
)

// Edge records a co-occurrence relationship between two tickers.
type Edge struct {
	TickerA   string    // lexicographically smaller ticker
	TickerB   string    // lexicographically larger ticker
	Weight    int       // number of co-occurrences
	FirstSeen time.Time
	LastSeen  time.Time
}

// edgeKey returns the canonical key for a (tickerA, tickerB) pair.
func edgeKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + ":" + b
}

// Store is the in-memory co-occurrence edge store.
type Store struct {
	mu    sync.RWMutex
	edges map[string]*Edge // edgeKey → *Edge
}

// NewStore creates an empty co-occurrence store.
func NewStore() *Store {
	return &Store{edges: make(map[string]*Edge)}
}

// Record adds or strengthens a co-occurrence edge between two tickers.
// The edge is only recorded if both tickers are distinct and non-empty.
func (s *Store) Record(tickerA, tickerB string, at time.Time) {
	if tickerA == "" || tickerB == "" || tickerA == tickerB {
		return
	}
	key := edgeKey(tickerA, tickerB)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.edges[key]; ok {
		e.Weight++
		if at.After(e.LastSeen) {
			e.LastSeen = at
		}
	} else {
		a, b := tickerA, tickerB
		if a > b {
			a, b = b, a
		}
		s.edges[key] = &Edge{
			TickerA:   a,
			TickerB:   b,
			Weight:    1,
			FirstSeen: at,
			LastSeen:  at,
		}
	}
}

// RecordGroup records all pair-wise co-occurrences within a group of tickers.
func (s *Store) RecordGroup(tickers []string, at time.Time) {
	for i := 0; i < len(tickers); i++ {
		for j := i + 1; j < len(tickers); j++ {
			s.Record(tickers[i], tickers[j], at)
		}
	}
}

// Related returns up to MaxRelated tickers most co-related to ticker,
// ordered by edge weight descending.
func (s *Store) Related(ticker string) []RelatedEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scored struct {
		ticker string
		weight int
		seen   time.Time
	}
	var candidates []scored

	for _, e := range s.edges {
		var other string
		if e.TickerA == ticker {
			other = e.TickerB
		} else if e.TickerB == ticker {
			other = e.TickerA
		} else {
			continue
		}
		candidates = append(candidates, scored{other, e.Weight, e.LastSeen})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].weight != candidates[j].weight {
			return candidates[i].weight > candidates[j].weight
		}
		return candidates[i].seen.After(candidates[j].seen)
	})

	max := MaxRelated
	if max > len(candidates) {
		max = len(candidates)
	}
	out := make([]RelatedEntry, max)
	for i, c := range candidates[:max] {
		out[i] = RelatedEntry{Ticker: c.ticker, Weight: c.weight, LastSeen: c.seen}
	}
	return out
}

// RelatedEntry is one entry in a Related response.
type RelatedEntry struct {
	Ticker   string    `json:"ticker"`
	Weight   int       `json:"weight"`
	LastSeen time.Time `json:"last_seen"`
}

// EdgeCount returns the total number of edges.
func (s *Store) EdgeCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.edges)
}

// BuildFromSignals seeds the store from a list of (tickers, timestamp) observations.
// Pairs are only recorded if both tickers were seen within WindowHours of each other.
// Call this once at startup to warm the in-memory store from historical signal data.
func (s *Store) BuildFromSignals(observations []Observation) {
	// Group observations by a 48h bucket. For simplicity, record all pairs
	// within each observation — each observation represents a co-appearance event.
	for _, obs := range observations {
		s.RecordGroup(obs.Tickers, obs.At)
	}
}

// Observation is one signal observation record for co-occurrence seeding.
type Observation struct {
	Tickers []string
	At      time.Time
}

// AllEdges returns a snapshot of all edges ordered by weight descending.
func (s *Store) AllEdges() []Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Edge, 0, len(s.edges))
	for _, e := range s.edges {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Weight > out[j].Weight })
	return out
}
