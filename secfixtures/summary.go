package secfixtures

import (
	"path/filepath"
	"sort"
	"strings"
)

type CorpusSummary struct {
	TotalUnits            int
	WithMetadata          int
	WithIndex             int
	WithBoth              int
	WithPrimaryCandidates int
	Forms                 map[string]int
	ByIssuer              map[string]int
	ByDocClass            map[string]int
}

func BuildCorpusSummary(units []FixtureUnit) (CorpusSummary, error) {
	s := CorpusSummary{
		TotalUnits: len(units),
		Forms:      map[string]int{},
		ByIssuer:   map[string]int{},
		ByDocClass: map[string]int{},
	}

	for _, u := range units {
		s.ByIssuer[u.IssuerDir]++
		if u.MetadataPath != "" {
			s.WithMetadata++
			m, err := LoadMetadata(u.MetadataPath)
			if err != nil {
				return CorpusSummary{}, err
			}
			if form := strings.TrimSpace(m.Form); form != "" {
				s.Forms[form]++
			}
		}
		if u.IndexPath != "" {
			s.WithIndex++
		}
		if u.MetadataPath != "" && u.IndexPath != "" {
			s.WithBoth++
		}
		if len(u.PrimaryCandidatePaths) > 0 {
			s.WithPrimaryCandidates++
		}

		for _, docPath := range u.PrimaryCandidatePaths {
			class, _, err := ReadAndClassifyDocument(docPath)
			if err != nil {
				return CorpusSummary{}, err
			}
			key := class.Kind
			if key == "" {
				key = "unknown"
			}
			s.ByDocClass[key]++
			if class.InlineXBRL {
				s.ByDocClass["likely_inline_xbrl"]++
			}
			if class.FilingLike {
				s.ByDocClass["likely_filing_document"]++
			}
			name := strings.ToLower(filepath.Base(docPath))
			if name == "filingsummary.xml" || name == "metalink.json" || name == "metallinks.json" {
				s.ByDocClass["interesting_companion_primary_candidate"]++
			}
		}
	}

	return s, nil
}

func SortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
