package secfixtures

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FixtureUnit is a discovered fixture directory and a lightweight inventory
// of known files in that directory.
type FixtureUnit struct {
	AbsDir            string
	RelDir            string
	MetadataPath      string
	IndexPath         string
	AllFiles          []string
	RawCandidatePaths []string
}

// Metadata captures the filing-level fields currently used by v1 corpus tests.
type Metadata struct {
	AcceptanceDatetime string `json:"acceptance_datetime"`
	AccessionNumber    string `json:"accession_number"`
	CIK                string `json:"cik"`
	FilingDate         string `json:"filing_date"`
	Form               string `json:"form"`
	PrimaryDocument    string `json:"primary_document"`
	Ticker             string `json:"ticker"`
}

// IndexData is a normalized view of directory.item entries from SEC index.json.
type IndexData struct {
	Items []IndexItem
}

type IndexItem struct {
	Name string
	Type string
	Size string
}

func DiscoverFixtureUnits(fixturesRoot string) ([]FixtureUnit, error) {
	absRoot, err := filepath.Abs(fixturesRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve fixtures root %q: %w", fixturesRoot, err)
	}

	dirFiles := map[string][]string{}
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		dir := filepath.Dir(path)
		dirFiles[dir] = append(dirFiles[dir], path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk fixtures root %q: %w", absRoot, err)
	}

	dirs := make([]string, 0, len(dirFiles))
	for dir := range dirFiles {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	units := make([]FixtureUnit, 0, len(dirs))
	for _, dir := range dirs {
		files := dirFiles[dir]
		sort.Strings(files)

		unit := FixtureUnit{
			AbsDir:   dir,
			AllFiles: append([]string{}, files...),
		}

		rel, relErr := filepath.Rel(absRoot, dir)
		if relErr != nil {
			unit.RelDir = dir
		} else {
			unit.RelDir = filepath.ToSlash(rel)
		}

		for _, file := range files {
			name := strings.ToLower(filepath.Base(file))
			switch name {
			case "metadata.json":
				unit.MetadataPath = file
			case "index.json":
				unit.IndexPath = file
			}

			if isRawCandidate(file) {
				unit.RawCandidatePaths = append(unit.RawCandidatePaths, file)
			}
		}
		sort.Strings(unit.RawCandidatePaths)
		units = append(units, unit)
	}

	return units, nil
}

func isRawCandidate(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	if strings.HasSuffix(name, ".json") {
		return false
	}

	switch ext {
	case ".htm", ".html", ".xml", ".txt", ".xsd":
		return true
	default:
		return false
	}
}

func LoadMetadata(path string) (Metadata, map[string]json.RawMessage, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, nil, fmt.Errorf("read metadata %q: %w", path, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return Metadata{}, nil, fmt.Errorf("parse metadata json %q: %w", path, err)
	}

	var metadata Metadata
	if err := json.Unmarshal(bytes, &metadata); err != nil {
		return Metadata{}, nil, fmt.Errorf("decode metadata fields %q: %w", path, err)
	}

	return metadata, raw, nil
}

func ValidateMetadataCore(m Metadata, raw map[string]json.RawMessage) error {
	var issues []string

	requireIfPresent := func(key, value string) {
		if _, ok := raw[key]; ok && strings.TrimSpace(value) == "" {
			issues = append(issues, fmt.Sprintf("%s present but empty", key))
		}
	}

	requireIfPresent("form", m.Form)
	requireIfPresent("accession_number", m.AccessionNumber)
	requireIfPresent("cik", m.CIK)
	requireIfPresent("filing_date", m.FilingDate)
	requireIfPresent("primary_document", m.PrimaryDocument)

	if len(raw) > 0 {
		if strings.TrimSpace(m.Form) == "" {
			issues = append(issues, "missing form")
		}
		if strings.TrimSpace(m.AccessionNumber) == "" {
			issues = append(issues, "missing accession_number")
		}
		if strings.TrimSpace(m.CIK) == "" {
			issues = append(issues, "missing cik")
		}
		if strings.TrimSpace(m.FilingDate) == "" {
			issues = append(issues, "missing filing_date")
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf(strings.Join(issues, "; "))
	}
	return nil
}

func LoadIndex(path string) (IndexData, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return IndexData{}, fmt.Errorf("read index %q: %w", path, err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(bytes, &root); err != nil {
		return IndexData{}, fmt.Errorf("parse index json %q: %w", path, err)
	}

	directoryRaw, ok := root["directory"]
	if !ok {
		return IndexData{}, fmt.Errorf("missing directory object")
	}

	var directory map[string]json.RawMessage
	if err := json.Unmarshal(directoryRaw, &directory); err != nil {
		return IndexData{}, fmt.Errorf("decode directory object: %w", err)
	}

	itemRaw, ok := directory["item"]
	if !ok {
		return IndexData{}, fmt.Errorf("missing directory.item")
	}

	items, err := decodeIndexItems(itemRaw)
	if err != nil {
		return IndexData{}, err
	}

	return IndexData{Items: items}, nil
}

func decodeIndexItems(raw json.RawMessage) ([]IndexItem, error) {
	var many []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &many); err == nil {
		return normalizeIndexItems(many), nil
	}

	var one map[string]json.RawMessage
	if err := json.Unmarshal(raw, &one); err == nil {
		return normalizeIndexItems([]map[string]json.RawMessage{one}), nil
	}

	return nil, fmt.Errorf("unable to decode directory.item as object or array")
}

func normalizeIndexItems(rawItems []map[string]json.RawMessage) []IndexItem {
	items := make([]IndexItem, 0, len(rawItems))
	for _, rawItem := range rawItems {
		var item IndexItem
		_ = decodeJSONString(rawItem["name"], &item.Name)
		_ = decodeJSONString(rawItem["type"], &item.Type)
		_ = decodeJSONString(rawItem["size"], &item.Size)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

func decodeJSONString(raw json.RawMessage, out *string) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
