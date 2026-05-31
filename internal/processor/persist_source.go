package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/secwatch"
)

// persistSourceDocument emits a source_document_persisted event into the store.
// Errors are returned to the caller; the caller decides whether to abort or continue.
func persistSourceDocument(
	ctx context.Context,
	store eventstore.EventStore,
	filing secwatch.FilingDiscoveredEvent,
	identity string,
	sourceType string,
	cleanedText string,
) error {
	doc := intelligence.SourceDocument{
		Identity:         identity,
		Ticker:           filing.Ticker,
		SourceType:       sourceType,
		Form:             filing.EffectiveForm(),
		DocumentURL:      filing.PrimaryDocument,
		CleanedText:      cleanedText,
		CleanedCharCount: len(cleanedText),
		FilingDate:       filing.FilingDate,
		PersistedAt:      time.Now().UTC(),
	}
	payload, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal source document: %w", err)
	}
	_, err = store.Append(ctx, eventstore.Event{
		ID:           "source_document_persisted:" + identity,
		Type:         "source_document_persisted",
		PartitionKey: identity,
		Source:       "processor",
		Data:         payload,
	})
	return err
}

// sourceDocumentExists scans the event store for an existing
// source_document_persisted event with the given identity.
// Used to skip re-persisting on replay, consistent with signalExists.
func sourceDocumentExists(ctx context.Context, store eventstore.EventStore, identity string) bool {
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil || len(recs) == 0 {
			return false
		}
		for _, r := range recs {
			if r.Event.Type == "source_document_persisted" && r.Event.PartitionKey == identity {
				return true
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}
