// Package norngate wires entity-graph's signal accuracy/correlation system
// — the "recon matcher" leg of HQ-SPEC-PRIME-101 §8.3, EMILY/BACKLOG.md
// S141-03 — to the NORN kernel (norn/pkg/norn).
//
// Unlike S141-02's EPS extractor migration, real historical grading ground
// truth exists here in force: var/entity-graph/accuracy.ndjson carries
// ~345K AccuracyRecords, ~32K of them resolved (confirmed or refuted) as of
// 2026-07-16 — internal/entitygraph's rules.go doc comment already
// describes config/entity-graph-rules.json as "the mutation surface" for
// "the recursive self-improvement loop," and accuracy.go's Correlate*
// functions plus BuildAccuracyReports already compute "confirmed
// suggestions as labels" almost exactly as PRIME-101 §1 describes this
// loop. No promotion/versioning mechanism existed before this package
// (verified: zero hits for promot|version in accuracy.go or rules.go).
package norngate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"norn/pkg/norn"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// ArtifactKind is the norn.Artifact.Kind for every entity-graph rules
// version this loop has ever produced.
const ArtifactKind = "entity_graph_rules"

// RulesArtifact content-hashes the loaded Rules (post-fallback-to-defaults,
// matching entitygraph.LoadRules's own behavior) rather than the raw config
// file bytes, so the artifact is well-defined whether or not
// config/entity-graph-rules.json exists on disk.
func RulesArtifact(rulesPath string) (norn.Artifact, error) {
	rules := entitygraph.LoadRules(rulesPath)
	b, err := json.Marshal(rules)
	if err != nil {
		return norn.Artifact{}, fmt.Errorf("norngate: marshal rules: %w", err)
	}
	return norn.Artifact{
		Hash: norn.Hash(b),
		Kind: ArtifactKind,
		Meta: map[string]string{"rules_path": rulesPath},
	}, nil
}

// SnapshotResolvedRecords reads every AccuracyRecord from accuracyDir,
// keeps only the resolved ones (GTConfirmed/GTRefuted — GTPending is
// excluded, not graded as either), sorts them deterministically, and
// writes them to a new file under snapshotDir named by their content hash.
//
// This snapshot step exists because var/entity-graph/accuracy.ndjson is
// live and growing (313K pending records still resolving as of
// 2026-07-16): PRIME-101 §5 requires frozen evals to hold still ("frozen
// means frozen... an eval set that drifts under the loop it grades
// destroys the contraction"), and grading directly against the live file
// would mean every re-grade sees a different, larger eval set. Rotation
// (taking a new snapshot later, as more records resolve) is itself
// supposed to be an explicit, versioned NORN event per §5 rule 3 — that
// wiring belongs to nornd (S141-04), which doesn't exist yet; this
// function only takes the snapshot, it doesn't yet register oracle
// rotation as its own event.
//
// Returns the snapshot's path and its content hash.
func SnapshotResolvedRecords(accuracyDir, snapshotDir string) (path string, hash string, err error) {
	records, err := entitygraph.LoadAccuracyRecords(accuracyDir)
	if err != nil {
		return "", "", fmt.Errorf("norngate: load accuracy records: %w", err)
	}

	resolved := make([]entitygraph.AccuracyRecord, 0, len(records))
	for _, r := range records {
		if r.Outcome == entitygraph.GTConfirmed || r.Outcome == entitygraph.GTRefuted {
			resolved = append(resolved, r)
		}
	}
	// Deterministic order so the snapshot's content hash — and everything
	// graded from it — doesn't depend on the live file's append order,
	// which could in principle shift if accuracy.ndjson is ever
	// rewritten/compacted by unrelated maintenance.
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].SignalType != resolved[j].SignalType {
			return resolved[i].SignalType < resolved[j].SignalType
		}
		return resolved[i].SignalID < resolved[j].SignalID
	})

	var buf []byte
	for i := range resolved {
		b, err := json.Marshal(resolved[i])
		if err != nil {
			return "", "", fmt.Errorf("norngate: marshal snapshot record: %w", err)
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}

	h := norn.Hash(buf)
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return "", "", fmt.Errorf("norngate: create snapshot dir: %w", err)
	}
	snapPath := filepath.Join(snapshotDir, fmt.Sprintf("entity-graph-eval-%s.ndjson", h[:16]))
	if err := os.WriteFile(snapPath, buf, 0o644); err != nil {
		return "", "", fmt.Errorf("norngate: write snapshot: %w", err)
	}
	return snapPath, h, nil
}

func loadSnapshotRecords(path string) ([]entitygraph.AccuracyRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("norngate: open snapshot %s: %w", path, err)
	}
	defer f.Close()
	var records []entitygraph.AccuracyRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r entitygraph.AccuracyRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("norngate: corrupt snapshot record: %w", err)
		}
		records = append(records, r)
	}
	return records, sc.Err()
}

// AccuracyOracle wraps entitygraph.BuildAccuracyReports as a norn.Oracle.
//
// What Grade measures — and, honestly, what it does not: it recomputes
// per-signal-type precision from the frozen snapshot at SnapshotPath, which
// records how entity-graph's signal generation has actually performed in
// production under whatever rules were in effect when each signal was
// produced. It does NOT re-run signal generation under the candidate
// artifact's specific rules against historical raw filings — that would be
// a genuine backtester (replay raw filings → regenerate signals under
// candidate rules → correlate against reality), which does not exist in
// this codebase today. Concretely: Grade currently returns the same Report
// regardless of which rules artifact is passed to it, because there is
// only one real "what actually happened" track record to measure, not yet
// a way to simulate an alternate one. This is disclosed in
// EMILY/BACKLOG.md's S141-03 entry rather than silently assumed — building
// the backtester is the natural next step to make Grade artifact-sensitive
// for real.
type AccuracyOracle struct {
	// SnapshotPath is the frozen eval snapshot produced by
	// SnapshotResolvedRecords.
	SnapshotPath string
}

func (o AccuracyOracle) Version() norn.OracleVersion {
	b, err := os.ReadFile(o.SnapshotPath)
	if err != nil {
		// Version is called before Grade in every real call path (Grade
		// reads the same file and would fail identically); a missing
		// snapshot is a caller error, not a recoverable oracle state.
		panic(fmt.Sprintf("norngate: read snapshot for Version(): %v", err))
	}
	return norn.OracleVersion(norn.Hash(b))
}

func (o AccuracyOracle) Grade(ctx context.Context, a norn.Artifact) (norn.Report, error) {
	select {
	case <-ctx.Done():
		return norn.Report{}, ctx.Err()
	default:
	}

	records, err := loadSnapshotRecords(o.SnapshotPath)
	if err != nil {
		return norn.Report{}, err
	}

	reports := entitygraph.BuildAccuracyReports(records)
	metrics := make(map[string]float64, len(reports)+2)

	var totalConfirmed, totalResolved int
	for _, r := range reports {
		metrics["precision_"+string(r.SignalType)] = r.Precision
		totalConfirmed += r.Confirmed
		totalResolved += r.Confirmed + r.Refuted
	}

	overallPrecision := 0.0
	if totalResolved > 0 {
		overallPrecision = float64(totalConfirmed) / float64(totalResolved)
	}
	metrics["precision_overall"] = overallPrecision
	metrics["resolved_count"] = float64(totalResolved)

	return norn.Report{
		OracleVersion: o.Version(),
		Metrics:       metrics,
		Notes: fmt.Sprintf("%d/%d resolved predictions confirmed across %d signal types (frozen snapshot — see AccuracyOracle doc comment on what this does and doesn't measure)",
			totalConfirmed, totalResolved, len(reports)),
	}, nil
}

var _ norn.Oracle = AccuracyOracle{}
