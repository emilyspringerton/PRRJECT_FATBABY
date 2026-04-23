package secfixtures_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/example/prrject-fatbaby/secfixtures"
)

func fixturesRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "fixtures")
}

func TestCorpusDiscovery_IsDeterministicAndNonEmpty(t *testing.T) {
	root := fixturesRoot(t)

	first, err := secfixtures.DiscoverFixtureUnits(root)
	if err != nil {
		t.Fatalf("discover units first pass: %v", err)
	}
	if len(first) == 0 {
		t.Fatalf("expected at least one fixture unit under %s", root)
	}

	second, err := secfixtures.DiscoverFixtureUnits(root)
	if err != nil {
		t.Fatalf("discover units second pass: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("determinism failure: first=%d second=%d", len(first), len(second))
	}

	for i := range first {
		a, b := first[i], second[i]
		if a.RelDir != b.RelDir {
			t.Fatalf("determinism failure at %d: %s != %s", i, a.RelDir, b.RelDir)
		}
		if strings.Join(a.FilesRelativeToUnitRoot, "|") != strings.Join(b.FilesRelativeToUnitRoot, "|") {
			t.Fatalf("determinism failure for files in %s", a.RelDir)
		}
		if a.IssuerDir == "" || a.FilingDir == "" {
			t.Fatalf("missing issuer/filing derivation for unit %s", a.RelDir)
		}
	}

	t.Logf("discovered %d fixture units under %s", len(first), root)
}

func TestCorpusMetadataJSON_IsLoadableAndHasCoreShape(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	metadataCount := 0
	for _, unit := range units {
		if unit.MetadataPath == "" {
			continue
		}
		metadataCount++

		if _, statErr := os.Stat(unit.MetadataPath); statErr != nil {
			t.Fatalf("metadata file missing for unit %s path %s: %v", unit.RelDir, unit.MetadataPath, statErr)
		}

		m, err := secfixtures.LoadMetadata(unit.MetadataPath)
		if err != nil {
			t.Fatalf("metadata load failed for %s: %v", unit.MetadataPath, err)
		}

		requireNonEmpty := func(label, value string) {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("metadata %s empty for unit %s (%s)", label, unit.RelDir, unit.MetadataPath)
			}
		}

		requireNonEmpty("accession_number", m.AccessionNumber)
		requireNonEmpty("cik", m.CIK)
		requireNonEmpty("filing_date", m.FilingDate)
		requireNonEmpty("form", m.Form)
		requireNonEmpty("ticker", m.Ticker)
		requireNonEmpty("primary_document", m.PrimaryDocument)
		requireNonEmpty("filing_index_url", m.FilingIndexURL)
		requireNonEmpty("primary_document_url", m.PrimaryDocumentURL)
	}

	if metadataCount == 0 {
		t.Fatalf("expected at least one metadata.json in corpus")
	}
}

func TestCorpusIndexJSON_IsLoadableAndDeterministic(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	indexCount := 0
	for _, unit := range units {
		if unit.IndexPath == "" {
			continue
		}
		indexCount++

		if _, statErr := os.Stat(unit.IndexPath); statErr != nil {
			t.Fatalf("index file missing for unit %s path %s: %v", unit.RelDir, unit.IndexPath, statErr)
		}

		idx, err := secfixtures.LoadIndex(unit.IndexPath)
		if err != nil {
			t.Fatalf("index load failed for %s: %v", unit.IndexPath, err)
		}
		if len(idx.Directory.Items) == 0 {
			t.Fatalf("index has empty directory.item for %s", unit.IndexPath)
		}

		names := make([]string, 0, len(idx.Directory.Items))
		for _, item := range idx.Directory.Items {
			if strings.TrimSpace(item.Name) == "" {
				t.Fatalf("index item has empty name in %s", unit.IndexPath)
			}
			names = append(names, strings.ToLower(item.Name))
		}
		sortedNames := append([]string{}, names...)
		sort.Strings(sortedNames)
		if strings.Join(names, "|") != strings.Join(sortedNames, "|") {
			t.Fatalf("index items should be deterministic/sorted after load for %s", unit.IndexPath)
		}
	}

	if indexCount == 0 {
		t.Fatalf("expected at least one index.json in corpus")
	}
}

func TestCorpusMetadataIndex_Relationships(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	registry := secfixtures.DefaultWeirdnessRegistry()
	withBoth := 0
	for _, unit := range units {
		if unit.MetadataPath == "" || unit.IndexPath == "" {
			continue
		}
		withBoth++

		m, err := secfixtures.LoadMetadata(unit.MetadataPath)
		if err != nil {
			t.Fatalf("metadata load failed for %s: %v", unit.MetadataPath, err)
		}
		idx, err := secfixtures.LoadIndex(unit.IndexPath)
		if err != nil {
			t.Fatalf("index load failed for %s: %v", unit.IndexPath, err)
		}

		if _, known := registry.ReasonForRelDir(unit.RelDir); !known {
			primary := strings.ToLower(strings.TrimSpace(m.PrimaryDocument))
			seen := false
			for _, item := range idx.Directory.Items {
				if strings.ToLower(item.Name) == primary {
					seen = true
					break
				}
			}
			if !seen {
				t.Fatalf("metadata primary_document %q not present in index items for unit %s", m.PrimaryDocument, unit.RelDir)
			}
		}

		if !strings.Contains(strings.ToLower(m.FilingIndexURL), strings.ToLower(strings.TrimLeft(m.CIK, "0"))) {
			t.Fatalf("filing_index_url does not include cik %q for unit %s url=%s", m.CIK, unit.RelDir, m.FilingIndexURL)
		}
		accessionNoDash := strings.ReplaceAll(strings.ToLower(m.AccessionNumber), "-", "")
		if !strings.Contains(strings.ToLower(m.FilingIndexURL), accessionNoDash) &&
			!strings.Contains(strings.ToLower(idx.Directory.Name), accessionNoDash) &&
			!strings.Contains(strings.ToLower(idx.Directory.ParentDir), accessionNoDash) {
			t.Fatalf("accession %q not found in filing_index_url/index directory fields for unit %s", m.AccessionNumber, unit.RelDir)
		}

		normalizedCIK := strings.TrimLeft(strings.ToLower(m.CIK), "0")
		if normalizedCIK != "" {
			if !strings.Contains(strings.ToLower(idx.Directory.Name), normalizedCIK) &&
				!strings.Contains(strings.ToLower(idx.Directory.ParentDir), normalizedCIK) {
				t.Fatalf("index directory fields do not appear to align with cik %q for unit %s", m.CIK, unit.RelDir)
			}
		}
	}
	if withBoth == 0 {
		t.Fatalf("expected at least one fixture with both metadata and index")
	}
}

func TestCorpusOnDiskVsIndex_Relationships(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	for _, unit := range units {
		diskSet := map[string]bool{}
		for _, relFile := range unit.FilesRelativeToUnitRoot {
			diskSet[strings.ToLower(relFile)] = true
			diskSet[strings.ToLower(filepath.Base(relFile))] = true
		}

		if unit.MetadataPath != "" {
			m, err := secfixtures.LoadMetadata(unit.MetadataPath)
			if err != nil {
				t.Fatalf("metadata load failed for %s: %v", unit.MetadataPath, err)
			}
			if strings.TrimSpace(m.PrimaryDocument) != "" {
				if !diskSet[strings.ToLower(m.PrimaryDocument)] {
					t.Fatalf("primary document %q from metadata missing on disk for unit %s", m.PrimaryDocument, unit.RelDir)
				}
			}
		}

		if unit.IndexPath != "" {
			idx, err := secfixtures.LoadIndex(unit.IndexPath)
			if err != nil {
				t.Fatalf("index load failed for %s: %v", unit.IndexPath, err)
			}

			missingCount := 0
			matchCount := 0
			for _, item := range idx.Directory.Items {
				if strings.TrimSpace(item.Name) == "" {
					continue
				}
				if !diskSet[strings.ToLower(item.Name)] {
					missingCount++
					continue
				}
				matchCount++
			}

			if matchCount == 0 {
				t.Fatalf("index/disk contradiction for unit %s: index has %d entries but none are present on disk", unit.RelDir, len(idx.Directory.Items))
			}
			t.Logf("unit %s index vs disk coverage: matches=%d missing=%d total=%d", unit.RelDir, matchCount, missingCount, len(idx.Directory.Items))
		}

		for _, interesting := range []string{"filingsummary.xml", "metalink.json", "metallinks.json"} {
			if diskSet[interesting] {
				foundCompanion := false
				for _, c := range unit.CompanionPaths {
					if strings.EqualFold(filepath.Base(c), interesting) {
						foundCompanion = true
						break
					}
				}
				if !foundCompanion {
					t.Fatalf("interesting companion file %q exists on disk but not detected in companion inventory for unit %s", interesting, unit.RelDir)
				}
			}
		}
	}
}

func TestPrimaryDocumentClassification_ForEachFixture(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	classified := 0
	for _, unit := range units {
		var docPath string
		if unit.MetadataPath != "" {
			m, err := secfixtures.LoadMetadata(unit.MetadataPath)
			if err != nil {
				t.Fatalf("metadata load failed for %s: %v", unit.MetadataPath, err)
			}
			if strings.TrimSpace(m.PrimaryDocument) != "" {
				docPath = filepath.Join(unit.RootDir, m.PrimaryDocument)
			}
		}
		if docPath == "" && len(unit.PrimaryCandidatePaths) > 0 {
			docPath = unit.PrimaryCandidatePaths[0]
		}
		if docPath == "" {
			continue
		}
		classified++

		class, size, err := secfixtures.ReadAndClassifyDocument(docPath)
		if err != nil {
			t.Fatalf("classification failed for %s (unit=%s): %v", docPath, unit.RelDir, err)
		}
		if size <= 0 {
			t.Fatalf("non-positive document size for %s (unit=%s)", docPath, unit.RelDir)
		}
		if strings.TrimSpace(class.Kind) == "" {
			t.Fatalf("empty classification kind for %s (unit=%s)", docPath, unit.RelDir)
		}
	}

	if classified == 0 {
		t.Fatalf("expected at least one classified primary document")
	}
}

func TestCorpusSummary_ReportsCoverageStats(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	summary, err := secfixtures.BuildCorpusSummary(units)
	if err != nil {
		t.Fatalf("build summary: %v", err)
	}
	if summary.TotalUnits == 0 {
		t.Fatalf("summary should report non-zero total units")
	}
	if summary.WithMetadata == 0 || summary.WithIndex == 0 || summary.WithBoth == 0 {
		t.Fatalf("summary should report metadata/index coverage: %+v", summary)
	}
	if len(summary.Forms) == 0 || len(summary.ByIssuer) == 0 || len(summary.ByDocClass) == 0 {
		t.Fatalf("summary should report forms/issuer/class coverage: %+v", summary)
	}

	t.Logf("SEC corpus summary: total=%d with_metadata=%d with_index=%d with_both=%d with_primary_candidates=%d",
		summary.TotalUnits, summary.WithMetadata, summary.WithIndex, summary.WithBoth, summary.WithPrimaryCandidates)
	for _, k := range secfixtures.SortedMapKeys(summary.Forms) {
		t.Logf("form[%s]=%d", k, summary.Forms[k])
	}
	for _, k := range secfixtures.SortedMapKeys(summary.ByIssuer) {
		t.Logf("issuer[%s]=%d", k, summary.ByIssuer[k])
	}
	for _, k := range secfixtures.SortedMapKeys(summary.ByDocClass) {
		t.Logf("docclass[%s]=%d", k, summary.ByDocClass[k])
	}
}

func TestWeirdnessRegistry_ExistsForFutureExceptions(t *testing.T) {
	r := secfixtures.DefaultWeirdnessRegistry()
	if r.ByRelDir == nil || r.ByAccession == nil {
		t.Fatalf("default weirdness registry maps should be initialized")
	}
}
