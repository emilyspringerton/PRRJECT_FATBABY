package graphread

import (
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// Store holds an in-memory snapshot of entity-graph data, refreshed periodically.
type Store struct {
	mu      sync.RWMutex
	signals []entitygraph.Signal
	nodes   map[string]*entitygraph.PersonNode
	dir     string
}

// NewStore creates a Store pointed at dir (e.g. var/entity-graph).
func NewStore(dir string) *Store {
	return &Store{dir: dir, nodes: make(map[string]*entitygraph.PersonNode)}
}

// Refresh reloads signals and nodes from disk. Safe to call concurrently.
func (s *Store) Refresh() error {
	sigs, err := entitygraph.LoadSignals(s.dir)
	if err != nil {
		return err
	}
	g := entitygraph.NewGraph()
	if err := g.LoadNodesFromDir(s.dir); err != nil {
		return err
	}
	s.mu.Lock()
	s.signals = sigs
	s.nodes = g.Nodes
	s.mu.Unlock()
	return nil
}

// LiveSignals returns unexpired signals. If ticker is non-empty, only signals
// for that ticker are returned. today must be YYYY-MM-DD.
func (s *Store) LiveSignals(ticker, today string) []entitygraph.Signal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []entitygraph.Signal
	for _, sig := range s.signals {
		if sig.ValidThrough != "" && sig.ValidThrough < today {
			continue
		}
		if ticker != "" && sig.Ticker != ticker {
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

// StartRefresh launches a background goroutine that calls Refresh every interval,
// logging errors with logf. Stops when done is closed.
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
