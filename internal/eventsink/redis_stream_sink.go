package eventsink

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/redis/go-redis/v9"
)

// RedisStreamSink publishes each event onto a Redis Stream (XADD) -- the real, durable,
// fan-out-ready queue docs/northstar/KUBERNETES_MIGRATION.md's own "durable queue" decision (S498,
// founder real-time: "part of the kubes plan was to switch to a queue so we can have a durable
// queue to work from for fanout to other stores like mysql") calls for. Once an event lands
// here, every downstream consumer (processor, a future GCS archiver, a future MySQL sink) reads
// it independently via its own real Redis consumer group (see RedisStreamConsumer in this same
// package), each with its own durable, server-tracked offset -- no consumer's failure or lag
// blocks another's, and none of them depend on the producer's own pod disk surviving a restart.
//
// This is the real mechanism the zero-downtime cutover plan rests on: a NEW k8s pod joins the
// SAME consumer group as an ADDITIONAL consumer before the OLD systemd process is stopped, and
// Redis's own per-group delivery (XREADGROUP) guarantees every entry is claimed by exactly one
// live consumer with zero gap -- see RedisStreamConsumer's own header comment for the full
// mechanics.
type RedisStreamSink struct {
	Client *redis.Client
	Stream string // e.g. "fatbaby:events:v1" -- versioned so a future incompatible field change
	// can move to a new stream name without breaking readers still on the old one.
	MaxLen int64 // approximate cap (XADD MAXLEN ~N), 0 = unbounded. Real, deliberate default:
	// this stream is meant to be read promptly by every real consumer group, not held as
	// long-term storage -- long-term storage is the separate GCS archival path
	// (internal/eventsink/s3_sink.go), not this one.
}

// StreamFieldData / StreamFieldID / StreamFieldType -- the real, fixed field names
// RedisStreamSink writes and RedisStreamConsumer reads back. Kept as named constants so the two
// halves of this pair can never silently drift apart.
const (
	StreamFieldData = "data"
	StreamFieldID   = "id"
	StreamFieldType = "type"
)

func (r RedisStreamSink) Write(ctx context.Context, evt eventstore.Event) error {
	if r.Client == nil {
		return fmt.Errorf("redis stream sink: nil client")
	}
	if r.Stream == "" {
		return fmt.Errorf("redis stream sink: empty stream name")
	}
	body, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	args := &redis.XAddArgs{
		Stream: r.Stream,
		Values: map[string]interface{}{
			StreamFieldID:   evt.ID,
			StreamFieldType: evt.Type,
			StreamFieldData: body,
		},
	}
	if r.MaxLen > 0 {
		args.MaxLen = r.MaxLen
		args.Approx = true
	}
	if err := r.Client.XAdd(ctx, args).Err(); err != nil {
		return fmt.Errorf("xadd stream=%s: %w", r.Stream, err)
	}
	return nil
}
