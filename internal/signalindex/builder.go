package signalindex

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

// Build scans the entire store from fromSeq and populates idx. fromSeq is
// normally 1 (full history); pass a higher value only via the documented
// -replay-from-seq emergency degraded-mode lever.
func Build(ctx context.Context, store eventstore.EventStore, idx *Index, fromSeq uint64, logger *log.Logger) error {
	scanned := 0
	return store.Scan(ctx, fromSeq, func(rec eventstore.Record) error {
		if err := idx.Ingest(rec); err != nil {
			return err
		}
		scanned++
		if scanned%1000 == 0 && logger != nil {
			logger.Printf("signalindex build scanned=%d latest_seq=%d", scanned, rec.Sequence)
		}
		return nil
	})
}

// Tail starts a goroutine that polls the store every pollInterval for new records.
// The first poll runs immediately (not after waiting a full pollInterval) so
// callers blocking on the returned channel aren't held up by an idle tick —
// Build() has typically just caught the index up to latest_seq, so this first
// poll is normally a fast no-op, not a second scan.
func Tail(ctx context.Context, store eventstore.EventStore, idx *Index, pollInterval time.Duration, logger *log.Logger) (ready <-chan struct{}) {
	readyCh := make(chan struct{})
	poll := func() {
		start := idx.LatestSeq() + 1
		err := store.Scan(ctx, start, func(rec eventstore.Record) error {
			if err := idx.Ingest(rec); err != nil && logger != nil {
				logger.Printf("signalindex tail ingest error: %v", err)
			}
			return nil
		})
		if err != nil && logger != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("signalindex tail read error: %v", err)
		}
	}
	go func() {
		defer close(readyCh)
		if ctx.Err() != nil {
			return
		}
		poll()
		readyCh <- struct{}{}
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				poll()
			}
		}
	}()
	return readyCh
}
