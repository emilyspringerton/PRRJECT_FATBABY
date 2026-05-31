package entitygraph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NodeType classifies an entity in the graph.
type NodeType string

const (
	NodeDirector  NodeType = "director"
	NodeExecutive NodeType = "executive"
	NodeAuditor   NodeType = "auditor"
	NodeSignatory NodeType = "signatory"
)

// FilingAppearance records one appearance of a person in a specific filing.
type FilingAppearance struct {
	Ticker         string  `json:"ticker"`
	CIK            string  `json:"cik"`
	Form           string  `json:"form"`
	FilingDate     string  `json:"filing_date"`
	ApprovalPct    float64 `json:"approval_pct,omitempty"`
	ForVotes       int64   `json:"for_votes,omitempty"`
	AgainstVotes   int64   `json:"against_votes,omitempty"`
	AbstainVotes   int64   `json:"abstain_votes,omitempty"`
	BrokerNonVotes int64   `json:"broker_non_votes,omitempty"`
}

// PersonNode is a vertex in the entity graph representing a named person.
type PersonNode struct {
	CanonicalID     string             `json:"canonical_id"`
	Name            string             `json:"name"`
	Type            NodeType           `json:"type"`
	FirstAppearance string             `json:"first_appearance"`
	LastAppearance  string             `json:"last_appearance"`
	FilingCount     int                `json:"filing_count"`
	// Centrality is the number of distinct companies (tickers) this director
	// appears at. A director with Centrality >= 3 is a "bridge node" — their
	// friction signals propagate risk across multiple companies.
	Centrality int                `json:"centrality"`
	Filings    []FilingAppearance `json:"filings"`
}

// EdgeType classifies a relationship between two entities.
type EdgeType string

const (
	EdgeBoardCoMember EdgeType = "board_co_member"
)

// Edge is a relationship between two entity nodes.
type Edge struct {
	Source    string            `json:"source"`
	Target    string            `json:"target"`
	EdgeType  EdgeType          `json:"edge_type"`
	Companies []string          `json:"companies"`
	Strength  int               `json:"strength"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// AuditorRecord tracks the most recently observed public accounting firm for a ticker.
// Stored as NDJSON in var/entity-graph/auditors.ndjson; the last record per ticker wins.
type AuditorRecord struct {
	Ticker     string `json:"ticker"`
	Auditor    string `json:"auditor"`
	FilingDate string `json:"filing_date"`
}

// Graph holds all nodes and edges in memory and can flush to NDJSON files.
type Graph struct {
	Nodes    map[string]*PersonNode
	Edges    map[string]*Edge
	Auditors map[string]*AuditorRecord // keyed by ticker
}

// NewGraph creates an empty graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes:    make(map[string]*PersonNode),
		Edges:    make(map[string]*Edge),
		Auditors: make(map[string]*AuditorRecord),
	}
}

// TrackAuditor records the auditor for a ticker. If the auditor differs from the
// previously recorded one, it returns (true, prevAuditorName). If this is the first
// record or no change, returns (false, "").
func (g *Graph) TrackAuditor(ticker, auditor, filingDate string) (changed bool, prev string) {
	if auditor == "" {
		return false, ""
	}
	existing, ok := g.Auditors[ticker]
	if ok && existing.Auditor != auditor {
		prev = existing.Auditor
		changed = true
	}
	g.Auditors[ticker] = &AuditorRecord{Ticker: ticker, Auditor: auditor, FilingDate: filingDate}
	return changed, prev
}

// LoadAuditorsFromDir reads auditor.ndjson and populates Auditors (last record per ticker wins).
func (g *Graph) LoadAuditorsFromDir(dir string) error {
	path := filepath.Join(dir, "auditors.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec AuditorRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		g.Auditors[rec.Ticker] = &rec
	}
	return sc.Err()
}

// FlushAuditors appends new/updated auditor records to auditors.ndjson.
func (g *Graph) FlushAuditors(dir string) error {
	return appendNDJSON(filepath.Join(dir, "auditors.ndjson"), func(enc *json.Encoder) error {
		for _, rec := range g.Auditors {
			if err := enc.Encode(rec); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpsertPerson adds or updates a person node given a new filing appearance.
func (g *Graph) UpsertPerson(name string, nodeType NodeType, app FilingAppearance) *PersonNode {
	id := Canonicalize(name)
	node, exists := g.Nodes[id]
	if !exists {
		node = &PersonNode{
			CanonicalID:     id,
			Name:            name,
			Type:            nodeType,
			FirstAppearance: app.FilingDate,
			LastAppearance:  app.FilingDate,
		}
		g.Nodes[id] = node
	}
	// Deduplicate by accession (ticker+date is close enough for Phase 1).
	for _, f := range node.Filings {
		if f.Ticker == app.Ticker && f.FilingDate == app.FilingDate && f.Form == app.Form {
			return node
		}
	}
	node.Filings = append(node.Filings, app)
	node.FilingCount = len(node.Filings)
	if app.FilingDate < node.FirstAppearance {
		node.FirstAppearance = app.FilingDate
	}
	if app.FilingDate > node.LastAppearance {
		node.LastAppearance = app.FilingDate
	}
	// Recompute centrality: distinct tickers across all filings.
	seen := map[string]bool{}
	for _, f := range node.Filings {
		seen[f.Ticker] = true
	}
	node.Centrality = len(seen)
	return node
}

// AddEdge creates or strengthens a co-appearance edge between two people.
func (g *Graph) AddEdge(srcID, tgtID string, edgeType EdgeType, company string) {
	// Normalise key so A→B and B→A don't duplicate.
	key := srcID + "|" + tgtID
	if srcID > tgtID {
		key = tgtID + "|" + srcID
	}
	e, exists := g.Edges[key]
	if !exists {
		e = &Edge{
			Source:   srcID,
			Target:   tgtID,
			EdgeType: edgeType,
			Strength: 0,
		}
		g.Edges[key] = e
	}
	e.Strength++
	for _, c := range e.Companies {
		if c == company {
			return
		}
	}
	e.Companies = append(e.Companies, company)
}

// BuildEdgesFromFiling creates board_co_member edges between all directors
// who appeared together in a single filing.
func (g *Graph) BuildEdgesFromFiling(directors []string, ticker string) {
	for i := 0; i < len(directors); i++ {
		for j := i + 1; j < len(directors); j++ {
			g.AddEdge(directors[i], directors[j], EdgeBoardCoMember, ticker)
		}
	}
}

// FlushNodes writes all nodes to <dir>/nodes.ndjson (append mode).
func (g *Graph) FlushNodes(dir string) error {
	return appendNDJSON(filepath.Join(dir, "nodes.ndjson"), func(enc *json.Encoder) error {
		for _, node := range g.Nodes {
			if err := enc.Encode(node); err != nil {
				return err
			}
		}
		return nil
	})
}

// FlushEdges writes all edges to <dir>/edges.ndjson (append mode).
func (g *Graph) FlushEdges(dir string) error {
	return appendNDJSON(filepath.Join(dir, "edges.ndjson"), func(enc *json.Encoder) error {
		for _, edge := range g.Edges {
			if err := enc.Encode(edge); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadEdgesFromDir reads all Edge records from <dir>/edges.ndjson into g.
func (g *Graph) LoadEdgesFromDir(dir string) error {
	path := filepath.Join(dir, "edges.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var e Edge
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		key := e.Source + "|" + e.Target
		if e.Source > e.Target {
			key = e.Target + "|" + e.Source
		}
		g.Edges[key] = &e
	}
	return sc.Err()
}

// appendNDJSON opens path in append+create mode and calls fn with an encoder.
func appendNDJSON(path string, fn func(*json.Encoder) error) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return fn(json.NewEncoder(f))
}

// LoadNodesFromDir reads all PersonNode records from <dir>/nodes.ndjson into g.
// Existing in-memory nodes are overwritten by the loaded values.
func (g *Graph) LoadNodesFromDir(dir string) error {
	path := filepath.Join(dir, "nodes.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var node PersonNode
		if err := json.Unmarshal(sc.Bytes(), &node); err != nil {
			continue
		}
		g.Nodes[node.CanonicalID] = &node
	}
	return sc.Err()
}

// LoadSignals reads all Signal records from <dir>/signals.ndjson.
// Used to supply historical context for composite signal scoring.
func LoadSignals(dir string) ([]Signal, error) {
	path := filepath.Join(dir, "signals.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var signals []Signal
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var s Signal
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			continue
		}
		signals = append(signals, s)
	}
	return signals, sc.Err()
}

// CompactNodes rewrites <dir>/nodes.ndjson in place, keeping only the most
// recent record per canonical_id. The append-only store accumulates duplicate
// node records across runs; compaction keeps the file from growing unboundedly.
// Call this periodically (e.g. after every 100 batches, or on startup).
func CompactNodes(dir string) error {
	path := filepath.Join(dir, "nodes.ndjson")
	nodes, err := func() (map[string]*PersonNode, error) {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		defer f.Close()
		m := map[string]*PersonNode{}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4<<20), 4<<20)
		for sc.Scan() {
			var n PersonNode
			if json.Unmarshal(sc.Bytes(), &n) == nil {
				m[n.CanonicalID] = &n
			}
		}
		return m, sc.Err()
	}()
	if err != nil || len(nodes) == 0 {
		return err
	}
	tmp := path + ".compact"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create compact file: %w", err)
	}
	enc := json.NewEncoder(f)
	for _, n := range nodes {
		if err := enc.Encode(n); err != nil {
			f.Close()
			return err
		}
	}
	f.Close()
	return os.Rename(tmp, path)
}

// WriteSignals appends a slice of signals to <dir>/signals.ndjson.
func WriteSignals(dir string, signals []Signal) error {
	return appendNDJSON(filepath.Join(dir, "signals.ndjson"), func(enc *json.Encoder) error {
		for i := range signals {
			if err := enc.Encode(&signals[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// RewriteSignals atomically replaces <dir>/signals.ndjson with the given slice.
// Used by backfill tooling to patch data without duplicating records.
func RewriteSignals(dir string, signals []Signal) error {
	path := filepath.Join(dir, "signals.ndjson")
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	for i := range signals {
		if err := enc.Encode(&signals[i]); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// nowDate returns the current UTC date in YYYY-MM-DD format.
func nowDate() string {
	return time.Now().UTC().Format("2006-01-02")
}
