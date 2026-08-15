package fabledata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildSnapshot_DeterministicHash(t *testing.T) {
	examples := []Example{
		{ID: "aaa", SourceIdentity: "sid-1"},
		{ID: "bbb", SourceIdentity: "sid-2"},
	}
	s1 := BuildSnapshot(examples)
	s2 := BuildSnapshot(examples)
	if s1.ManifestHash != s2.ManifestHash {
		t.Errorf("same example set produced different hashes: %s vs %s", s1.ManifestHash, s2.ManifestHash)
	}
	if s1.RecordCount != 2 {
		t.Errorf("RecordCount = %d, want 2", s1.RecordCount)
	}
}

func TestBuildSnapshot_DifferentSetsDifferentHash(t *testing.T) {
	a := BuildSnapshot([]Example{{ID: "aaa"}})
	b := BuildSnapshot([]Example{{ID: "bbb"}})
	if a.ManifestHash == b.ManifestHash {
		t.Error("different example sets produced the same manifest hash")
	}
}

func TestWriteSnapshot_WritesManifestAndExamples(t *testing.T) {
	dir := t.TempDir()
	examples := []Example{
		{ID: "aaa", SourceIdentity: "sid-1", Headline: "h1", Verdict: "confirmed"},
	}
	snap := BuildSnapshot(examples)

	path, err := WriteSnapshot(dir, snap, examples)
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	examplesPath := filepath.Join(dir, "snapshots", snap.ManifestHash+".examples.ndjson")
	if _, err := os.Stat(examplesPath); err != nil {
		t.Fatalf("examples file not written: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var loaded Snapshot
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if loaded.ManifestHash != snap.ManifestHash {
		t.Errorf("loaded hash = %s, want %s", loaded.ManifestHash, snap.ManifestHash)
	}
}

func TestWriteSnapshot_IdenticalSnapshotNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	examples := []Example{{ID: "aaa", SourceIdentity: "sid-1"}}
	snap := BuildSnapshot(examples)

	path1, err := WriteSnapshot(dir, snap, examples)
	if err != nil {
		t.Fatalf("first WriteSnapshot: %v", err)
	}
	firstModTime := mustModTime(t, path1)

	// Re-running with the exact same example set should be a no-op write,
	// not silently overwrite an "immutable" manifest.
	path2, err := WriteSnapshot(dir, snap, examples)
	if err != nil {
		t.Fatalf("second WriteSnapshot: %v", err)
	}
	if path1 != path2 {
		t.Fatalf("paths differ: %s vs %s", path1, path2)
	}
	secondModTime := mustModTime(t, path2)
	if !firstModTime.Equal(secondModTime) {
		t.Error("second WriteSnapshot call modified an existing manifest file -- should have been a no-op")
	}
}

func mustModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}

func TestLoadTombstones_MissingFileReturnsEmptySet(t *testing.T) {
	ts, err := LoadTombstones(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("LoadTombstones on missing file should not error: %v", err)
	}
	if len(ts) != 0 {
		t.Errorf("expected empty tombstone set, got %d", len(ts))
	}
}

func TestLoadTombstones_LoadsRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tombstones.json")
	if err := os.WriteFile(path, []byte(`["abc123", "def456"]`), 0o644); err != nil {
		t.Fatalf("write tombstones: %v", err)
	}
	ts, err := LoadTombstones(path)
	if err != nil {
		t.Fatalf("LoadTombstones: %v", err)
	}
	if !ts["abc123"] || !ts["def456"] {
		t.Errorf("expected both IDs tombstoned, got %v", ts)
	}
	if len(ts) != 2 {
		t.Errorf("expected exactly 2 tombstoned IDs, got %d", len(ts))
	}
}
