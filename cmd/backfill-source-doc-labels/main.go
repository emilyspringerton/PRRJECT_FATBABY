// Command backfill-source-doc-labels is the S160-05/S166-01 one-off
// migration: re-derives the correct Form/SourceType/FilingDate for
// source_document_persisted records that were mislabeled by the
// pre-S160-01 sourceTypeForForm default (everything that wasn't
// successfully read as an 8-K silently became "press_release" with an
// empty Form).
//
// S160-01 (2026-07-19) fixed the labeling logic going forward but never
// backfilled what it had already written. Those bad records were
// permanently stuck: processor's own seen.hasSource/sourceDocumentExists
// dedup means a corrected record is never regenerated for an identity
// that already has one. Measured live 2026-08-14: 1,104 of 13,744
// source_document_persisted records (real SEC filings under
// https://www.sec.gov/Archives/..., not actual press releases) across 11
// tickers. This is also the direct root cause of S166-01 (ticker page
// earnings widget showing 2003/2008-era dates): earnings-calendar
// requires Form=="8-K" or SourceType=="sec_8k" plus real Item 2.02 text
// to derive a confirmed report date, and these records never matched.
//
// This tool does NOT re-fetch document content from SEC (expensive,
// unnecessary, and a real network-load concern on a box with a known OOM
// history) -- it reuses each broken record's own already-fetched
// CleanedText/CleanedCharCount, and only corrects Form/SourceType/
// FilingDate by cross-referencing the record's own originating
// filing_discovered event (which was always correct; the mislabeling
// only ever happened in processor's own persist step). Emits a new
// source_document_persisted event at the same PartitionKey/identity --
// append-only, doesn't touch history -- which internal/newssite/docindex's
// now-fixed last-write-wins Ingest (see 2026-08-14 fix) picks up as a
// correction, and which earnings-calendar's own tailing scan picks up
// naturally on its next poll.
//
// One-shot by design (matches cmd/norn-eps-migrate, cmd/projector -one-shot).
// Idempotent: a record already correctly labeled is left alone, so
// re-running after a partial run or after new data lands is safe.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/processor"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "eventstore root")
	dryRun := flag.Bool("dry-run", false, "report what would change without appending corrections")
	flag.Parse()

	logger := log.New(os.Stdout, "backfill-source-doc-labels ", log.LstdFlags|log.LUTC)

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Pass 1: index every filing_discovered event by its identity, the
	// same "<cik>:<accession>" format source_document_persisted's own
	// Identity field uses -- this is the always-correct source of truth
	// for Form/FilingDate.
	filings := make(map[string]secwatch.FilingDiscoveredEvent)
	if err := store.Scan(ctx, 1, func(r eventstore.Record) error {
		if r.Event.Type != "filing_discovered" {
			return nil
		}
		var ev secwatch.FilingDiscoveredEvent
		if json.Unmarshal(r.Event.Data, &ev) != nil {
			return nil
		}
		id := secwatch.FilingIdentity(ev.CIK, ev.AccessionNumber)
		filings[id] = ev
		return nil
	}); err != nil {
		logger.Fatalf("scan filing_discovered: %v", err)
	}
	logger.Printf("indexed %d filing_discovered events", len(filings))

	// Pass 2: find broken source_document_persisted records and correct
	// the ones we have a real filing_discovered match for.
	checked, broken, corrected, noMatch := 0, 0, 0, 0
	if err := store.Scan(ctx, 1, func(r eventstore.Record) error {
		if r.Event.Type != "source_document_persisted" {
			return nil
		}
		var doc intelligence.SourceDocument
		if json.Unmarshal(r.Event.Data, &doc) != nil {
			return nil
		}
		checked++
		if !isMislabeled(doc) {
			return nil
		}
		broken++

		filing, ok := filings[doc.Identity]
		if !ok {
			noMatch++
			logger.Printf("no filing_discovered match for identity=%s ticker=%s -- skipped", doc.Identity, doc.Ticker)
			return nil
		}
		form := filing.EffectiveForm()
		if form == "" {
			noMatch++
			logger.Printf("filing_discovered for identity=%s has no form either -- skipped", doc.Identity)
			return nil
		}

		correctedDoc := doc
		correctedDoc.Form = form
		correctedDoc.SourceType = processor.SourceTypeForForm(form)
		if correctedDoc.FilingDate == "" {
			correctedDoc.FilingDate = filing.FilingDate
		}
		correctedDoc.PersistedAt = time.Now().UTC()

		logger.Printf("correcting identity=%s ticker=%s form %q->%q source_type %q->%q",
			doc.Identity, doc.Ticker, doc.Form, correctedDoc.Form, doc.SourceType, correctedDoc.SourceType)

		if *dryRun {
			corrected++
			return nil
		}
		payload, err := json.Marshal(correctedDoc)
		if err != nil {
			return fmt.Errorf("marshal corrected doc %s: %w", doc.Identity, err)
		}
		if _, err := store.Append(ctx, eventstore.Event{
			ID:           "source_document_persisted:" + doc.Identity,
			Type:         "source_document_persisted",
			PartitionKey: doc.Identity,
			Source:       "backfill-source-doc-labels",
			Data:         payload,
		}); err != nil {
			return fmt.Errorf("append correction for %s: %w", doc.Identity, err)
		}
		corrected++
		return nil
	}); err != nil {
		logger.Fatalf("scan source_document_persisted: %v", err)
	}

	mode := "LIVE"
	if *dryRun {
		mode = "DRY-RUN"
	}
	logger.Printf("%s complete: checked=%d broken=%d corrected=%d no_match=%d", mode, checked, broken, corrected, noMatch)
}

// isMislabeled is the same broken-record signature found live 2026-08-14:
// a real SEC EDGAR filing (not PR Newswire content) that got the pre-S160-01
// default treatment -- empty Form, SourceType wrongly defaulted to
// "press_release".
func isMislabeled(doc intelligence.SourceDocument) bool {
	return doc.Form == "" &&
		doc.SourceType == "press_release" &&
		strings.HasPrefix(doc.DocumentURL, "https://www.sec.gov/Archives")
}
