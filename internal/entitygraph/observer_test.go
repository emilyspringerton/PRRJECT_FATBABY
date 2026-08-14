package entitygraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPublishObservation_DoesNotClobberSameSecondArchive is a regression
// test for a real bug found live 2026-08-14: PublishObservation wrote
// straight to <timestamp-to-the-second>.json with no collision check, so
// two observations landing in the same second silently overwrote each
// other -- confirmed live, a real founder directive lost this way
// mid-session (entity-graph's own periodic health report happened to land
// on the exact same second as a `emily observe` CLI call). emily observe's
// own writer (internal/obs/writer.go) already had this exact numeric-suffix
// collision guard, added 2026-08-09 after losing observations the same
// way -- entity-graph's independently-implemented writer never got it.
func TestPublishObservation_DoesNotClobberSameSecondArchive(t *testing.T) {
	dir := t.TempDir()
	const ts = "2026-08-14T00-46-01"

	obs1 := Observation{Timestamp: ts, Source: "entity-graph", Status: "ok"}
	obs2 := Observation{Timestamp: ts, Source: "entity-graph", Status: "needs_attention"}

	if err := publishObservationAt(obs1, dir, ts); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := publishObservationAt(obs2, dir, ts); err != nil {
		t.Fatalf("second publish: %v", err)
	}

	firstArchive := filepath.Join(dir, ts+".json")
	got1, err := os.ReadFile(firstArchive)
	if err != nil {
		t.Fatalf("read first archive: %v", err)
	}
	var d1 Observation
	if err := json.Unmarshal(got1, &d1); err != nil {
		t.Fatalf("unmarshal first archive: %v", err)
	}
	if d1.Status != "ok" {
		t.Errorf("first archive was overwritten by the second write -- collision guard did not fire: got status %q, want \"ok\"", d1.Status)
	}

	secondArchive := filepath.Join(dir, ts+"-2.json")
	got2, err := os.ReadFile(secondArchive)
	if err != nil {
		t.Fatalf("second observation was not written to a distinct file %s: %v", secondArchive, err)
	}
	var d2 Observation
	if err := json.Unmarshal(got2, &d2); err != nil {
		t.Fatalf("unmarshal second archive: %v", err)
	}
	if d2.Status != "needs_attention" {
		t.Errorf("second archive has wrong content: got status %q, want \"needs_attention\"", d2.Status)
	}
}

// TestPublishObservation_LatestJSONAlwaysReflectsMostRecentWrite confirms
// latest.json (unlike the archive) is meant to always be overwritten --
// only the archive needs collision protection, latest.json's whole point is
// being the current snapshot.
func TestPublishObservation_LatestJSONAlwaysReflectsMostRecentWrite(t *testing.T) {
	dir := t.TempDir()
	if err := publishObservationAt(Observation{Timestamp: "a", Status: "ok"}, dir, "2026-08-14T00-00-00"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := publishObservationAt(Observation{Timestamp: "b", Status: "needs_attention"}, dir, "2026-08-14T00-00-01"); err != nil {
		t.Fatalf("second publish: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatalf("read latest.json: %v", err)
	}
	var got Observation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status != "needs_attention" {
		t.Errorf("latest.json = %q, want the second (most recent) write", got.Status)
	}
}
