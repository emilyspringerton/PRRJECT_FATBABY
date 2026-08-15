package fableeval

import (
	"context"
	"testing"
)

const repoRootRel = "../.."

func TestEPSHeadlineOracle_Grade_AllFixturesCorrect(t *testing.T) {
	oracle := EPSHeadlineOracle{}
	artifact, err := CurrentGeneratorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentGeneratorArtifact: %v", err)
	}

	report, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}

	// The fixtures were built directly from buildHeadline's own documented
	// format spec; this asserts eps.Generate agrees on all four branches.
	if report.Metrics["accuracy"] != 1.0 {
		t.Errorf("accuracy = %v, want 1.0 (all %d fixtures should pass): %+v",
			report.Metrics["accuracy"], len(DefaultEvalSet), report.Metrics)
	}
	if report.OracleVersion != oracle.Version() {
		t.Errorf("Report.OracleVersion = %q, want %q", report.OracleVersion, oracle.Version())
	}
}

func TestEPSHeadlineOracle_Grade_Deterministic(t *testing.T) {
	oracle := EPSHeadlineOracle{}
	artifact, err := CurrentGeneratorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentGeneratorArtifact: %v", err)
	}

	r1, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade run 1: %v", err)
	}
	r2, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade run 2: %v", err)
	}
	if r1.OracleVersion != r2.OracleVersion {
		t.Errorf("OracleVersion differs across runs: %q vs %q", r1.OracleVersion, r2.OracleVersion)
	}
	if len(r1.Metrics) != len(r2.Metrics) {
		t.Fatalf("metric count differs: %d vs %d", len(r1.Metrics), len(r2.Metrics))
	}
	for k, v := range r1.Metrics {
		if r2.Metrics[k] != v {
			t.Errorf("metric %q differs across runs: %v vs %v", k, v, r2.Metrics[k])
		}
	}
}

func TestEPSHeadlineOracle_Version_ChangesWithEvalSet(t *testing.T) {
	oracle := EPSHeadlineOracle{}
	v1 := oracle.Version()

	saved := DefaultEvalSet
	defer func() { DefaultEvalSet = saved }()

	DefaultEvalSet = append([]EvalCase{}, saved...)
	DefaultEvalSet[0].ExpectedHeadline = "a different expected headline"
	v2 := oracle.Version()

	if v1 == v2 {
		t.Error("changing the eval set definition should change Version() -- rotation must be versioned")
	}
}

func TestEPSHeadlineOracle_Grade_DetectsWrongHeadline(t *testing.T) {
	oracle := EPSHeadlineOracle{}

	saved := DefaultEvalSet
	defer func() { DefaultEvalSet = saved }()
	DefaultEvalSet = append([]EvalCase{}, saved...)
	DefaultEvalSet[0].ExpectedHeadline = "this will never match anything eps.Generate produces"

	artifact, err := CurrentGeneratorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentGeneratorArtifact: %v", err)
	}
	report, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if report.Metrics["accuracy"] == 1.0 {
		t.Error("expected a mismatched case to bring accuracy below 1.0, but it did not -- Grade is not actually comparing headlines")
	}
	if report.Metrics["case_"+DefaultEvalSet[0].Name] != 0.0 {
		t.Errorf("expected the deliberately-wrong case to score 0.0, got %v", report.Metrics["case_"+DefaultEvalSet[0].Name])
	}
}

func TestCurrentGeneratorArtifact_StableAcrossCalls(t *testing.T) {
	a1, err := CurrentGeneratorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentGeneratorArtifact: %v", err)
	}
	a2, err := CurrentGeneratorArtifact(repoRootRel)
	if err != nil {
		t.Fatalf("CurrentGeneratorArtifact: %v", err)
	}
	if a1.Hash != a2.Hash {
		t.Errorf("hashing the same unchanged source file twice produced different hashes: %s vs %s", a1.Hash, a2.Hash)
	}
	if a1.Kind != ArtifactKind {
		t.Errorf("Kind = %q, want %q", a1.Kind, ArtifactKind)
	}
}
