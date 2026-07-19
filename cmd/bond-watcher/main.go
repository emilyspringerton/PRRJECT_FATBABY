// bond-watcher fetches the latest observation for each tracked FRED
// treasury/credit-spread series once daily and records it into the event
// store as an audit trail. Meant to run once per day from a systemd timer
// (same idiom as movers-watcher, earnings-alert), not a long-running poll
// loop -- FRED's daily series update once per business day, so polling
// more often than that buys nothing.
//
// Gates on marketcal.IsMarketDay itself, same reasoning as movers-watcher:
// a timer misfire or manual run on a weekend/holiday should no-op, not
// record a duplicate/stale snapshot.
//
// See PRRJECT_FATBABY/docs/northstar/auto-generated-articles.md (Phase 5).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/bonddata"
	"github.com/example/prrject-fatbaby/internal/marketcal"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "eventstore root")
	force := flag.Bool("force", false, "record even if today is not a recognized market day (testing only)")
	dryRun := flag.Bool("dry-run", false, "fetch and print but do not write to the event store")
	flag.Parse()

	logger := log.New(os.Stdout, "bond-watcher ", log.LstdFlags|log.LUTC)
	now := time.Now().UTC()

	if !*force && !marketcal.IsMarketDay(now) {
		if name := marketcal.HolidayName(now); name != "" {
			logger.Printf("not a market day (%s) -- skipping", name)
		} else {
			logger.Printf("not a market day (weekend) -- skipping")
		}
		return
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 15 * time.Second}

	var store eventstore.EventStore
	if !*dryRun {
		s, err := eventstore.NewFileStore(*storeRoot)
		if err != nil {
			logger.Fatalf("open store: %v", err)
		}
		defer s.Close()
		store = s
	}

	recorded := 0
	for _, series := range bonddata.TrackedSeries {
		obs, err := bonddata.FetchLatest(ctx, client, series)
		if err != nil {
			logger.Printf("WARNING: fetch %s failed: %v (skipping)", series.ID, err)
			continue
		}
		logger.Printf("%s (%s) = %.2f as of %s", series.Label, series.ID, obs.Value, obs.Date.Format("2006-01-02"))

		if *dryRun {
			continue
		}
		if err := emitObservation(ctx, store, obs); err != nil {
			logger.Printf("WARNING: emit %s failed: %v", series.ID, err)
			continue
		}
		recorded++
	}
	logger.Printf("done: %d/%d series recorded", recorded, len(bonddata.TrackedSeries))
}

// emitObservation records one bond_yield_observed event. ID is
// deterministic per (series, date) -- eventstore.Append itself does not
// dedupe by ID (confirmed by reading file_store.go, not assumed), so a
// manual re-run the same day still appends a second record; the
// deterministic ID exists so a downstream consumer building an index can
// dedupe on it if that ever matters, not as an append-time guarantee.
func emitObservation(ctx context.Context, store eventstore.EventStore, obs bonddata.Observation) error {
	data, err := json.Marshal(obs)
	if err != nil {
		return err
	}
	_, err = store.Append(ctx, eventstore.Event{
		ID:         fmt.Sprintf("bond_yield_observed:%s:%s", obs.SeriesID, obs.Date.Format("2006-01-02")),
		Type:       "bond_yield_observed",
		Source:     "bond-watcher",
		OccurredAt: obs.Date,
		Data:       data,
	})
	return err
}
