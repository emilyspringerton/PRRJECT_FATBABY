package eventstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStore_AppendReadAndRecover(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	e := Event{ID: "evt-1", Type: "filing_discovered", OccurredAt: time.Now().UTC(), Data: json.RawMessage(`{"x":1}`)}
	recs, err := s.Append(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if recs[0].Sequence != 1 {
		t.Fatalf("expected seq 1 got %d", recs[0].Sequence)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	latest, err := s.LatestSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest != 1 {
		t.Fatalf("expected latest 1 got %d", latest)
	}

	got, err := s.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Event.ID != "evt-1" {
		t.Fatalf("unexpected read results %#v", got)
	}
}

func TestFileStore_InvalidAndTruncatedRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Append(context.Background())
	if err != ErrEmptyAppend {
		t.Fatalf("expected ErrEmptyAppend got %v", err)
	}
	_, err = s.Append(context.Background(), Event{Type: "x", Data: json.RawMessage(`{"a":1}`)})
	if err != ErrInvalidEventID {
		t.Fatalf("expected ErrInvalidEventID got %v", err)
	}

	_, err = s.Append(context.Background(),
		Event{ID: "1", Type: "a", Data: json.RawMessage(`{"a":1}`)},
		Event{ID: "2", Type: "b", Data: json.RawMessage(`{"b":2}`)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "events", "*.ndjson"))
	if err != nil || len(files) != 1 {
		t.Fatalf("glob files err=%v count=%d", err, len(files))
	}
	f, err := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"sequence":3,"event":{"id":"evt-3"`)
	_ = f.Close()

	s, err = NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 good records got %d", len(got))
	}
}

// TestFileStore_ReadFrom_SeesNewAppendsFromSeparateWriterProcess reproduces
// the real cross-process staleness bug found 2026-08-20 (founder real-time:
// "check all of the FATBABY data for freshness" / "the homepage of news site
// is totally useless"): cmd/prwatch-body opens its own FileStore handle
// read-only against the SAME directory cmd/prwatch's own separate process
// writes into. That reader handle's s.current is always nil (it never
// Appends), which used to make ReadFrom's "is this the active journal, never
// cache its max sequence" check fall through to "" for every file --
// including today's, still growing under the writer's process -- so the
// reader's fileMaxSeq skip-cache silently froze after the first read and
// stopped seeing anything the writer appended afterward, all day, until the
// next UTC date rollover created a fresh uncached file. This test opens two
// separate FileStore handles against one directory (mirroring the real
// discoveryStore/bodyStore split) to catch any regression of that fix.
func TestFileStore_ReadFrom_SeesNewAppendsFromSeparateWriterProcess(t *testing.T) {
	dir := t.TempDir()

	writer, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	reader, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if _, err := writer.Append(context.Background(), Event{ID: "evt-1", Type: "t", OccurredAt: time.Now().UTC(), Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}

	got, err := reader.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Event.ID != "evt-1" {
		t.Fatalf("first read: expected 1 record (evt-1), got %#v", got)
	}

	// The real bug: a second write to the still-growing today's file, from
	// the writer's separate process/handle, must still be visible to the
	// reader's next ReadFrom call -- not silently swallowed by a stale
	// closed-file cache entry from the first read above.
	if _, err := writer.Append(context.Background(), Event{ID: "evt-2", Type: "t", OccurredAt: time.Now().UTC(), Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}

	got, err = reader.ReadFrom(context.Background(), 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Event.ID != "evt-2" {
		t.Fatalf("second read: expected 1 new record (evt-2), got %#v -- this is the real cross-process staleness bug if it fails", got)
	}
}
