package secwatch

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Watchlist struct {
	Entries []WatchEntry `json:"entries"`
}

type WatchEntry struct {
	Ticker       string   `json:"ticker"`
	CompanyName  string   `json:"company_name,omitempty"`
	CIK          string   `json:"cik"`
	AllowedForms []string `json:"allowed_forms"`
	Enabled      bool     `json:"enabled"`
	PollPrio     int      `json:"poll_priority,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

func LoadWatchlist(path string) (Watchlist, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Watchlist{}, fmt.Errorf("read watchlist %q: %w", path, err)
	}
	var out Watchlist
	if err := json.Unmarshal(b, &out); err != nil {
		return Watchlist{}, fmt.Errorf("decode watchlist %q: %w", path, err)
	}
	for i := range out.Entries {
		entry := &out.Entries[i]
		entry.Ticker = strings.ToUpper(strings.TrimSpace(entry.Ticker))
		entry.CIK = NormalizeCIK(entry.CIK)
		entry.AllowedForms = normalizeForms(entry.AllowedForms)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		if out.Entries[i].PollPrio == out.Entries[j].PollPrio {
			return out.Entries[i].Ticker < out.Entries[j].Ticker
		}
		return out.Entries[i].PollPrio < out.Entries[j].PollPrio
	})
	return out, nil
}

func (w Watchlist) EnabledEntries() []WatchEntry {
	out := make([]WatchEntry, 0, len(w.Entries))
	for _, e := range w.Entries {
		if e.Enabled {
			out = append(out, e)
		}
	}
	return out
}

func normalizeForms(forms []string) []string {
	set := map[string]struct{}{}
	for _, f := range forms {
		f = strings.ToUpper(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		set[f] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
