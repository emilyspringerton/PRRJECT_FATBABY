package secfixtures_test

import (
	"path/filepath"
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
		if first[i].RelDir != second[i].RelDir {
			t.Fatalf("determinism failure at %d: %s != %s", i, first[i].RelDir, second[i].RelDir)
		}
		if strings.Join(first[i].AllFiles, "|") != strings.Join(second[i].AllFiles, "|") {
			t.Fatalf("determinism failure for files in %s", first[i].RelDir)
		}
	}
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

		metadata, raw, err := secfixtures.LoadMetadata(unit.MetadataPath)
		if err != nil {
			t.Fatalf("metadata load failed for %s: %v", unit.MetadataPath, err)
		}
		if err := secfixtures.ValidateMetadataCore(metadata, raw); err != nil {
			t.Fatalf("metadata core validation failed for %s: %v", unit.MetadataPath, err)
		}
	}

	if metadataCount == 0 {
		t.Fatalf("expected at least one metadata.json in corpus")
	}
}

func TestCorpusIndexJSON_IsLoadableAndReferencesPrimaryDocWhenPresent(t *testing.T) {
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

		indexData, err := secfixtures.LoadIndex(unit.IndexPath)
		if err != nil {
			t.Fatalf("index load failed for %s: %v", unit.IndexPath, err)
		}
		if len(indexData.Items) == 0 {
			t.Fatalf("index contains no directory.item entries: %s", unit.IndexPath)
		}

		if unit.MetadataPath == "" {
			continue
		}

		metadata, _, err := secfixtures.LoadMetadata(unit.MetadataPath)
		if err != nil {
			t.Fatalf("metadata load failed for %s: %v", unit.MetadataPath, err)
		}
		if strings.TrimSpace(metadata.PrimaryDocument) == "" {
			continue
		}

		primaryLower := strings.ToLower(metadata.PrimaryDocument)
		inIndex := false
		for _, item := range indexData.Items {
			if strings.ToLower(item.Name) == primaryLower {
				inIndex = true
				break
			}
		}
		if inIndex {
			continue
		}

		inDir := false
		for _, file := range unit.AllFiles {
			if strings.ToLower(filepath.Base(file)) == primaryLower {
				inDir = true
				break
			}
		}

		if !inDir {
			t.Fatalf("primary_document %q not found in index entries or fixture dir for %s", metadata.PrimaryDocument, unit.RelDir)
		}
	}

	if indexCount == 0 {
		t.Fatalf("expected at least one index.json in corpus")
	}
}

func TestCorpusRawDocuments_AreReadableNonEmptyAndClassifiable(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	rawCount := 0
	for _, unit := range units {
		for _, docPath := range unit.RawCandidatePaths {
			rawCount++
			classification, size, err := secfixtures.ReadAndClassifyDocument(docPath)
			if err != nil {
				t.Fatalf("raw document smoke failure for %s: %v", docPath, err)
			}
			if size <= 0 {
				t.Fatalf("raw document %s reported non-positive size %d", docPath, size)
			}
			if strings.TrimSpace(classification.Kind) == "" {
				t.Fatalf("raw document %s returned empty classification", docPath)
			}
		}
	}

	if rawCount == 0 {
		t.Fatalf("expected at least one candidate raw document in corpus")
	}
}

func TestCorpusSummary_ReportsBroadCoverageStats(t *testing.T) {
	units, err := secfixtures.DiscoverFixtureUnits(fixturesRoot(t))
	if err != nil {
		t.Fatalf("discover units: %v", err)
	}

	type stats struct {
		units            int
		withMetadata     int
		withIndex        int
		withRawDocs      int
		rawDocs          int
		inlineXBRL       int
		htmlDocs         int
		xmlDocs          int
		txtDocs          int
		unknownClassDocs int
	}

	s := stats{units: len(units)}
	for _, unit := range units {
		if unit.MetadataPath != "" {
			s.withMetadata++
		}
		if unit.IndexPath != "" {
			s.withIndex++
		}
		if len(unit.RawCandidatePaths) > 0 {
			s.withRawDocs++
		}

		for _, docPath := range unit.RawCandidatePaths {
			classification, _, err := secfixtures.ReadAndClassifyDocument(docPath)
			if err != nil {
				t.Fatalf("summary classifier failed for %s: %v", docPath, err)
			}
			s.rawDocs++
			if classification.InlineXBRL {
				s.inlineXBRL++
			}
			switch classification.Kind {
			case "html":
				s.htmlDocs++
			case "xml":
				s.xmlDocs++
			case "txt":
				s.txtDocs++
			default:
				s.unknownClassDocs++
			}
		}
	}

	if s.units == 0 || s.rawDocs == 0 {
		t.Fatalf("expected non-empty corpus stats: %+v", s)
	}

	t.Logf("SEC corpus summary: units=%d with_metadata=%d with_index=%d with_raw_docs=%d raw_docs=%d inline_xbrl=%d html=%d xml=%d txt=%d unknown=%d",
		s.units, s.withMetadata, s.withIndex, s.withRawDocs, s.rawDocs, s.inlineXBRL, s.htmlDocs, s.xmlDocs, s.txtDocs, s.unknownClassDocs)
}
