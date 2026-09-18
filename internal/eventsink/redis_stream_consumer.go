package eventsink

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/redis/go-redis/v9"
)

// StreamEntry is one decoded Redis Stream entry -- the real event plus the real Redis-assigned
// entry ID (needed to Ack it once this consumer's own downstream write has actually succeeded).
type StreamEntry struct {
	ID    string
	Event eventstore.Event
}

// RedisStreamConsumer reads RedisStreamSink's own stream via a real, durable consumer group --
// this is the real read-side half of the "durable queue for fanout to other stores like mysql"
// decision, and the real mechanism the zero-downtime cutover plan relies on.
//
// Redis delivers each stream entry to exactly one consumer WITHIN a group (XREADGROUP), tracked
// in that group's own Pending Entries List (PEL) until XAck. That single property is what makes
// the "run the new pod alongside the old process, then flip" cutover pattern safe with zero gap:
// during the overlap window, whichever consumer asks first (old process or new pod, both real,
// both alive) gets the next entry -- nothing is delivered twice under steady operation, and
// nothing is silently dropped even if one consumer is killed mid-flight (its un-acked entries
// stay in the PEL, recoverable via XClaim/XAutoClaim, not lost the way an in-memory queue's
// in-flight work would be). This is a real, structural improvement over the old systemd-process
// cutover story (stop old, start new -- a hard gap by construction) for anything reading FROM
// this queue.
//
// One real, separate cutover concern this type does NOT solve: the PRODUCER side (secwatch/
// prwatch-body actually polling an external API like SEC EDGAR on a timer) has no equivalent
// "shared group" primitive to lean on, since polling an external HTTP endpoint isn't reading
// from Redis. That side's own cutover safety instead rests on running the new pod and old
// process in brief parallel and relying on each event's own stable, pre-existing eventstore.
// Event.ID for idempotent dedup downstream -- a real, different, already-solved property (see
// docs/northstar/KUBERNETES_MIGRATION.md's own cutover section for the full split).
type RedisStreamConsumer struct {
	Client   *redis.Client
	Stream   string
	Group    string // e.g. "processor", "gcs-archiver", "mysql-sink" -- one real, independent,
	// durably-tracked read position per real downstream consumer, not shared across them.
	Consumer string // this specific process/pod's own consumer name within the group (e.g. a
	// real pod name) -- distinct consumers in the same group are what makes the
	// overlap-then-flip cutover pattern above actually work.
}

// EnsureGroup creates the consumer group (and the stream itself, via MKSTREAM) if it doesn't
// already exist -- idempotent, safe to call on every process startup, matching this repo's own
// established "safe to call repeatedly" convention for setup-on-boot code. "0" as the starting
// ID means a brand-new group sees every entry already in the stream, not just future ones --
// the real, correct default for a consumer that needs to catch up on history, not just tail it.
func (r RedisStreamConsumer) EnsureGroup(ctx context.Context) error {
	if r.Client == nil {
		return fmt.Errorf("redis stream consumer: nil client")
	}
	err := r.Client.XGroupCreateMkStream(ctx, r.Stream, r.Group, "0").Err()
	if err != nil && !isBusyGroupErr(err) {
		return fmt.Errorf("create consumer group stream=%s group=%s: %w", r.Stream, r.Group, err)
	}
	return nil
}

// isBusyGroupErr -- Redis's own real, expected "already exists" response (BUSYGROUP) when
// EnsureGroup runs against a group a prior process instance already created. Not a real failure.
func isBusyGroupErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

// ReadNew reads up to count new (never-before-delivered-to-this-group) entries, blocking for up
// to block waiting for at least one to arrive. A zero-length, nil-error result means the block
// window elapsed with nothing new -- the real, honest "poll again" signal, not an error.
func (r RedisStreamConsumer) ReadNew(ctx context.Context, count int64, block time.Duration) ([]StreamEntry, error) {
	if r.Client == nil {
		return nil, fmt.Errorf("redis stream consumer: nil client")
	}
	res, err := r.Client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    r.Group,
		Consumer: r.Consumer,
		Streams:  []string{r.Stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("xreadgroup stream=%s group=%s: %w", r.Stream, r.Group, err)
	}
	return decodeStreamMessages(res)
}

func decodeStreamMessages(res []redis.XStream) ([]StreamEntry, error) {
	var out []StreamEntry
	for _, stream := range res {
		for _, msg := range stream.Messages {
			raw, ok := msg.Values[StreamFieldData]
			if !ok {
				return nil, fmt.Errorf("stream entry %s missing %q field", msg.ID, StreamFieldData)
			}
			rawStr, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("stream entry %s field %q is not a string", msg.ID, StreamFieldData)
			}
			var evt eventstore.Event
			if err := json.Unmarshal([]byte(rawStr), &evt); err != nil {
				return nil, fmt.Errorf("decode stream entry %s: %w", msg.ID, err)
			}
			out = append(out, StreamEntry{ID: msg.ID, Event: evt})
		}
	}
	return out, nil
}

// Ack marks entries as durably processed -- removes them from the group's PEL. A caller must
// only call this AFTER its own downstream write (a real MySQL insert, a real GCS upload, etc.)
// has actually succeeded -- at-least-once delivery is only real if Ack happens after the real
// work completes, not before, matching the standard, correct consumer-group usage pattern.
func (r RedisStreamConsumer) Ack(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	if r.Client == nil {
		return fmt.Errorf("redis stream consumer: nil client")
	}
	if err := r.Client.XAck(ctx, r.Stream, r.Group, ids...).Err(); err != nil {
		return fmt.Errorf("xack stream=%s group=%s: %w", r.Stream, r.Group, err)
	}
	return nil
}
