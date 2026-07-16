package norngate

import (
	"context"
	"fmt"

	"norn/pkg/norn"
)

// Migrate performs the S141-02 migration: promotes the current eps.Extract
// implementation as the first "eps_extractor" artifact NORN has ever
// governed, and proves replay determinism by grading it twice independently
// and requiring an identical Report both times before recording anything —
// then delegates the actual bootstrap-or-gate decision to norn.GradeAndPromote,
// the kernel's shared implementation of that branch (see pkg/norn/promote.go's
// doc comment: this migration's first draft is what surfaced the need for it).
//
// What this proves: the mechanism — extract → grade → gate → promote,
// wired through pkg/norn exactly as every future NORN instantiation will
// be — is deterministic and produces a real, auditable promotion event.
//
// What this does NOT prove (see DefaultEvalSet's doc comment for the full
// reality-check): PRIME-101 §8.2's acceptance text, "identical promotion
// decisions replayed through NORN from historical events," reads most
// naturally as replaying real production history. None exists — verified,
// not assumed. This migration proves determinism against the only
// ground-truth-labeled data that exists (four fixtures) instead. It
// upgrades to a real historical-event replay automatically, no code change
// required, the moment cmd/eps-reconciler produces its first real
// confirmed/contradicted verdict.
func Migrate(ctx context.Context, reg *norn.NDJSONRegistry, repoRoot, fixturesDir string) (norn.Decision, error) {
	oracle := EPSOracle{FixturesDir: fixturesDir}

	artifact, err := CurrentExtractorArtifact(repoRoot)
	if err != nil {
		return norn.Decision{}, err
	}

	report1, err := oracle.Grade(ctx, artifact)
	if err != nil {
		return norn.Decision{}, fmt.Errorf("norngate: grade (run 1): %w", err)
	}
	report2, err := oracle.Grade(ctx, artifact)
	if err != nil {
		return norn.Decision{}, fmt.Errorf("norngate: grade (run 2): %w", err)
	}
	if !reportsEqual(report1, report2) {
		return norn.Decision{}, fmt.Errorf(
			"norngate: replay determinism violated — grading the same artifact twice produced different reports: %+v vs %+v",
			report1, report2)
	}

	policy := norn.GatePolicy{
		Kind:           artifact.Kind,
		NoRegressionOn: []string{"accuracy"},
		ApprovalTier:   norn.TierAutonomous,
	}
	return norn.GradeAndPromote(ctx, reg, oracle, norn.DefaultGate{}, artifact, policy)
}
