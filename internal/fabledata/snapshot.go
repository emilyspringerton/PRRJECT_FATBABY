package fabledata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Snapshot is an immutable, content-addressed manifest of example record
// IDs -- diffable and citable per HQ-SPEC-AI-103 §4a. Training reads a
// snapshot, never the live stores; a training bundle (fabletrain, not this
// package) hashes {this snapshot, tokenizer, code rev, config, seed}.
//
// The manifest stores IDs only, not the example bodies -- the examples
// themselves are written alongside as examples.ndjson (see WriteSnapshot).
// This keeps the manifest small and its hash meaningful as a pure
// membership commitment: "this exact set of graded records, in this exact
// composition," independent of how the example bodies are serialized.
type Snapshot struct {
	ManifestHash string   `json:"manifest_hash"`
	CreatedAt    string   `json:"created_at"` // RFC3339 UTC
	RecordCount  int      `json:"record_count"`
	ExampleIDs   []string `json:"example_ids"` // sorted, matches BuildExamples' own order
}

// BuildSnapshot computes the manifest hash over the given (already sorted
// by ID, per BuildExamples' contract) examples. Same example set -> same
// hash, every time, regardless of when it's rebuilt -- CreatedAt is
// metadata, not part of the hash input.
func BuildSnapshot(examples []Example) Snapshot {
	ids := make([]string, len(examples))
	h := sha256.New()
	for i, e := range examples {
		ids[i] = e.ID
		h.Write([]byte(e.ID))
		h.Write([]byte{0})
	}
	return Snapshot{
		ManifestHash: hex.EncodeToString(h.Sum(nil)),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		RecordCount:  len(examples),
		ExampleIDs:   ids,
	}
}

// WriteSnapshot persists the manifest to <dir>/snapshots/<hash>.json and
// the example bodies to <dir>/snapshots/<hash>.examples.ndjson. Refuses to
// overwrite an existing snapshot with the same hash -- immutability isn't
// just a doc comment, a re-run over unchanged data is a no-op write, and a
// re-run over changed data gets a different hash and a new file, never a
// silent overwrite of history.
func WriteSnapshot(dir string, snap Snapshot, examples []Example) (string, error) {
	snapDir := filepath.Join(dir, "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		return "", err
	}
	manifestPath := filepath.Join(snapDir, snap.ManifestHash+".json")
	if _, err := os.Stat(manifestPath); err == nil {
		return manifestPath, nil // identical snapshot already committed, nothing to do
	}

	examplesPath := filepath.Join(snapDir, snap.ManifestHash+".examples.ndjson")
	ef, err := os.Create(examplesPath)
	if err != nil {
		return "", err
	}
	for _, e := range examples {
		line, err := json.Marshal(e)
		if err != nil {
			ef.Close()
			os.Remove(examplesPath)
			return "", err
		}
		if _, err := fmt.Fprintf(ef, "%s\n", line); err != nil {
			ef.Close()
			os.Remove(examplesPath)
			return "", err
		}
	}
	if err := ef.Close(); err != nil {
		os.Remove(examplesPath)
		return "", err
	}

	manifestBytes, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		os.Remove(examplesPath)
		return "", err
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		os.Remove(examplesPath)
		return "", err
	}
	return manifestPath, nil
}

// LoadTombstones reads a set of example IDs to exclude from future
// snapshots -- HQ-SPEC-AI-103 §4a's "contamination guard (eval records are
// tombstoned out of every training snapshot by hash -- mechanical, not
// procedural)". Empty/missing file is not an error: fableeval (S145-02)
// hasn't landed yet, so there are no eval records to tombstone against
// today -- this reads whatever tombstone list exists, including none.
func LoadTombstones(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
