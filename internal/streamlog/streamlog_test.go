package streamlog

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitWritesNDJSON(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Info("test", "hello world")
	line := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(line, "{") {
		t.Fatalf("expected JSON, got: %q", line)
	}
	var e Event
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatalf("parse event: %v", err)
	}
	if e.Level != LevelInfo {
		t.Errorf("expected INFO, got %q", e.Level)
	}
	if e.Source != "test" {
		t.Errorf("expected source=test, got %q", e.Source)
	}
	if e.Msg != "hello world" {
		t.Errorf("expected msg, got %q", e.Msg)
	}
}

func TestEmitWithData(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Emit(LevelFetch, "spider", "fetching", map[string]any{"url": "http://example.com", "status": 200})
	var e Event
	json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &e)
	if e.Data["url"] != "http://example.com" {
		t.Errorf("expected url in data, got %v", e.Data)
	}
}

func TestErrorIncludesErrString(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Error("spider", "connection refused", &testErr{"dial tcp: refused"})
	var e Event
	json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &e)
	if e.Level != LevelError {
		t.Errorf("expected ERROR level")
	}
	if e.Data["error"] != "dial tcp: refused" {
		t.Errorf("expected error string in data, got %v", e.Data)
	}
}

func TestDiscardDoesNotPanic(t *testing.T) {
	l := Discard()
	l.Info("x", "discarded")
	l.Done("x", "done", map[string]any{"n": 5})
}

func TestConcurrentEmit(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			l.Info("concurrent", "msg", map[string]any{"i": i})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }
