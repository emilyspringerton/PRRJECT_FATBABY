package newssite

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

// DocEntry is the display-ready view of one source_document_persisted record.
type DocEntry struct {
	Seq         uint64
	Identity    string
	Ticker      string
	SourceType  string
	Form        string
	DocumentURL string
	BodyPreview string
	FullText    string
	CharCount   int
	FilingDate  string    // SEC filing date (e.g. "2019-04-26"); use for display
	PersistedAt time.Time // pipeline-index timestamp; shown as footnote only
}

// ReadLatest scans the event store from the tail and returns up to limit
// source_document_persisted entries, newest first.
// TODO: optimize this by adding reverse scan support in the event store.
func ReadLatest(ctx context.Context, store eventstore.EventStore, limit int) ([]DocEntry, error) {
	if limit <= 0 {
		return []DocEntry{}, nil
	}

	all := make([]DocEntry, 0, limit)
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil {
			return nil, err
		}
		if len(recs) == 0 {
			break
		}
		for _, rec := range recs {
			if rec.Event.Type != "source_document_persisted" {
				continue
			}
			entry, ok := toEntry(rec)
			if !ok {
				continue
			}
			all = append(all, entry)
		}
		from = recs[len(recs)-1].Sequence + 1
	}

	reverse(all)
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// ReadByIdentity returns the single DocEntry whose Identity matches the given
// string, or (DocEntry{}, false) if not found.
func ReadByIdentity(ctx context.Context, store eventstore.EventStore, identity string) (DocEntry, bool, error) {
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil {
			return DocEntry{}, false, err
		}
		if len(recs) == 0 {
			return DocEntry{}, false, nil
		}
		for _, rec := range recs {
			if rec.Event.Type != "source_document_persisted" {
				continue
			}
			entry, ok := toEntry(rec)
			if !ok {
				continue
			}
			if entry.Identity == identity {
				return entry, true, nil
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
}

func toEntry(rec eventstore.Record) (DocEntry, bool) {
	var doc intelligence.SourceDocument
	if err := json.Unmarshal(rec.Event.Data, &doc); err != nil {
		return DocEntry{}, false
	}
	if doc.Ticker == "" {
		return DocEntry{}, false
	}
	return DocEntry{
		Seq:         rec.Sequence,
		Identity:    doc.Identity,
		Ticker:      doc.Ticker,
		SourceType:  doc.SourceType,
		Form:        doc.Form,
		DocumentURL: doc.DocumentURL,
		BodyPreview: previewText(doc.CleanedText, 800),
		FullText:    doc.CleanedText,
		CharCount:   doc.CleanedCharCount,
		FilingDate:  doc.FilingDate,
		PersistedAt: doc.PersistedAt,
	}, true
}

func previewText(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	trimmed := string(runes[:maxRunes])
	lastSpace := strings.LastIndex(trimmed, " ")
	if lastSpace > 0 {
		return strings.TrimSpace(trimmed[:lastSpace])
	}
	return strings.TrimSpace(trimmed)
}

func reverse(entries []DocEntry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}
