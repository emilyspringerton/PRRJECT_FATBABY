package earningscal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store is an in-memory earnings calendar backed by an append-only NDJSON file.
// The canonical file is <dir>/dates.ndjson; the latest record per ID wins.
type Store struct {
	mu       sync.RWMutex
	byID     map[string]*EarningsDate // dedup: latest record per ID wins
	dir      string
}

// NewStore creates a Store that reads from dir (e.g. "var/earnings-calendar").
func NewStore(dir string) *Store {
	return &Store{dir: dir, byID: make(map[string]*EarningsDate)}
}

// Refresh reloads dates.ndjson from disk.
func (s *Store) Refresh() error {
	path := filepath.Join(s.dir, "dates.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	byID := make(map[string]*EarningsDate)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 2<<20), 2<<20)
	for sc.Scan() {
		var d EarningsDate
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			continue
		}
		if d.ID == "" || d.Ticker == "" || d.ReportDate == "" {
			continue
		}
		if d.FiscalYear == 0 {
			// Known-bad legacy records (found live: 61 of them, all
			// "confirmed"/8k_filing — a since-fixed extraction bug stored
			// FiscalYear=0 when ExtractPeriod found a quarter label but no
			// explicit year in the filing text). A corrected record with a
			// real ID exists separately for each; the file is append-only
			// so these can't be deleted, only filtered on load.
			continue
		}
		// Last record per ID wins (newest append wins).
		byID[d.ID] = &d
	}
	if err := sc.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.byID = byID
	s.mu.Unlock()
	return nil
}

// Append writes one EarningsDate to dates.ndjson (append-only).
func (s *Store) Append(d EarningsDate) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "dates.ndjson"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if err != nil {
		return err
	}
	// Update in-memory index immediately.
	s.mu.Lock()
	s.byID[d.ID] = &d
	s.mu.Unlock()
	return nil
}

// All returns every EarningsDate sorted by ReportDate ascending.
func (s *Store) All() []*EarningsDate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*EarningsDate, 0, len(s.byID))
	for _, d := range s.byID {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReportDate < out[j].ReportDate
	})
	return out
}

// Query returns EarningsDates filtered by ticker, from/to date range and status.
// All filters are optional ("" = no filter).
func (s *Store) Query(tickers []string, from, to string, statuses []string) []*EarningsDate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tickerSet := toSet(tickers)
	statusSet := toSet(statuses)

	var out []*EarningsDate
	for _, d := range s.byID {
		if len(tickerSet) > 0 && !tickerSet[strings.ToUpper(d.Ticker)] {
			continue
		}
		if from != "" && d.ReportDate < from {
			continue
		}
		if to != "" && d.ReportDate > to {
			continue
		}
		if len(statusSet) > 0 && !statusSet[string(d.Status)] {
			continue
		}
		cp := *d
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReportDate < out[j].ReportDate
	})
	return out
}

// Upcoming returns at most n EarningsDates with ReportDate >= today, soonest first.
func (s *Store) Upcoming(today string, n int) []*EarningsDate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*EarningsDate
	for _, d := range s.byID {
		if d.ReportDate >= today {
			cp := *d
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReportDate < out[j].ReportDate
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// GetByID returns the EarningsDate for the given ID, or nil if not found.
func (s *Store) GetByID(id string) *EarningsDate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d := s.byID[id]
	if d == nil {
		return nil
	}
	cp := *d
	return &cp
}

// Count returns the total number of distinct earnings date records.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
}

// StatusPriority returns a numeric priority for dedup decisions.
// Higher = more trusted; only overwrite if new record has equal or higher priority.
func StatusPriority(s Status) int {
	switch s {
	case StatusConfirmed:
		return 3
	case StatusAnnounced:
		return 2
	case StatusBackfilled:
		return 1
	default:
		return 0
	}
}

func toSet(ss []string) map[string]bool {
	if len(ss) == 0 {
		return nil
	}
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
