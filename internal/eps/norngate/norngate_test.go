package norngate

import (
	"context"
	"path/filepath"
	"testing"

	"norn/pkg/norn"
)

// repoRoot and fixturesDir assume tests run from this package's directory
// (Go's default `go test` working directory).
const repoRootRel = "../../.."

func fixturesDirRel() string {
	return filepath.Join(repoRootRel, "fixtures", "eps")
}

func TestEPSOracle_Grade_AllFixturesCorrect(t *testing.T) {
	oracle := EPSOracle{FixturesDir: fixturesDirRel()}
	artifact, err := CurrentExtractorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentExtractorArtifact: %v", err)
	}

	report, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}

	// extract_test.go already proves each fixture individually extracts the
	// correct EPS; this asserts the oracle wrapper agrees on all four.
	if report.Metrics["accuracy"] != 1.0 {
		t.Errorf("accuracy = %v, want 1.0 (all 4 fixtures should pass): %+v", report.Metrics["accuracy"], report.Metrics)
	}
	if report.OracleVersion != oracle.Version() {
		t.Errorf("Report.OracleVersion = %q, want %q", report.OracleVersion, oracle.Version())
	}
}

func TestEPSOracle_Grade_Deterministic(t *testing.T) {
	oracle := EPSOracle{FixturesDir: fixturesDirRel()}
	artifact, err := CurrentExtractorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentExtractorArtifact: %v", err)
	}

	r1, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade run 1: %v", err)
	}
	r2, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade run 2: %v", err)
	}
	if !reportsEqual(r1, r2) {
		t.Fatalf("grading the same artifact twice produced different reports: %+v vs %+v", r1, r2)
	}
}

func TestEPSOracle_Version_StableAcrossCalls(t *testing.T) {
	oracle := EPSOracle{FixturesDir: fixturesDirRel()}
	if oracle.Version() != oracle.Version() {
		t.Fatal("Version() is not stable across calls")
	}
}

func TestCurrentExtractorArtifact_HashStable(t *testing.T) {
	a1, err := CurrentExtractorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentExtractorArtifact: %v", err)
	}
	a2, err := CurrentExtractorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentExtractorArtifact: %v", err)
	}
	if a1.Hash != a2.Hash {
		t.Fatalf("hash not stable: %s vs %s", a1.Hash, a2.Hash)
	}
	if a1.Kind != ArtifactKind {
		t.Errorf("Kind = %q, want %q", a1.Kind, ArtifactKind)
	}
}

func TestMigrate_BootstrapsFirstPromotion(t *testing.T) {
	regPath := filepath.Join(t.TempDir(), "eps_extractor.ndjson")
	reg, err := norn.NewNDJSONRegistry(regPath)
	if err != nil {
		t.Fatalf("NewNDJSONRegistry: %v", err)
	}

	decision, err := Migrate(context.Background(), reg, repoRootRel, fixturesDirRel())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !decision.Promote {
		t.Fatalf("expected bootstrap promotion, got Promote=false (reason: %s)", decision.Reason)
	}

	golden, err := reg.Golden(ArtifactKind)
	if err != nil {
		t.Fatalf("Golden after bootstrap: %v", err)
	}
	wantArtifact, _ := CurrentExtractorArtifact(repoRootRel)
	if golden.Hash != wantArtifact.Hash {
		t.Errorf("golden hash = %s, want %s", golden.Hash, wantArtifact.Hash)
	}
}

func TestMigrate_SecondRunSameCodeIsNoOp(t *testing.T) {
	regPath := filepath.Join(t.TempDir(), "eps_extractor.ndjson")
	reg, err := norn.NewNDJSONRegistry(regPath)
	if err != nil {
		t.Fatalf("NewNDJSONRegistry: %v", err)
	}

	first, err := Migrate(context.Background(), reg, repoRootRel, fixturesDirRel())
	if err != nil {
		t.Fatalf("Migrate (first): %v", err)
	}
	if !first.Promote {
		t.Fatalf("expected first run to bootstrap-promote, got: %s", first.Reason)
	}

	second, err := Migrate(context.Background(), reg, repoRootRel, fixturesDirRel())
	if err != nil {
		t.Fatalf("Migrate (second): %v", err)
	}
	if second.Promote {
		t.Fatalf("expected second run against unchanged code to be a no-op, got Promote=true")
	}
}

func TestMigrate_ReplayFromDisk_IdenticalDecision(t *testing.T) {
	// Runs Migrate against two independent registries (fresh temp files) and
	// requires the bootstrap decision to be identical both times — the
	// replay-determinism proof this migration exists to demonstrate.
	dec1, err := Migrate(context.Background(), mustReg(t), repoRootRel, fixturesDirRel())
	if err != nil {
		t.Fatalf("Migrate (registry 1): %v", err)
	}
	dec2, err := Migrate(context.Background(), mustReg(t), repoRootRel, fixturesDirRel())
	if err != nil {
		t.Fatalf("Migrate (registry 2): %v", err)
	}
	if dec1.Promote != dec2.Promote || dec1.Reason != dec2.Reason {
		t.Fatalf("replay determinism violated across independent registries: %+v vs %+v", dec1, dec2)
	}
}

func mustReg(t *testing.T) *norn.NDJSONRegistry {
	t.Helper()
	reg, err := norn.NewNDJSONRegistry(filepath.Join(t.TempDir(), "eps_extractor.ndjson"))
	if err != nil {
		t.Fatalf("NewNDJSONRegistry: %v", err)
	}
	return reg
}
