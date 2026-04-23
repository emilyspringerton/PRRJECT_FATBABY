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

// FixtureUnit represents one filing fixture unit rooted at fixtures/<issuer>/<filing>/.
type FixtureUnit struct {
	RootDir                 string
	RelDir                  string
	IssuerDir               string
	FilingDir               string
	MetadataPath            string
	IndexPath               string
	AllFiles                []string // absolute, deterministic
	FilesRelativeToUnitRoot []string // deterministic
	PrimaryCandidatePaths   []string // absolute, deterministic
	CompanionPaths          []string // absolute, deterministic
}

// Metadata captures the filing-level fields currently used by corpus tests.
type Metadata struct {
	Ticker                string `json:"ticker"`
	CIK                   string `json:"cik"`
	AccessionNumber       string `json:"accession_number"`
	FilingDate            string `json:"filing_date"`
	Form                  string `json:"form"`
	PrimaryDocument       string `json:"primary_document"`
	PrimaryDocumentURL    string `json:"primary_document_url"`
	FilingIndexURL        string `json:"filing_index_url"`
	AcceptanceDatetime    string `json:"acceptance_datetime"`
	PrimaryDocDescription string `json:"primary_doc_description"`
}

// IndexData is a normalized view of directory data from SEC index.json.
type IndexData struct {
	Directory IndexDirectory
}

type IndexDirectory struct {
	Name      string
	ParentDir string
	Items     []IndexItem
}

type IndexItem struct {
	Name         string
	Type         string
	Size         string
	LastModified string
}

func DiscoverFixtureUnits(fixturesRoot string) ([]FixtureUnit, error) {
	absRoot, err := filepath.Abs(fixturesRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve fixtures root %q: %w", fixturesRoot, err)
	}

	type agg struct {
		root      string
		issuerDir string
		filingDir string
		files     []string
	}
	unitsByRel := map[string]*agg{}

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			return fmt.Errorf("compute rel path for %q: %w", path, relErr)
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		if len(parts) < 3 {
			// e.g. fixtures/manifest.json or fixtures/<issuer>/manifest.json
			return nil
		}

		key := strings.Join(parts[:2], "/")
		item := unitsByRel[key]
		if item == nil {
			item = &agg{
				root:      filepath.Join(absRoot, parts[0], parts[1]),
				issuerDir: parts[0],
				filingDir: parts[1],
			}
			unitsByRel[key] = item
		}
		item.files = append(item.files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk fixtures root %q: %w", absRoot, err)
	}

	relKeys := make([]string, 0, len(unitsByRel))
	for key := range unitsByRel {
		relKeys = append(relKeys, key)
	}
	sort.Strings(relKeys)

	units := make([]FixtureUnit, 0, len(relKeys))
	for _, relKey := range relKeys {
		a := unitsByRel[relKey]
		sort.Strings(a.files)

		u := FixtureUnit{
			RootDir:   a.root,
			RelDir:    relKey,
			IssuerDir: a.issuerDir,
			FilingDir: a.filingDir,
			AllFiles:  append([]string{}, a.files...),
		}

		for _, absFile := range a.files {
			relFile, relErr := filepath.Rel(u.RootDir, absFile)
			if relErr != nil {
				relFile = filepath.Base(absFile)
			}
			relFile = filepath.ToSlash(relFile)
			u.FilesRelativeToUnitRoot = append(u.FilesRelativeToUnitRoot, relFile)

			baseLower := strings.ToLower(filepath.Base(absFile))
			switch baseLower {
			case "metadata.json":
				u.MetadataPath = absFile
				continue
			case "index.json":
				u.IndexPath = absFile
				continue
			}

			if isPrimaryCandidate(absFile) {
				u.PrimaryCandidatePaths = append(u.PrimaryCandidatePaths, absFile)
			} else {
				u.CompanionPaths = append(u.CompanionPaths, absFile)
			}
		}

		sort.Strings(u.FilesRelativeToUnitRoot)
		sort.Strings(u.PrimaryCandidatePaths)
		sort.Strings(u.CompanionPaths)
		units = append(units, u)
	}

	return units, nil
}

func isPrimaryCandidate(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".htm", ".html", ".xml", ".txt":
		return true
	default:
		return false
	}
}

func LoadMetadata(path string) (Metadata, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("read metadata %q: %w", path, err)
	}

	var m Metadata
	if err := json.Unmarshal(bytes, &m); err != nil {
		return Metadata{}, fmt.Errorf("decode metadata %q: %w", path, err)
	}
	return m, nil
}

func LoadIndex(path string) (IndexData, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return IndexData{}, fmt.Errorf("read index %q: %w", path, err)
	}

	var rawRoot map[string]json.RawMessage
	if err := json.Unmarshal(bytes, &rawRoot); err != nil {
		return IndexData{}, fmt.Errorf("parse index json %q: %w", path, err)
	}

	dirRaw, ok := rawRoot["directory"]
	if !ok {
		return IndexData{}, fmt.Errorf("index %q missing directory object", path)
	}

	var rawDir map[string]json.RawMessage
	if err := json.Unmarshal(dirRaw, &rawDir); err != nil {
		return IndexData{}, fmt.Errorf("index %q decode directory object: %w", path, err)
	}

	var out IndexData
	_ = decodeString(rawDir["name"], &out.Directory.Name)
	_ = decodeString(rawDir["parent-dir"], &out.Directory.ParentDir)

	itemRaw, ok := rawDir["item"]
	if !ok {
		return IndexData{}, fmt.Errorf("index %q missing directory.item", path)
	}

	items, err := decodeIndexItems(itemRaw)
	if err != nil {
		return IndexData{}, fmt.Errorf("index %q decode directory.item: %w", path, err)
	}
	out.Directory.Items = items
	return out, nil
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
		_ = decodeString(rawItem["name"], &item.Name)
		_ = decodeString(rawItem["type"], &item.Type)
		_ = decodeString(rawItem["size"], &item.Size)
		_ = decodeString(rawItem["last-modified"], &item.LastModified)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if strings.EqualFold(items[i].Name, items[j].Name) {
			return strings.ToLower(items[i].Type) < strings.ToLower(items[j].Type)
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

func decodeString(raw json.RawMessage, out *string) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err == nil {
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(raw, &num); err == nil {
		*out = num.String()
		return nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		*out = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", f), "0"), ".")
		return nil
	}
	return fmt.Errorf("decode json scalar as string")
}
