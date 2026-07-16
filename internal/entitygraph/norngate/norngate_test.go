package norngate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"norn/pkg/norn"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// writeAccuracyFixture writes a small, controlled accuracy.ndjson into dir
// so tests don't depend on the size or exact content of the live
// var/entity-graph/accuracy.ndjson (345K records) — deterministic, fast,
// and independent of production data drifting over time.
func writeAccuracyFixture(t *testing.T, dir string) {
	t.Helper()
	records := []entitygraph.AccuracyRecord{
		{SignalID: "s1", Ticker: "AAA", SignalType: "activist_risk", Outcome: entitygraph.GTConfirmed, RecordedAt: "2026-01-01"},
		{SignalID: "s2", Ticker: "BBB", SignalType: "activist_risk", Outcome: entitygraph.GTConfirmed, RecordedAt: "2026-01-01"},
		{SignalID: "s3", Ticker: "CCC", SignalType: "activist_risk", Outcome: entitygraph.GTRefuted, RecordedAt: "2026-01-01"},
		{SignalID: "s4", Ticker: "DDD", SignalType: "governance_entrenchment", Outcome: entitygraph.GTConfirmed, RecordedAt: "2026-01-01"},
		{SignalID: "s5", Ticker: "EEE", SignalType: "governance_entrenchment", Outcome: entitygraph.GTPending, RecordedAt: "2026-01-01"},
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "accuracy.ndjson"))
	if err != nil {
		t.Fatalf("create accuracy.ndjson: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode record: %v", err)
		}
	}
}

func writeRulesFixture(t *testing.T, path string) {
	t.Helper()
	rules := entitygraph.DefaultRules()
	b, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal rules: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}
}

func TestSnapshotResolvedRecords_ExcludesPending(t *testing.T) {
	accuracyDir := t.TempDir()
	snapshotDir := t.TempDir()
	writeAccuracyFixture(t, accuracyDir)

	path, hash, err := SnapshotResolvedRecords(accuracyDir, snapshotDir)
	if err != nil {
		t.Fatalf("SnapshotResolvedRecords: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	records, err := loadSnapshotRecords(path)
	if err != nil {
		t.Fatalf("loadSnapshotRecords: %v", err)
	}
	// 5 fixture records, 1 pending — 4 resolved should survive.
	if len(records) != 4 {
		t.Fatalf("expected 4 resolved records (pending excluded), got %d", len(records))
	}
	for _, r := range records {
		if r.Outcome == entitygraph.GTPending {
			t.Errorf("pending record %s leaked into snapshot", r.SignalID)
		}
	}
}

func TestSnapshotResolvedRecords_DeterministicHash(t *testing.T) {
	accuracyDir := t.TempDir()
	writeAccuracyFixture(t, accuracyDir)

	_, hash1, err := SnapshotResolvedRecords(accuracyDir, t.TempDir())
	if err != nil {
		t.Fatalf("snapshot 1: %v", err)
	}
	_, hash2, err := SnapshotResolvedRecords(accuracyDir, t.TempDir())
	if err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("snapshot hash not deterministic: %s vs %s", hash1, hash2)
	}
}

func TestAccuracyOracle_Grade_ComputesPrecision(t *testing.T) {
	accuracyDir := t.TempDir()
	writeAccuracyFixture(t, accuracyDir)
	snapPath, _, err := SnapshotResolvedRecords(accuracyDir, t.TempDir())
	if err != nil {
		t.Fatalf("SnapshotResolvedRecords: %v", err)
	}

	oracle := AccuracyOracle{SnapshotPath: snapPath}
	rulesPath := filepath.Join(t.TempDir(), "rules.json")
	writeRulesFixture(t, rulesPath)
	artifact, err := RulesArtifact(rulesPath)
	if err != nil {
		t.Fatalf("RulesArtifact: %v", err)
	}

	report, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}

	// activist_risk: 2 confirmed, 1 refuted -> precision 2/3.
	got := report.Metrics["precision_activist_risk"]
	want := 2.0 / 3.0
	if got != want {
		t.Errorf("precision_activist_risk = %v, want %v", got, want)
	}
	// governance_entrenchment: 1 confirmed, 0 refuted (pending excluded) -> precision 1.0.
	if got := report.Metrics["precision_governance_entrenchment"]; got != 1.0 {
		t.Errorf("precision_governance_entrenchment = %v, want 1.0", got)
	}
	// overall: 3 confirmed / 4 resolved.
	if got := report.Metrics["precision_overall"]; got != 0.75 {
		t.Errorf("precision_overall = %v, want 0.75", got)
	}
	if got := report.Metrics["resolved_count"]; got != 4 {
		t.Errorf("resolved_count = %v, want 4", got)
	}
}

func TestAccuracyOracle_Grade_Deterministic(t *testing.T) {
	accuracyDir := t.TempDir()
	writeAccuracyFixture(t, accuracyDir)
	snapPath, _, err := SnapshotResolvedRecords(accuracyDir, t.TempDir())
	if err != nil {
		t.Fatalf("SnapshotResolvedRecords: %v", err)
	}
	oracle := AccuracyOracle{SnapshotPath: snapPath}
	rulesPath := filepath.Join(t.TempDir(), "rules.json")
	writeRulesFixture(t, rulesPath)
	artifact, _ := RulesArtifact(rulesPath)

	r1, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade run 1: %v", err)
	}
	r2, err := oracle.Grade(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Grade run 2: %v", err)
	}
	if !reportsEqual(r1, r2) {
		t.Fatalf("grading the same snapshot twice produced different reports: %+v vs %+v", r1, r2)
	}
}

func TestMigrate_BootstrapsFirstPromotion(t *testing.T) {
	accuracyDir := t.TempDir()
	writeAccuracyFixture(t, accuracyDir)
	rulesPath := filepath.Join(t.TempDir(), "rules.json")
	writeRulesFixture(t, rulesPath)

	regPath := filepath.Join(t.TempDir(), "entity_graph_rules.ndjson")
	reg, err := norn.NewNDJSONRegistry(regPath)
	if err != nil {
		t.Fatalf("NewNDJSONRegistry: %v", err)
	}

	decision, err := Migrate(context.Background(), reg, rulesPath, accuracyDir, t.TempDir())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !decision.Promote {
		t.Fatalf("expected bootstrap promotion, got Promote=false (%s)", decision.Reason)
	}

	golden, err := reg.Golden(ArtifactKind)
	if err != nil {
		t.Fatalf("Golden: %v", err)
	}
	wantArtifact, _ := RulesArtifact(rulesPath)
	if golden.Hash != wantArtifact.Hash {
		t.Errorf("golden hash = %s, want %s", golden.Hash, wantArtifact.Hash)
	}
}

func TestMigrate_SecondRunSameRulesIsNoOp(t *testing.T) {
	accuracyDir := t.TempDir()
	writeAccuracyFixture(t, accuracyDir)
	rulesPath := filepath.Join(t.TempDir(), "rules.json")
	writeRulesFixture(t, rulesPath)

	regPath := filepath.Join(t.TempDir(), "entity_graph_rules.ndjson")
	reg, err := norn.NewNDJSONRegistry(regPath)
	if err != nil {
		t.Fatalf("NewNDJSONRegistry: %v", err)
	}

	if _, err := Migrate(context.Background(), reg, rulesPath, accuracyDir, t.TempDir()); err != nil {
		t.Fatalf("Migrate (first): %v", err)
	}
	second, err := Migrate(context.Background(), reg, rulesPath, accuracyDir, t.TempDir())
	if err != nil {
		t.Fatalf("Migrate (second): %v", err)
	}
	if second.Promote {
		t.Fatalf("expected second run against unchanged rules to be a no-op, got Promote=true")
	}
}
