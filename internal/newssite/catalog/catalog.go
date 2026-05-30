// Package catalog merges pipeline signals, governance signals, and source documents
// into a single in-memory per-ticker catalog. It is the shared backbone for
// /ticker/{symbol}, /tickers, /search, and /api/tickers.
package catalog

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

// TickerRow is one entry in the ticker catalog.
type TickerRow struct {
	Symbol         string
	SignalCount     int    // pipeline signals (from signalindex)
	GovSignals      int    // live entity-graph governance signals
	DocCount        int    // source documents
	DirectorCount   int    // distinct directors tracked
	Auditor         string // current auditor; "" if unknown
	MaxSeverity     string // highest live gov signal: critical|high|medium|low|""
	LatestActivity  time.Time
	Forms           []string // distinct SEC forms seen

	critHighCount int // for internal ranking
}

// SearchResult is one ranked ticker from a search query.
type SearchResult struct {
	Symbol         string
	SignalCount     int
	MaxSeverity     string
	LatestActivity  time.Time
	Tier            int // 1=exact, 2=prefix, 3=substring
}

// Catalog is a live, refreshable view of all covered tickers.
// Build populates it; all read methods are safe for concurrent use.
type Catalog struct {
	mu     sync.RWMutex
	rows   map[string]*TickerRow
	sorted []*TickerRow // by activity desc
	alpha  []*TickerRow // alpha asc
}

// New returns an empty Catalog.
func New() *Catalog { return &Catalog{rows: make(map[string]*TickerRow)} }

// Build (re)constructs the catalog from the three live data sources.
// All three may be nil (graceful degradation).
// today is YYYY-MM-DD; governance signals with ValidThrough < today are excluded.
func (c *Catalog) Build(sigIdx *signalindex.Index, graph *graphread.Store, docIdx *docindex.Index, today string) {
	rows := make(map[string]*TickerRow)

	ensure := func(sym string) *TickerRow {
		sym = Normalize(sym)
		if sym == "" {
			return nil
		}
		r, ok := rows[sym]
		if !ok {
			r = &TickerRow{Symbol: sym}
			rows[sym] = r
		}
		return r
	}

	// Pipeline signals from signalindex.
	if sigIdx != nil {
		for _, sum := range sigIdx.Summary() {
			r := ensure(sum.Ticker)
			if r == nil {
				continue
			}
			r.SignalCount = sum.SignalCount
			if sum.LatestSignal.After(r.LatestActivity) {
				r.LatestActivity = sum.LatestSignal
			}
		}
	}

	// Governance signals from graphread.
	if graph != nil {
		for _, sig := range graph.AllSignals() {
			if sig.ValidThrough != "" && sig.ValidThrough < today {
				continue
			}
			r := ensure(sig.Ticker)
			if r == nil {
				continue
			}
			r.GovSignals++
			if sevRank(string(sig.Severity)) > sevRank(r.MaxSeverity) {
				r.MaxSeverity = string(sig.Severity)
			}
			if sig.Severity == "critical" || sig.Severity == "high" {
				r.critHighCount++
			}
		}
		// Per-ticker: director count + auditor.
		for sym := range rows {
			rows[sym].DirectorCount = len(graph.DirectorsFor(sym))
			if aud, ok := graph.AuditorFor(sym); ok {
				rows[sym].Auditor = aud.Auditor
			}
		}
	}

	// Source documents from docindex.
	if docIdx != nil {
		formsSeen := make(map[string]map[string]bool)
		for _, ds := range docIdx.AllSummaries() {
			r := ensure(ds.Ticker)
			if r == nil {
				continue
			}
			r.DocCount++
			if ds.PersistedAt.After(r.LatestActivity) {
				r.LatestActivity = ds.PersistedAt
			}
			if ds.Form != "" {
				if formsSeen[ds.Ticker] == nil {
					formsSeen[ds.Ticker] = make(map[string]bool)
				}
				formsSeen[ds.Ticker][ds.Form] = true
			}
		}
		for sym, fset := range formsSeen {
			sym = Normalize(sym)
			if r, ok := rows[sym]; ok {
				forms := make([]string, 0, len(fset))
				for f := range fset {
					forms = append(forms, f)
				}
				sort.Strings(forms)
				r.Forms = forms
			}
		}
		// Any ticker with docs but no signals gets a director/auditor lookup too.
		if graph != nil {
			for sym := range rows {
				if rows[sym].DirectorCount == 0 {
					rows[sym].DirectorCount = len(graph.DirectorsFor(sym))
				}
				if rows[sym].Auditor == "" {
					if aud, ok := graph.AuditorFor(sym); ok {
						rows[sym].Auditor = aud.Auditor
					}
				}
			}
		}
	}

	// Build sorted slices.
	all := make([]*TickerRow, 0, len(rows))
	for _, r := range rows {
		all = append(all, r)
	}

	sorted := make([]*TickerRow, len(all))
	copy(sorted, all)
	sort.Slice(sorted, func(i, j int) bool {
		ri, rj := sorted[i], sorted[j]
		si := ri.SignalCount + ri.GovSignals
		sj := rj.SignalCount + rj.GovSignals
		if si != sj {
			return si > sj
		}
		if !ri.LatestActivity.Equal(rj.LatestActivity) {
			return ri.LatestActivity.After(rj.LatestActivity)
		}
		return ri.Symbol < rj.Symbol
	})

	alpha := make([]*TickerRow, len(all))
	copy(alpha, all)
	sort.Slice(alpha, func(i, j int) bool { return alpha[i].Symbol < alpha[j].Symbol })

	c.mu.Lock()
	c.rows = rows
	c.sorted = sorted
	c.alpha = alpha
	c.mu.Unlock()
}

// Lookup returns the TickerRow for a symbol (normalized), or nil if unknown.
func (c *Catalog) Lookup(symbol string) *TickerRow {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rows[Normalize(symbol)]
}

// Known reports whether a symbol is in the catalog.
func (c *Catalog) Known(symbol string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.rows[Normalize(symbol)]
	return ok
}

// Directory returns all rows. If byAlpha is true they are sorted alphabetically;
// otherwise by activity (signal count desc, then latest activity desc).
func (c *Catalog) Directory(byAlpha bool) []*TickerRow {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if byAlpha {
		cp := make([]*TickerRow, len(c.alpha))
		copy(cp, c.alpha)
		return cp
	}
	cp := make([]*TickerRow, len(c.sorted))
	copy(cp, c.sorted)
	return cp
}

// AllSymbols returns every known symbol in alphabetical order (for <datalist>).
func (c *Catalog) AllSymbols() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	syms := make([]string, 0, len(c.rows))
	for sym := range c.rows {
		syms = append(syms, sym)
	}
	sort.Strings(syms)
	return syms
}

// Search returns ranked results for needle q.
// Tier 1 = exact match, Tier 2 = prefix, Tier 3 = substring.
// Within a tier results are sorted by total signal count desc, then latest activity desc.
// Empty q returns nil.
func (c *Catalog) Search(q string) []SearchResult {
	needle := Normalize(q)
	if needle == "" {
		return nil
	}
	c.mu.RLock()
	rows := c.rows
	c.mu.RUnlock()

	type cand struct {
		row  *TickerRow
		tier int
	}
	var exact, prefix, sub []cand
	for sym, row := range rows {
		switch {
		case sym == needle:
			exact = append(exact, cand{row, 1})
		case strings.HasPrefix(sym, needle):
			prefix = append(prefix, cand{row, 2})
		case strings.Contains(sym, needle):
			sub = append(sub, cand{row, 3})
		}
	}

	rankInTier := func(cs []cand) {
		sort.Slice(cs, func(i, j int) bool {
			ri, rj := cs[i].row, cs[j].row
			si := ri.SignalCount + ri.GovSignals
			sj := rj.SignalCount + rj.GovSignals
			if si != sj {
				return si > sj
			}
			if !ri.LatestActivity.Equal(rj.LatestActivity) {
				return ri.LatestActivity.After(rj.LatestActivity)
			}
			return ri.Symbol < rj.Symbol
		})
	}
	rankInTier(prefix)
	rankInTier(sub)

	all := append(append(exact, prefix...), sub...)
	out := make([]SearchResult, 0, len(all))
	for _, cd := range all {
		out = append(out, SearchResult{
			Symbol:        cd.row.Symbol,
			SignalCount:   cd.row.SignalCount + cd.row.GovSignals,
			MaxSeverity:   cd.row.MaxSeverity,
			LatestActivity: cd.row.LatestActivity,
			Tier:          cd.tier,
		})
	}
	return out
}

// NearestMatches returns up to n symbol suggestions for an unknown query.
func (c *Catalog) NearestMatches(q string, n int) []string {
	results := c.Search(q)
	out := make([]string, 0, n)
	for _, r := range results {
		if len(out) >= n {
			break
		}
		out = append(out, r.Symbol)
	}
	return out
}

// Normalize is the canonical ticker normalizer:
// upper-case and trim whitespace. Does NOT strip punctuation so BRK.B survives.
func Normalize(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// sevRank converts a severity string to a numeric rank for MaxSeverity tracking.
func sevRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
