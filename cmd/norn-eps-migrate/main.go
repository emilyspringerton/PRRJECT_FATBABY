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

	"norn/pkg/apples"
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
		if err != nil {
			log.Fatalf("norn-eps-migrate: promotion succeeded but re-reading golden artifact failed: %v", err)
		}
		fmt.Printf("golden eps_extractor artifact: %s\n", golden.Hash)
		// Best-effort: PostPromotionFromEnv returns apples.ErrCredentialsNotSet
		// when IDUNA_BASE_URL/IDUNA_AGENT_SECRET aren't set — the promotion
		// itself is already durably recorded in the registry regardless, so
		// this is a log line, never a fatal error.
		if id, err := apples.PostPromotionFromEnv(context.Background(), golden, decision, "PRRJECT_FATBABY"); err != nil {
			log.Printf("norn-eps-migrate: %v (promotion is still recorded in the registry)", err)
		} else {
			fmt.Printf("Apple #%d filed\n", id)
		}
	}
}
