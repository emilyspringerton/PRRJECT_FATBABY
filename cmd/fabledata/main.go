// Command fabledata builds a content-addressed training snapshot from the
// EPS headline oracle corpus (HQ-SPEC-AI-103 §4a, build order step 1).
package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"github.com/example/prrject-fatbaby/internal/fabledata"
)

func main() {
	epsDir := flag.String("eps-dir", "var/eps", "directory containing articles.ndjson + oracle.ndjson")
	outDir := flag.String("out-dir", "var/fabledata", "directory to write the snapshot under (snapshots/ subdir)")
	tombstonesPath := flag.String("tombstones", "", "path to a JSON array of example IDs to exclude (defaults to <out-dir>/tombstones.json)")
	flag.Parse()

	tsPath := *tombstonesPath
	if tsPath == "" {
		tsPath = filepath.Join(*outDir, "tombstones.json")
	}
	tombstones, err := fabledata.LoadTombstones(tsPath)
	if err != nil {
		log.Fatalf("fabledata: load tombstones: %v", err)
	}

	examples, err := fabledata.BuildExamples(*epsDir, tombstones)
	if err != nil {
		log.Fatalf("fabledata: build examples: %v", err)
	}

	snap := fabledata.BuildSnapshot(examples)
	path, err := fabledata.WriteSnapshot(*outDir, snap, examples)
	if err != nil {
		log.Fatalf("fabledata: write snapshot: %v", err)
	}

	fmt.Printf("fabledata: %d graded examples -> %s (manifest %s)\n", snap.RecordCount, path, snap.ManifestHash)
}
