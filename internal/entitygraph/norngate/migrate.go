package norngate

import (
	"context"
	"fmt"

	"norn/pkg/norn"
)

// Migrate performs the S141-03 (recon matcher leg) migration: takes a
// frozen snapshot of entity-graph's resolved accuracy history, promotes the
// current rules as the first "entity_graph_rules" artifact NORN has ever
// governed (or gates a later candidate against the incumbent), and proves
// replay determinism by grading twice independently before recording
// anything — the same discipline as S141-02's EPS migration, delegating
// the actual bootstrap-or-gate branch to norn.GradeAndPromote.
//
// What real historical replay this proves, unlike S141-02: the frozen
// snapshot is real production ground truth (accuracy.ndjson's resolved
// records — genuine confirmed/refuted outcomes from live entity-graph
// signal generation), not fixtures. Re-grading the same snapshot twice and
// getting identical precision numbers both times is a real replay of real
// historical events producing an identical decision — PRIME-101 §8.2's
// acceptance text, satisfied for real for the first time in this kernel's
// life. What it does NOT yet prove: a genuine candidate-vs-incumbent
// comparison for a different ruleset, which needs a backtester that
// doesn't exist (see AccuracyOracle's doc comment). That gap is disclosed
// in EMILY/BACKLOG.md, not silently closed.
func Migrate(ctx context.Context, reg *norn.NDJSONRegistry, rulesPath, accuracyDir, snapshotDir string) (norn.Decision, error) {
	snapPath, _, err := SnapshotResolvedRecords(accuracyDir, snapshotDir)
	if err != nil {
		return norn.Decision{}, err
	}
	oracle := AccuracyOracle{SnapshotPath: snapPath}

	artifact, err := RulesArtifact(rulesPath)
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
			"norngate: replay determinism violated — grading the same snapshot twice produced different reports: %+v vs %+v",
			report1, report2)
	}

	policy := norn.GatePolicy{
		Kind:           artifact.Kind,
		NoRegressionOn: []string{"precision_overall"},
		ApprovalTier:   norn.TierAutonomous,
	}
	return norn.GradeAndPromote(ctx, reg, oracle, norn.DefaultGate{}, artifact, policy)
}

// reportsEqual compares two Reports for the replay-determinism check. No
// tolerance is applied — Grade's computation is a pure deterministic
// function of the frozen snapshot file; any observed difference is a real
// bug, not float noise.
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
