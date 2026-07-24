// Package companybios serves the one-shot draft company bios written to
// config/company_bios.json (EMILY/BACKLOG.md S166-03/S170-22) on ticker pages.
package companybios

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// Store is an in-memory, ticker-keyed map of bio text backed by a single
// flat JSON file: {"bios": {"AAPL": "...", ...}}.
type Store struct {
	mu   sync.RWMutex
	path string
	bios map[string]string
}

// NewStore creates a Store that reads from path (e.g. "config/company_bios.json").
func NewStore(path string) *Store {
	return &Store{path: path, bios: make(map[string]string)}
}

type fileShape struct {
	Bios map[string]string `json:"bios"`
}

// Refresh reloads the bio file from disk. Missing file is not an error --
// bios are an optional enrichment layer, not load-bearing data.
func (s *Store) Refresh() error {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var fs fileShape
	if err := json.Unmarshal(b, &fs); err != nil {
		return err
	}
	s.mu.Lock()
	s.bios = fs.Bios
	s.mu.Unlock()
	return nil
}

// Bio returns the bio text for ticker, or "" if none exists.
func (s *Store) Bio(ticker string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bios[strings.ToUpper(ticker)]
}

// Count returns the number of tickers with a bio loaded.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.bios)
}
