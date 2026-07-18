package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStore_Scan_MatchesReadFrom(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		e := Event{ID: idFor(i), Type: "filing_discovered", Data: json.RawMessage(`{"x":1}`)}
		if _, err := s.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	want, err := s.ReadFrom(ctx, 4, 100)
	if err != nil {
		t.Fatal(err)
	}

	var got []Record
	if err := s.Scan(ctx, 4, func(r Record) error {
		got = append(got, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(got) != len(want) {
		t.Fatalf("Scan returned %d records, ReadFrom returned %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Sequence != want[i].Sequence || got[i].Event.ID != want[i].Event.ID {
			t.Fatalf("record %d mismatch: Scan=%+v ReadFrom=%+v", i, got[i], want[i])
		}
	}
}

func TestFileStore_Scan_ResumesFromCurrentFileWithoutReprocessing(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		e := Event{ID: idFor(i), Type: "filing_discovered", Data: json.RawMessage(`{"x":1}`)}
		if _, err := s.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	var firstPass []Record
	if err := s.Scan(ctx, 1, func(r Record) error {
		firstPass = append(firstPass, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(firstPass) != 5 {
		t.Fatalf("first pass got %d records, want 5", len(firstPass))
	}

	// Append two more records to the same (still-open) current journal.
	for i := 5; i < 7; i++ {
		e := Event{ID: idFor(i), Type: "filing_discovered", Data: json.RawMessage(`{"x":1}`)}
		if _, err := s.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	// A tail-style Scan starting right after the last-seen sequence must see
	// only the two new records — proving the resume path doesn't re-hand back
	// records already delivered, and doesn't require a full re-read to find them.
	var secondPass []Record
	if err := s.Scan(ctx, firstPass[len(firstPass)-1].Sequence+1, func(r Record) error {
		secondPass = append(secondPass, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(secondPass) != 2 {
		t.Fatalf("second pass got %d records, want 2: %+v", len(secondPass), secondPass)
	}
	if secondPass[0].Event.ID != idFor(5) || secondPass[1].Event.ID != idFor(6) {
		t.Fatalf("unexpected records in second pass: %+v", secondPass)
	}
}

func TestFileStore_Scan_NoNewEventsDoesNoWork(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.Append(ctx, Event{ID: "1", Type: "filing_discovered", Data: json.RawMessage(`{"x":1}`)}); err != nil {
		t.Fatal(err)
	}

	// Warm the resume cursor for the current file.
	if err := s.Scan(ctx, 1, func(Record) error { return nil }); err != nil {
		t.Fatal(err)
	}

	calls := 0
	if err := s.Scan(ctx, 2, func(Record) error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("expected no records/no re-delivery on an unchanged file, got %d calls", calls)
	}
}

func TestFileStore_Scan_TruncatedTrailingLineTolerated(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(),
		Event{ID: "1", Type: "a", Data: json.RawMessage(`{"a":1}`)},
		Event{ID: "2", Type: "b", Data: json.RawMessage(`{"b":2}`)},
	); err != nil {
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

	var got []Record
	if err := s.Scan(context.Background(), 1, func(r Record) error {
		got = append(got, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 good records got %d", len(got))
	}
}

func TestFileStore_Scan_FnErrorStopsScan(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		e := Event{ID: idFor(i), Type: "filing_discovered", Data: json.RawMessage(`{"x":1}`)}
		if _, err := s.Append(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	wantErr := errors.New("stop here")
	seen := 0
	err = s.Scan(ctx, 1, func(r Record) error {
		seen++
		if r.Sequence == 3 {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	if seen != 3 {
		t.Fatalf("expected scan to stop after 3rd record, saw %d", seen)
	}
}

func idFor(i int) string {
	return fmt.Sprintf("evt-%d", i)
}
