// Package streamlog emits structured NDJSON log events to an io.Writer.
// Every spider, crawler, and research operation streams events here so the
// caller can tail -f them, pipe them to emily watch, or store them as
// FatBaby observations with full provenance.
//
// Event format (one JSON object per line):
//
//	{"at":"2026-06-26T07:30:00Z","level":"FETCH","source":"reddit","msg":"fetching r/investing","data":{"url":"..."}}
package streamlog

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// Level constants for structured event classification.
const (
	LevelInfo      = "INFO"
	LevelFetch     = "FETCH"
	LevelExtract   = "EXTRACT"
	LevelSynthesize = "SYNTHESIZE"
	LevelWarn      = "WARN"
	LevelError     = "ERROR"
	LevelDone      = "DONE"
)

// Event is one structured log event.
type Event struct {
	At     time.Time      `json:"at"`
	Level  string         `json:"level"`
	Source string         `json:"source"`
	Msg    string         `json:"msg"`
	Data   map[string]any `json:"data,omitempty"`
}

// Logger streams events as NDJSON to a writer. Safe for concurrent use.
type Logger struct {
	w  io.Writer
	mu sync.Mutex
}

// New returns a Logger writing to w. Pass os.Stdout for live tailing.
func New(w io.Writer) *Logger {
	return &Logger{w: w}
}

// Stdout returns a Logger writing to os.Stdout.
func Stdout() *Logger { return New(os.Stdout) }

// Discard returns a Logger that drops all events (for tests).
func Discard() *Logger { return New(io.Discard) }

// Emit writes one structured event to the underlying writer.
func (l *Logger) Emit(level, source, msg string, data map[string]any) {
	e := Event{
		At:     time.Now().UTC(),
		Level:  level,
		Source: source,
		Msg:    msg,
		Data:   data,
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.w.Write(b)
	l.w.Write([]byte("\n"))
	l.mu.Unlock()
}

func (l *Logger) Info(source, msg string, data ...map[string]any) {
	var d map[string]any
	if len(data) > 0 {
		d = data[0]
	}
	l.Emit(LevelInfo, source, msg, d)
}

func (l *Logger) Fetch(source, url string, statusCode int, byteCount int64) {
	l.Emit(LevelFetch, source, "fetched "+url, map[string]any{
		"url":    url,
		"status": statusCode,
		"bytes":  byteCount,
	})
}

func (l *Logger) Extract(source, url string, entityCount int) {
	l.Emit(LevelExtract, source, "extracted from "+url, map[string]any{
		"url":          url,
		"entity_count": entityCount,
	})
}

func (l *Logger) Warn(source, msg string, data ...map[string]any) {
	var d map[string]any
	if len(data) > 0 {
		d = data[0]
	}
	l.Emit(LevelWarn, source, msg, d)
}

func (l *Logger) Error(source, msg string, err error) {
	l.Emit(LevelError, source, msg, map[string]any{"error": err.Error()})
}

func (l *Logger) Done(source, msg string, summary map[string]any) {
	l.Emit(LevelDone, source, msg, summary)
}
