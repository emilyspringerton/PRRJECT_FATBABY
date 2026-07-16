// Command norn-eps-migrate runs the S141-02 NORN migration for the EPS
// headline extractor: promotes the current internal/eps.Extract
// implementation as the first "eps_extractor" artifact NORN governs (or, on
// a later run against changed code, gates a real candidate against the
// incumbent). See internal/eps/norngate for the full doctrine and the
// documented gap between this migration's fixture-based eval set and
// PRIME-101 §8.2's literal "replay real historical events" acceptance text.
//
// One-shot by design, matching cmd/projector's -one-shot convention — this
// is not a daemon. Re-run it whenever internal/eps/extract.go changes; it
// no-ops when the content hash is unchanged.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"norn/pkg/norn"

	"github.com/example/prrject-fatbaby/internal/eps/norngate"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "PRRJECT_FATBABY repo root (contains internal/eps/extract.go and fixtures/eps/)")
	registryPath := flag.String("registry", "var/norn/eps_extractor.ndjson", "NORN NDJSON registry path for the eps_extractor kind")
	flag.Parse()

	fixturesDir := *repoRoot + "/fixtures/eps"

	if err := os.MkdirAll(filepath.Dir(*registryPath), 0o755); err != nil {
		log.Fatalf("norn-eps-migrate: create registry dir: %v", err)
	}

	reg, err := norn.NewNDJSONRegistry(*registryPath)
	if err != nil {
		log.Fatalf("norn-eps-migrate: open registry: %v", err)
	}

	decision, err := norngate.Migrate(context.Background(), reg, *repoRoot, fixturesDir)
	if err != nil {
		log.Fatalf("norn-eps-migrate: %v", err)
	}

	fmt.Printf("decision: promote=%v reason=%q\n", decision.Promote, decision.Reason)
	if decision.Promote {
		golden, err := reg.Golden(norngate.ArtifactKind)
		if err == nil {
			fmt.Printf("golden eps_extractor artifact: %s\n", golden.Hash)
		}
	}
}
