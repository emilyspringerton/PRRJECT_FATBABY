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
