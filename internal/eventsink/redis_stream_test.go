package eventsink

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/redis/go-redis/v9"
)

// newTestRedis -- a real, in-process, miniredis-backed *redis.Client (not a mock of the client
// interface -- a real Redis protocol implementation in-memory), same real "verify against the
// real wire behavior, not a hand-rolled stub" discipline this monorepo already applies elsewhere
// (e.g. SHANKPIT's own GOLDENBAND reader/writer round trips). No live Redis/Memorystore needed
// to prove RedisStreamSink/RedisStreamConsumer's own real, correct behavior against this stream's
// actual real semantics (XADD/XREADGROUP/XACK/PEL), only an actual live cluster/Memorystore
// instance to prove production reachability, a real, separate, not-yet-verified step (this
// sandbox has neither `redis-server` nor live GCP credentials -- see docs/northstar/
// KUBERNETES_MIGRATION.md's own honest status note).
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestRedisStreamSinkWriteAndConsumerReadNew(t *testing.T) {
	client := newTestRedis(t)
	ctx := context.Background()

	sink := RedisStreamSink{Client: client, Stream: "fatbaby:events:v1"}
	evt := eventstore.Event{ID: "evt-1", Type: "filing_discovered", Source: "secwatch", Data: json.RawMessage(`{"ticker":"AAPL"}`)}
	if err := sink.Write(ctx, evt); err != nil {
		t.Fatalf("Write: %v", err)
	}

	consumer := RedisStreamConsumer{Client: client, Stream: "fatbaby:events:v1", Group: "processor", Consumer: "processor-1"}
	if err := consumer.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}
	// Calling EnsureGroup a second time must be a real, harmless no-op (BUSYGROUP), matching
	// this type's own documented "safe to call on every process startup" contract.
	if err := consumer.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup (second call) should be idempotent: %v", err)
	}

	entries, err := consumer.ReadNew(ctx, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("ReadNew: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Event.ID != "evt-1" || entries[0].Event.Type != "filing_discovered" {
		t.Fatalf("decoded event mismatch: %+v", entries[0].Event)
	}

	// Real, direct proof of the PEL-based delivery guarantee this type's own header comment
	// makes: before Ack, a SECOND consumer in the SAME group reading with ">" must NOT also
	// receive this already-delivered entry (Redis delivers each entry to exactly one consumer
	// per group) -- the real property the zero-downtime cutover plan depends on.
	secondConsumer := RedisStreamConsumer{Client: client, Stream: "fatbaby:events:v1", Group: "processor", Consumer: "processor-2"}
	moreEntries, err := secondConsumer.ReadNew(ctx, 10, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("second consumer ReadNew: %v", err)
	}
	if len(moreEntries) != 0 {
		t.Fatalf("a second consumer in the same group should NOT receive an already-delivered entry, got %d", len(moreEntries))
	}

	if err := consumer.Ack(ctx, entries[0].ID); err != nil {
		t.Fatalf("Ack: %v", err)
	}
}

func TestRedisStreamSinkAndConsumer_TwoIndependentGroupsBothSeeEveryEvent(t *testing.T) {
	// The real "fanout to other stores like mysql" property: two DIFFERENT consumer groups
	// (e.g. "processor" and "mysql-sink") reading the SAME stream must each independently see
	// every event, since each group tracks its own read position -- neither one's progress
	// affects the other's.
	client := newTestRedis(t)
	ctx := context.Background()

	sink := RedisStreamSink{Client: client, Stream: "fatbaby:events:v1"}
	for i := 0; i < 3; i++ {
		evt := eventstore.Event{ID: "evt", Type: "t", Source: "s", Data: json.RawMessage(`{}`)}
		if err := sink.Write(ctx, evt); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	processorGroup := RedisStreamConsumer{Client: client, Stream: "fatbaby:events:v1", Group: "processor", Consumer: "p1"}
	mysqlGroup := RedisStreamConsumer{Client: client, Stream: "fatbaby:events:v1", Group: "mysql-sink", Consumer: "m1"}
	if err := processorGroup.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup processor: %v", err)
	}
	if err := mysqlGroup.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup mysql-sink: %v", err)
	}

	pEntries, err := processorGroup.ReadNew(ctx, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("processor ReadNew: %v", err)
	}
	if len(pEntries) != 3 {
		t.Fatalf("processor group: got %d entries, want 3", len(pEntries))
	}

	mEntries, err := mysqlGroup.ReadNew(ctx, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("mysql-sink ReadNew: %v", err)
	}
	if len(mEntries) != 3 {
		t.Fatalf("mysql-sink group: got %d entries, want 3 (independent of the processor group's own progress)", len(mEntries))
	}
}

func TestRedisStreamConsumer_NewConsumerJoiningSameGroupSharesRemainingWork(t *testing.T) {
	// The real cutover-safety property: a NEW consumer (e.g. an incoming k8s pod) joining an
	// EXISTING group picks up entries the OLD consumer (e.g. the outgoing systemd process)
	// hasn't yet claimed -- no separate "handoff" API needed, just being a second real consumer
	// in the same group.
	client := newTestRedis(t)
	ctx := context.Background()

	sink := RedisStreamSink{Client: client, Stream: "fatbaby:events:v1"}
	for i := 0; i < 4; i++ {
		evt := eventstore.Event{ID: "evt", Type: "t", Source: "s", Data: json.RawMessage(`{}`)}
		if err := sink.Write(ctx, evt); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	oldConsumer := RedisStreamConsumer{Client: client, Stream: "fatbaby:events:v1", Group: "processor", Consumer: "systemd-old"}
	if err := oldConsumer.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}
	firstBatch, err := oldConsumer.ReadNew(ctx, 2, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("old consumer ReadNew: %v", err)
	}
	if len(firstBatch) != 2 {
		t.Fatalf("old consumer: got %d, want 2", len(firstBatch))
	}
	for _, e := range firstBatch {
		if err := oldConsumer.Ack(ctx, e.ID); err != nil {
			t.Fatalf("old consumer Ack: %v", err)
		}
	}

	// The "new pod" joins the SAME group -- no EnsureGroup call needed (BUSYGROUP would just be
	// a no-op if it did), proving a second consumer can start reading immediately.
	newConsumer := RedisStreamConsumer{Client: client, Stream: "fatbaby:events:v1", Group: "processor", Consumer: "pod-new"}
	secondBatch, err := newConsumer.ReadNew(ctx, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("new consumer ReadNew: %v", err)
	}
	if len(secondBatch) != 2 {
		t.Fatalf("new consumer: got %d entries, want the remaining 2 (zero missed, zero duplicated)", len(secondBatch))
	}
}
