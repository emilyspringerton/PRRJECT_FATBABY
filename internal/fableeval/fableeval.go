// Package fableeval is the FABLE eval harness (HQ-SPEC-AI-103 §4d, build
// order step 2): a frozen, versioned suite that grades a headline generator
// -- template-based today, a FABLE checkpoint once E0 (S145-03) exists --
// against known-correct EPS headline text. Suites are NORN oracles (PRIME-101
// §5): frozen, versioned, rotated only against held-out reality.
//
// "Frozen as oracle v1, before any training, so day-one numbers are honest"
// (S145-02's own text) is why this exists ahead of E0: once a real FABLE
// checkpoint is graded, its very first number must be measured against a
// suite that was fixed before anyone knew what that number would be, not
// tuned after the fact to make it look good.
//
// v0 scope: grade eps.Generate() -- the one real headline generator that
// exists today (deterministic, template-based) -- against a frozen fixture
// set, exactly mirroring internal/eps/norngate's own EPSOracle pattern for
// the extraction side (S141-02). This is deliberately the same shape a
// future FABLE-checkpoint Oracle candidate will be graded through: swapping
// eps.Generate() for a checkpoint's inference call is the only change
// S145-03 needs to make here, not a new harness.
package fableeval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"norn/pkg/norn"

	"github.com/example/prrject-fatbaby/internal/eps"
)

// ArtifactKind is the norn.Artifact.Kind for every EPS headline generator
// this loop has ever produced -- the template generator today, FABLE
// checkpoints (by weight-file hash) once E0 lands.
const ArtifactKind = "eps_headline_generator"

// EvalCase pairs a known EarningsData input with its known-correct expected
// headline text. Ground truth here is derived from buildHeadline's own
// documented format spec (internal/eps/article.go), not from live oracle
// data -- the live corpus currently has zero confirmed cases (see
// internal/fabledata, S145-01), the same honest gap EPSOracle's own
// DefaultEvalSet doc comment already discloses for the extraction side.
// Once real confirmed fabledata snapshots exist, this suite should be
// extended (or a second, snapshot-sourced suite added) the same way
// EPSOracle's own comment describes -- no interface change required.
type EvalCase struct {
	Name             string
	Input            eps.EarningsData
	ExpectedHeadline string
}

func f64(v float64) *float64 { return &v }

// DefaultEvalSet covers GAAP-positive, GAAP-loss, adjusted-only, and a
// full-year period -- one case per buildHeadline branch (internal/eps/
// article.go's own doc comment enumerates exactly these four shapes).
var DefaultEvalSet = []EvalCase{
	{
		Name: "gaap_positive_quarter",
		Input: eps.EarningsData{
			SourceIdentity:       "fableeval:gaap_positive_quarter",
			Issuer:               "Acme Corp",
			Ticker:               "ACME",
			Period:               eps.EarningsPeriod{FiscalQuarter: "Q1", FiscalYear: 2026},
			EPSGAAPDiluted:       f64(2.40),
			ExtractionConfidence: 0.95,
		},
		ExpectedHeadline: "Acme Corp reports Q1 2026 EPS of $2.40",
	},
	{
		Name: "gaap_loss_quarter",
		Input: eps.EarningsData{
			SourceIdentity:       "fableeval:gaap_loss_quarter",
			Issuer:               "Loss Inc",
			Ticker:               "LOSS",
			Period:               eps.EarningsPeriod{FiscalQuarter: "Q3", FiscalYear: 2026},
			EPSGAAPDiluted:       f64(-0.42),
			ExtractionConfidence: 0.95,
		},
		ExpectedHeadline: "Loss Inc reports Q3 2026 loss of $0.42 per share",
	},
	{
		Name: "adjusted_only_quarter",
		Input: eps.EarningsData{
			SourceIdentity:       "fableeval:adjusted_only_quarter",
			Issuer:               "NonGAAP LLC",
			Ticker:               "NGAP",
			Period:               eps.EarningsPeriod{FiscalQuarter: "Q2", FiscalYear: 2026},
			EPSAdjusted:          f64(1.15),
			ExtractionConfidence: 0.95,
		},
		ExpectedHeadline: "NonGAAP LLC reports Q2 2026 adjusted EPS of $1.15",
	},
	{
		Name: "gaap_positive_full_year",
		Input: eps.EarningsData{
			SourceIdentity:       "fableeval:gaap_positive_full_year",
			Issuer:               "Annual Co",
			Ticker:               "ANNL",
			Period:               eps.EarningsPeriod{FiscalQuarter: "FY", FiscalYear: 2025},
			EPSGAAPDiluted:       f64(9.87),
			ExtractionConfidence: 0.95,
		},
		ExpectedHeadline: "Annual Co reports full-year 2025 EPS of $9.87",
	},
}

// EPSHeadlineOracle wraps eps.Generate as a norn.Oracle. Grade always runs
// the current eps.Generate function -- the same one built into this binary
// -- against DefaultEvalSet; which generator version is captured by the
// calling artifact's content hash (see CurrentGeneratorArtifact), not by
// runtime dispatch inside Grade itself. Same convention EPSOracle
// established for the extraction side.
type EPSHeadlineOracle struct{}

// Version returns a content hash over the eval set's definition. Changing
// DefaultEvalSet is what "rotating the oracle" means for this instantiation
// (PRIME-101 §5: rotation must be versioned, never in-place).
func (o EPSHeadlineOracle) Version() norn.OracleVersion {
	b, err := json.Marshal(DefaultEvalSet)
	if err != nil {
		// DefaultEvalSet is a static, always-marshalable literal; a failure
		// here would mean the eval set itself is malformed at compile time.
		panic(fmt.Sprintf("fableeval: eval set does not marshal: %v", err))
	}
	return norn.OracleVersion(norn.Hash(b))
}

// Grade runs eps.Generate against every case in DefaultEvalSet and scores
// exact-match accuracy against the known-correct headline. Metrics follow
// the norn.Report higher-is-better convention (1.0 = exact match, 0.0 =
// anything else, for both per-case and aggregate "accuracy" metrics).
// Exact string match, deliberately, for v0: a template generator's output
// is either exactly right or a real bug -- graded text-similarity scoring
// is meaningful once a genuinely non-deterministic generator (FABLE) exists
// to grade, not before.
func (o EPSHeadlineOracle) Grade(ctx context.Context, a norn.Artifact) (norn.Report, error) {
	metrics := make(map[string]float64, len(DefaultEvalSet)+1)
	correct := 0

	for _, c := range DefaultEvalSet {
		select {
		case <-ctx.Done():
			return norn.Report{}, ctx.Err()
		default:
		}

		input := c.Input // copy: Generate takes a pointer and must not mutate the shared fixture
		article, ok := eps.Generate(&input)
		pass := ok && article.Headline == c.ExpectedHeadline
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
		Notes:         fmt.Sprintf("%d/%d fixture cases exact-match (fixture-based eval — see DefaultEvalSet doc comment)", correct, len(DefaultEvalSet)),
	}, nil
}

var _ norn.Oracle = EPSHeadlineOracle{}

// CurrentGeneratorArtifact content-hashes internal/eps/article.go's source
// file so a real code change to the generator -- not a re-run of this
// migration against unchanged code -- is what produces a new candidate
// artifact hash. Mirrors norngate.CurrentExtractorArtifact exactly.
func CurrentGeneratorArtifact(repoRoot string) (norn.Artifact, error) {
	path := filepath.Join(repoRoot, "internal", "eps", "article.go")
	src, err := os.ReadFile(path)
	if err != nil {
		return norn.Artifact{}, fmt.Errorf("fableeval: read %s: %w", path, err)
	}
	return norn.Artifact{
		Hash: norn.Hash(src),
		Kind: ArtifactKind,
		Meta: map[string]string{"source_file": "internal/eps/article.go"},
	}, nil
}
