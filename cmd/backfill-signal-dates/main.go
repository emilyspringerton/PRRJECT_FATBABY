// backfill-signal-dates repairs signals.ndjson records that were written without
// a filing_date by looking up the correct date from filing_discovered events in the
// event store. Run once after upgrading from a version that stored signals without
// the filing_date field.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "secwatch event store root")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "entity-graph directory")
	dryRun := flag.Bool("dry-run", false, "print what would change without writing")
	flag.Parse()

	logger := log.New(os.Stdout, "backfill-signal-dates ", log.LstdFlags|log.LUTC)

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	dateIndex := buildFilingDateIndex(ctx, store, logger)
	logger.Printf("loaded %d filing dates from event store", len(dateIndex))

	signals, err := entitygraph.LoadSignals(*graphDir)
	if err != nil {
		logger.Fatalf("load signals: %v", err)
	}
	logger.Printf("loaded %d signals", len(signals))

	fixed := 0
	notFound := 0
	for i, s := range signals {
		if s.FilingDate != "" {
			continue
		}
		sourceID := s.Metadata["source_identity"]
		if sourceID == "" {
			continue
		}
		date, ok := dateIndex[sourceID]
		if !ok {
			logger.Printf("no date found for source_identity=%s signal_id=%s", sourceID, s.SignalID)
			notFound++
			continue
		}
		logger.Printf("backfill signal_id=%s source_identity=%s filing_date=%s", s.SignalID, sourceID, date)
		signals[i].FilingDate = date
		fixed++
	}
	logger.Printf("backfill complete: fixed=%d not_found=%d dry_run=%v", fixed, notFound, *dryRun)

	if *dryRun || fixed == 0 {
		return
	}

	if err := entitygraph.RewriteSignals(*graphDir, signals); err != nil {
		logger.Fatalf("rewrite signals: %v", err)
	}
	logger.Printf("signals.ndjson rewritten")
}

// buildFilingDateIndex scans the event store for filing_discovered events and
// returns a map from filing identity (CIK:ACCESSION) to SEC filing date.
func buildFilingDateIndex(ctx context.Context, store eventstore.EventStore, logger *log.Logger) map[string]string {
	index := make(map[string]string)
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil || len(recs) == 0 {
			break
		}
		for _, r := range recs {
			if r.Event.Type != "filing_discovered" {
				continue
			}
			var ev secwatch.FilingDiscoveredEvent
			if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
				continue
			}
			if ev.FilingDate == "" || ev.CIK == "" || ev.AccessionNumber == "" {
				continue
			}
			id := secwatch.FilingIdentity(ev.CIK, ev.AccessionNumber)
			if _, exists := index[id]; !exists {
				index[id] = ev.FilingDate
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
	return index
}
