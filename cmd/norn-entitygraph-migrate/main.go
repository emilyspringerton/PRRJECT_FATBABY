// Command norn-entitygraph-migrate runs the S141-03 (recon matcher leg)
// NORN migration for entity-graph's signal accuracy/correlation system:
// snapshots the current resolved accuracy history and promotes the current
// rules as the first "entity_graph_rules" artifact NORN governs (or gates a
// later candidate against the incumbent). See internal/entitygraph/norngate
// for the full doctrine, including the disclosed gap between this
// migration's real-but-static grading and a genuine candidate-vs-incumbent
// backtest (which needs a re-simulation capability that doesn't exist yet).
//
// One-shot by design, matching cmd/norn-eps-migrate and cmd/projector's
// -one-shot convention — not a daemon. Re-run it whenever
// config/entity-graph-rules.json changes, or periodically to roll the eval
// snapshot forward as more accuracy records resolve.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"norn/pkg/apples"
	"norn/pkg/norn"

	"github.com/example/prrject-fatbaby/internal/entitygraph/norngate"
)

func main() {
	rulesPath := flag.String("rules", "config/entity-graph-rules.json", "entity-graph rules config path")
	accuracyDir := flag.String("accuracy-dir", "var/entity-graph", "directory containing accuracy.ndjson")
	snapshotDir := flag.String("snapshot-dir", "var/norn/entity-graph-snapshots", "directory for frozen eval snapshots")
	registryPath := flag.String("registry", "var/norn/entity_graph_rules.ndjson", "NORN NDJSON registry path for the entity_graph_rules kind")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*registryPath), 0o755); err != nil {
		log.Fatalf("norn-entitygraph-migrate: create registry dir: %v", err)
	}

	reg, err := norn.NewNDJSONRegistry(*registryPath)
	if err != nil {
		log.Fatalf("norn-entitygraph-migrate: open registry: %v", err)
	}

	decision, err := norngate.Migrate(context.Background(), reg, *rulesPath, *accuracyDir, *snapshotDir)
	if err != nil {
		log.Fatalf("norn-entitygraph-migrate: %v", err)
	}

	fmt.Printf("decision: promote=%v reason=%q\n", decision.Promote, decision.Reason)
	if decision.Promote {
		golden, err := reg.Golden(norngate.ArtifactKind)
		if err != nil {
			log.Fatalf("norn-entitygraph-migrate: promotion succeeded but re-reading golden artifact failed: %v", err)
		}
		fmt.Printf("golden entity_graph_rules artifact: %s\n", golden.Hash)
		if id, err := apples.PostPromotionFromEnv(context.Background(), golden, decision, "PRRJECT_FATBABY"); err != nil {
			log.Printf("norn-entitygraph-migrate: %v (promotion is still recorded in the registry)", err)
		} else {
			fmt.Printf("Apple #%d filed\n", id)
		}
	}
}
