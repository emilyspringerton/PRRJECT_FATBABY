// Package norngate wires the EPS headline extractor (internal/eps) to the
// NORN kernel (norn/pkg/norn) — HQ-SPEC-PRIME-101 §8.2, EMILY/BACKLOG.md
// S141-02: "the loop that invented the pattern proves the kernel."
//
// Kept separate from internal/eps itself so the core extraction package
// stays NORN-agnostic; this package is the adapter, not the extractor.
package norngate

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"norn/pkg/norn"

	"github.com/example/prrject-fatbaby/internal/eps"
)

// ArtifactKind is the norn.Artifact.Kind for every EPS extractor version
// this loop has ever produced.
const ArtifactKind = "eps_extractor"

// EvalCase is one frozen, ground-truth-labeled case in the eval set.
type EvalCase struct {
	Name        string
	FixtureFile string
	Ticker      string
	ExpectedEPS float64
	Tolerance   float64
}

// DefaultEvalSet is sourced from fixtures/eps/*.txt using the known-correct
// values already codified in internal/eps/extract_test.go (TestExtract_AAPL_
// Q1_2026 et al.) — the only ground-truth-labeled EPS data that exists
// anywhere in this system as of 2026-07-16.
//
// Real production oracle cases exist (var/eps/oracle.ndjson) but every one
// of them is "pending": the reconciler (cmd/eps-reconciler) has never once
// matched a pending case against a filed 8-K in its entire operating
// history — verified against the full var/secwatch event store (zero
// events for either pending case's ticker/period) and
// var/logs/eps-reconciler.log (every reconciliation cycle since 2026-05-30
// logged "no matches found against pending cases"). This is not a bug —
// the watchlist just hasn't produced an overlapping filing yet — but it
// means there is no real historical grading or promotion decision to
// replay. EPSOracle is fixture-based for that reason, documented rather
// than silently assumed to be production data. The moment the reconciler
// produces its first confirmed/contradicted case, this eval set should be
// extended (or replaced) with real cases — no interface change required,
// see Grade below.
var DefaultEvalSet = []EvalCase{
	{Name: "aapl_q1_2026", FixtureFile: "aapl_q1_2026.txt", Ticker: "AAPL", ExpectedEPS: 2.40, Tolerance: 0.005},
	{Name: "msft_q2_2026", FixtureFile: "msft_q2_2026.txt", Ticker: "MSFT", ExpectedEPS: 3.23, Tolerance: 0.005},
	{Name: "nvda_q3_2026", FixtureFile: "nvda_q3_2026.txt", Ticker: "NVDA", ExpectedEPS: 0.78, Tolerance: 0.005},
	{Name: "loss_example", FixtureFile: "loss_example.txt", Ticker: "ACME", ExpectedEPS: -0.42, Tolerance: 0.005},
}

// EPSOracle wraps eps.Extract as a norn.Oracle. Grade always runs the
// current eps.Extract function — the same one built into this binary —
// against DefaultEvalSet; there is only one real extractor implementation
// in the repo (verified via git log on extract.go: two commits, no
// alternate heuristic branches), so "which extractor version" is captured
// by the calling artifact's content hash (see CurrentExtractorArtifact),
// not by any runtime dispatch inside Grade itself.
type EPSOracle struct {
	// FixturesDir is the directory containing the eval set's fixture files
	// (fixtures/eps/*.txt relative to the repo root).
	FixturesDir string
}

// Version returns a content hash over the eval set's definition (case
// names, expected values, tolerances) — not the fixture file contents,
// which are large and not meant to live inside a short version identifier.
// Changing DefaultEvalSet is what "rotating the oracle" means for this
// instantiation (PRIME-101 §5: rotation must be versioned, never in-place).
func (o EPSOracle) Version() norn.OracleVersion {
	b, err := json.Marshal(DefaultEvalSet)
	if err != nil {
		// DefaultEvalSet is a static, always-marshalable literal; a failure
		// here would mean the eval set itself is malformed at compile time.
		panic(fmt.Sprintf("norngate: eval set does not marshal: %v", err))
	}
	return norn.OracleVersion(norn.Hash(b))
}

// Grade runs eps.Extract against every fixture in DefaultEvalSet and scores
// accuracy: the fraction of cases whose extracted GAAP diluted EPS matches
// the known-correct value within tolerance. Metrics follow the norn.Report
// higher-is-better convention throughout (1.0 = correct, 0.0 = wrong, for
// both the per-case and the aggregate "accuracy" metric).
func (o EPSOracle) Grade(ctx context.Context, a norn.Artifact) (norn.Report, error) {
	metrics := make(map[string]float64, len(DefaultEvalSet)+1)
	correct := 0

	for _, c := range DefaultEvalSet {
		select {
		case <-ctx.Done():
			return norn.Report{}, ctx.Err()
		default:
		}

		text, err := os.ReadFile(filepath.Join(o.FixturesDir, c.FixtureFile))
		if err != nil {
			return norn.Report{}, fmt.Errorf("norngate: read fixture %s: %w", c.FixtureFile, err)
		}

		got := eps.Extract(string(text), "norn-eval:"+c.Name, c.Ticker)
		pass := got.EPSGAAPDiluted != nil && math.Abs(*got.EPSGAAPDiluted-c.ExpectedEPS) <= c.Tolerance
		if pass {
			correct++
			metrics["case_"+c.Name] = 1.0
		} else {
			metrics["case_"+c.Name] = 0.0
		}
	}

	metrics["accuracy"] = float64(correct) / float64(len(DefaultEvalSet))

	return norn.Report{
		OracleVersion: o.Version(),
		Metrics:       metrics,
		Notes:         fmt.Sprintf("%d/%d fixture cases correct (fixture-based eval — see DefaultEvalSet doc comment)", correct, len(DefaultEvalSet)),
	}, nil
}

var _ norn.Oracle = EPSOracle{}

// CurrentExtractorArtifact content-hashes internal/eps/extract.go's source
// file so a real code change to the extractor — not a re-run of this
// migration against unchanged code — is what produces a new candidate
// artifact hash. Running this against the same source twice must produce
// the same hash and, downstream, the same grade: that repeatability is the
// replay-determinism proof this package exercises.
func CurrentExtractorArtifact(repoRoot string) (norn.Artifact, error) {
	path := filepath.Join(repoRoot, "internal", "eps", "extract.go")
	src, err := os.ReadFile(path)
	if err != nil {
		return norn.Artifact{}, fmt.Errorf("norngate: read %s: %w", path, err)
	}
	return norn.Artifact{
		Hash: norn.Hash(src),
		Kind: ArtifactKind,
		Meta: map[string]string{"source_file": "internal/eps/extract.go"},
	}, nil
}

// reportsEqual compares two Reports for the replay-determinism check.
// Metrics maps compare by value at every key present in either map — no
// tolerance is applied because Grade's computation is a pure deterministic
// function of the fixture files and the extractor code; any observed
// difference is a real bug (nondeterminism), not float noise.
func reportsEqual(a, b norn.Report) bool {
	if a.OracleVersion != b.OracleVersion || a.Notes != b.Notes {
		return false
	}
	if len(a.Metrics) != len(b.Metrics) {
		return false
	}
	for k, v := range a.Metrics {
		if bv, ok := b.Metrics[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
