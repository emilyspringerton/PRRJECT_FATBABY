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

type Event struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	OccurredAt   time.Time       `json:"occurred_at"`
	AggregateKey string          `json:"aggregate_key,omitempty"`
	Source       string          `json:"source,omitempty"`
	Data         json.RawMessage `json:"data"`
}

type Record struct {
	Sequence   uint64    `json:"sequence"`
	Event      Event     `json:"event"`
	AppendedAt time.Time `json:"appended_at"`
}

type EventStore interface {
	Append(ctx context.Context, events ...Event) ([]Record, error)
	ReadFrom(ctx context.Context, fromSequence uint64, limit int) ([]Record, error)
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
	return event, nil
}
