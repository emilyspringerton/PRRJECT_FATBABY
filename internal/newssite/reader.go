package newssite

import (
	"context"
	"encoding/json"
	"sort"
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

// readChunkSize is the number of event store records fetched per backward step.
// Sized to hold ~a day's worth of events without excessive memory pressure.
const readChunkSize = 512

// ReadLatest returns up to limit source_document_persisted entries, newest first.
// It walks backward from the latest sequence in chunks so it only reads as many
// events as needed, not the entire store. This keeps latency low even when the
// store has grown to hundreds of thousands of events.
func ReadLatest(ctx context.Context, store eventstore.EventStore, limit int) ([]DocEntry, error) {
	if limit <= 0 {
		return []DocEntry{}, nil
	}

	latest, err := store.LatestSequence(ctx)
	if err != nil {
		return nil, err
	}
	if latest == 0 {
		return []DocEntry{}, nil
	}

	result := make([]DocEntry, 0, limit)

	// Walk backward from latest in chunks of readChunkSize.
	// cursor is the exclusive upper bound for the next read.
	cursor := latest + 1
	for cursor > 1 && len(result) < limit {
		start := uint64(1)
		if cursor > readChunkSize {
			start = cursor - readChunkSize
		}
		size := int(cursor - start)

		recs, err := store.ReadFrom(ctx, start, size)
		if err != nil {
			return nil, err
		}

		// Scan this chunk newest-first (reverse order).
		for i := len(recs) - 1; i >= 0 && len(result) < limit; i-- {
			if recs[i].Event.Type != "source_document_persisted" {
				continue
			}
			entry, ok := toEntry(recs[i])
			if !ok {
				continue
			}
			result = append(result, entry)
		}

		if start == 1 {
			break
		}
		cursor = start
	}

	// Sort by FilingDate descending so historical re-ingested filings don't
	// appear as breaking news above recent ones.
	sort.Slice(result, func(i, j int) bool {
		fi, fj := result[i].FilingDate, result[j].FilingDate
		switch {
		case fi != "" && fj != "":
			return fi > fj
		case fi != "":
			return true
		case fj != "":
			return false
		default:
			return result[i].PersistedAt.After(result[j].PersistedAt)
		}
	})

	return result, nil
}

// ReadAtSequence fetches exactly one record at a known sequence number — the
// fast path for detail pages when the caller already knows the sequence
// (e.g. from docindex.Index.ByIdentity's O(1) lookup). Unlike ReadByIdentity,
// this does not scan the store from the beginning: FileStore.ReadFrom skips
// whole day-partitioned files entirely before fromSequence, so this touches
// at most the one partition containing the target record.
func ReadAtSequence(ctx context.Context, store eventstore.EventStore, seq uint64) (DocEntry, bool, error) {
	recs, err := store.ReadFrom(ctx, seq, 1)
	if err != nil {
		return DocEntry{}, false, err
	}
	if len(recs) == 0 || recs[0].Sequence != seq {
		return DocEntry{}, false, nil
	}
	entry, ok := toEntry(recs[0])
	return entry, ok, nil
}

// ReadByIdentity returns the single DocEntry whose Identity matches the given
// string, or (DocEntry{}, false) if not found. This scans the store from the
// beginning — O(entire history) — and should only be used as a fallback when
// the in-memory docindex hasn't indexed the identity yet (e.g. mid-rebuild).
// Prefer docindex.Index.ByIdentity + ReadAtSequence, the fast path.
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
