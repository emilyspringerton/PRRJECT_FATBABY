// Package graphread loads entity-graph NDJSON files into memory and exposes
// typed lookups with background refresh.
package graphread

import (
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// Store holds an in-memory snapshot of entity-graph data, refreshed periodically.
type Store struct {
	mu          sync.RWMutex
	signals     []entitygraph.Signal
	nodes       map[string]*entitygraph.PersonNode
	dirByTicker map[string][]*entitygraph.PersonNode  // ticker → directors
	auditors    map[string]*entitygraph.AuditorRecord // ticker → auditor
	edgesByNode map[string][]*entitygraph.Edge        // canonical_id → edges involving that person
	dir         string
}

// NewStore creates a Store pointed at dir (e.g. var/entity-graph).
func NewStore(dir string) *Store {
	return &Store{
		dir:         dir,
		nodes:       make(map[string]*entitygraph.PersonNode),
		dirByTicker: make(map[string][]*entitygraph.PersonNode),
		auditors:    make(map[string]*entitygraph.AuditorRecord),
		edgesByNode: make(map[string][]*entitygraph.Edge),
	}
}

// Refresh reloads all entity-graph data from disk. Safe to call concurrently.
func (s *Store) Refresh() error {
	sigs, err := entitygraph.LoadSignals(s.dir)
	if err != nil {
		return err
	}

	g := entitygraph.NewGraph()
	if err := g.LoadNodesFromDir(s.dir); err != nil {
		return err
	}
	if err := g.LoadAuditorsFromDir(s.dir); err != nil {
		return err
	}
	if err := g.LoadEdgesFromDir(s.dir); err != nil {
		return err
	}

	// Build directorsByTicker and edgesByNode reverse indexes.
	// A node is added to a ticker's list at most once (deduplication by canonical ID).
	dirByTicker := make(map[string][]*entitygraph.PersonNode)
	for _, node := range g.Nodes {
		seen := make(map[string]bool)
		for _, f := range node.Filings {
			t := norm(f.Ticker)
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			dirByTicker[t] = append(dirByTicker[t], node)
		}
	}

	// Build edgesByNode index so we can look up all edges for a canonical ID quickly.
	edgesByNode := make(map[string][]*entitygraph.Edge, len(g.Edges)*2)
	for _, e := range g.Edges {
		edge := e // capture
		edgesByNode[e.Source] = append(edgesByNode[e.Source], edge)
		edgesByNode[e.Target] = append(edgesByNode[e.Target], edge)
	}

	s.mu.Lock()
	s.signals = sigs
	s.nodes = g.Nodes
	s.dirByTicker = dirByTicker
	s.auditors = g.Auditors
	s.edgesByNode = edgesByNode
	s.mu.Unlock()
	return nil
}

// LiveSignals returns unexpired signals. If ticker is non-empty, only signals
// for that ticker are returned. today must be YYYY-MM-DD.
func (s *Store) LiveSignals(ticker, today string) []entitygraph.Signal {
	ticker = norm(ticker)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []entitygraph.Signal
	for _, sig := range s.signals {
		if sig.ValidThrough != "" && sig.ValidThrough < today {
			continue
		}
		if ticker != "" && norm(sig.Ticker) != ticker {
			continue
		}
		out = append(out, sig)
	}
	return out
}

// AllSignals returns a snapshot of all signals (including expired).
func (s *Store) AllSignals() []entitygraph.Signal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]entitygraph.Signal, len(s.signals))
	copy(cp, s.signals)
	return cp
}

// Node looks up a PersonNode by canonical ID.
func (s *Store) Node(canonicalID string) (*entitygraph.PersonNode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[canonicalID]
	return n, ok
}

// DirectorsFor returns all PersonNodes that have appeared in filings for ticker.
// The returned slice is a copy safe to read without holding a lock.
func (s *Store) DirectorsFor(ticker string) []*entitygraph.PersonNode {
	ticker = norm(ticker)
	s.mu.RLock()
	src := s.dirByTicker[ticker]
	cp := make([]*entitygraph.PersonNode, len(src))
	copy(cp, src)
	s.mu.RUnlock()
	return cp
}

// AuditorFor returns the latest AuditorRecord for a ticker, if any.
func (s *Store) AuditorFor(ticker string) (*entitygraph.AuditorRecord, bool) {
	ticker = norm(ticker)
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.auditors[ticker]
	return a, ok
}

// AllNodes returns all PersonNodes as a slice. Safe to iterate; callers must not mutate.
func (s *Store) AllNodes() []*entitygraph.PersonNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*entitygraph.PersonNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, n)
	}
	return out
}

// EdgesFor returns all edges involving canonicalID (as source or target).
func (s *Store) EdgesFor(canonicalID string) []*entitygraph.Edge {
	s.mu.RLock()
	src := s.edgesByNode[canonicalID]
	cp := make([]*entitygraph.Edge, len(src))
	copy(cp, src)
	s.mu.RUnlock()
	return cp
}

// SignalsForPerson returns all signals whose Entity field canonicalizes to canonicalID.
func (s *Store) SignalsForPerson(canonicalID string) []entitygraph.Signal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []entitygraph.Signal
	for _, sig := range s.signals {
		if sig.Entity != "" && entitygraph.Canonicalize(sig.Entity) == canonicalID {
			out = append(out, sig)
		}
	}
	return out
}

// AllTickers returns every ticker that has at least one director or auditor record.
func (s *Store) AllTickers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool)
	for t := range s.dirByTicker {
		seen[t] = true
	}
	for t := range s.auditors {
		seen[t] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	return out
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
					logf("graphread refresh: %v", err)
				}
			}
		}
	}()
}

func norm(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
