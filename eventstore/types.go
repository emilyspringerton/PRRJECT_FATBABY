package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrEmptyAppend      = errors.New("append requires at least one event")
	ErrInvalidEventID   = errors.New("event id is required")
	ErrInvalidEventType = errors.New("event type is required")
	ErrInvalidData      = errors.New("event data is required")
)

// DiscoveryIdentity acts as an explicit composite payload signature field.
type DiscoveryIdentity struct {
	CIK             string `json:"cik,omitempty"`
	AccessionNumber string `json:"accession_number,omitempty"`
	URL             string `json:"url,omitempty"`
}

// Event is the canonical cross-service envelope for replay, enrichment, analytics, and training.
type Event struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Source       string            `json:"source"`
	OccurredAt   time.Time         `json:"occurred_at"`
	IngestedAt   time.Time         `json:"ingested_at"`
	PartitionKey string            `json:"partition_key,omitempty"`
	Identity     DiscoveryIdentity `json:"identity,omitempty"`
	Data         json.RawMessage   `json:"data"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Record struct {
	Sequence   uint64    `json:"sequence"`
	Event      Event     `json:"event"`
	AppendedAt time.Time `json:"appended_at"`
}

type EventStore interface {
	Append(ctx context.Context, events ...Event) ([]Record, error)
	ReadFrom(ctx context.Context, fromSequence uint64, limit int) ([]Record, error)
	// Scan streams every record with Sequence >= fromSeq to fn, in order, reading
	// each journal file exactly once without materializing it in full. fn
	// returning an error stops the scan and that error is returned.
	Scan(ctx context.Context, fromSeq uint64, fn func(Record) error) error
	LatestSequence(ctx context.Context) (uint64, error)
	Close() error
}

func normalizeAndValidateEvent(event Event) (Event, error) {
	if event.ID == "" {
		return Event{}, ErrInvalidEventID
	}
	if event.Type == "" {
		return Event{}, ErrInvalidEventType
	}
	if len(event.Data) == 0 {
		return Event{}, ErrInvalidData
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.IngestedAt.IsZero() {
		event.IngestedAt = time.Now().UTC()
	}
	if event.Source == "" {
		event.Source = "unknown"
	}
	return event, nil
}
