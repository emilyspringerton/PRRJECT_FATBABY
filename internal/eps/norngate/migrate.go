package norngate

import (
	"context"
	"fmt"

	"norn/pkg/norn"
)

// Migrate performs the S141-02 migration: promotes the current eps.Extract
// implementation as the first "eps_extractor" artifact NORN has ever
// governed, and proves replay determinism by grading it twice independently
// and requiring an identical Report both times before recording anything.
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

	if err := reg.Record(norn.Event{
		Type:     norn.EventEvaluationCompleted,
		Kind:     artifact.Kind,
		Artifact: &artifact,
		Report:   &report1,
	}); err != nil {
		return norn.Decision{}, fmt.Errorf("norngate: record evaluation: %w", err)
	}

	incumbent, err := reg.Golden(artifact.Kind)
	if err != nil {
		// Bootstrap: no incumbent has ever existed for this kind (verified —
		// var/norn/eps_extractor.ndjson has no prior artifact_promoted
		// event). Gate.Decide requires an incumbent Report to compare
		// against (§2: "never worse than the incumbent"); with none, there
		// is nothing to regress against, so the first artifact of a kind is
		// promoted directly rather than gated. This is the one legitimate
		// bypass of Gate in the whole loop, and it happens exactly once per
		// kind — every subsequent artifact goes through DefaultGate for real.
		decision := norn.Decision{
			Promote: true,
			Reason:  "bootstrap: no prior eps_extractor artifact exists, nothing to regress against",
		}
		if err := recordDecision(reg, artifact, report1, decision); err != nil {
			return decision, err
		}
		return decision, nil
	}

	if incumbent.Hash == artifact.Hash {
		return norn.Decision{
			Promote: false,
			Reason:  "candidate is already golden (identical content hash) — nothing to promote",
		}, nil
	}

	incumbentHistory, err := reg.History(artifact.Kind)
	if err != nil {
		return norn.Decision{}, fmt.Errorf("norngate: load history for gate comparison: %w", err)
	}
	incumbentReport, ok := latestReportForHash(incumbentHistory, incumbent.Hash)
	if !ok {
		return norn.Decision{}, fmt.Errorf("norngate: incumbent artifact %s has no recorded evaluation to gate against", incumbent.Hash)
	}

	policy := norn.GatePolicy{
		Kind:           artifact.Kind,
		NoRegressionOn: []string{"accuracy"},
		ApprovalTier:   norn.TierAutonomous,
	}
	gate := norn.DefaultGate{}
	decision := gate.Decide(incumbentReport, report1, policy)
	if err := recordDecision(reg, artifact, report1, decision); err != nil {
		return decision, err
	}
	return decision, nil
}

func recordDecision(reg *norn.NDJSONRegistry, artifact norn.Artifact, report norn.Report, decision norn.Decision) error {
	if err := reg.Record(norn.Event{
		Type:     norn.EventGateEvaluated,
		Kind:     artifact.Kind,
		Artifact: &artifact,
		Report:   &report,
		Decision: &decision,
	}); err != nil {
		return fmt.Errorf("norngate: record gate decision: %w", err)
	}
	if decision.Promote {
		if err := reg.Record(norn.Event{Type: norn.EventArtifactPromoted, Kind: artifact.Kind, Artifact: &artifact}); err != nil {
			return fmt.Errorf("norngate: record promotion: %w", err)
		}
	} else {
		if err := reg.Record(norn.Event{Type: norn.EventArtifactRejected, Kind: artifact.Kind, Artifact: &artifact, Reason: decision.Reason}); err != nil {
			return fmt.Errorf("norngate: record rejection: %w", err)
		}
	}
	return nil
}

// latestReportForHash finds the most recent evaluation_completed Report
// recorded for the artifact with the given hash, searching newest-first.
func latestReportForHash(history []norn.Event, hash string) (norn.Report, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		e := history[i]
		if e.Type == norn.EventEvaluationCompleted && e.Artifact != nil && e.Artifact.Hash == hash && e.Report != nil {
			return *e.Report, true
		}
	}
	return norn.Report{}, false
}
