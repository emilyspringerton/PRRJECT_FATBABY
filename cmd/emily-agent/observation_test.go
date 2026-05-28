package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestDispatcher(t *testing.T) (*ToolDispatcher, string) {
	t.Helper()
	root := t.TempDir()
	d := NewToolDispatcher()
	registerFatbabyTools(d, root)
	return d, root
}

func TestWriteObservationCreatesLatestAndArchive(t *testing.T) {
	d, root := newTestDispatcher(t)
	fn := d.handlers["fatbaby_write_observation"]
	if fn == nil {
		t.Fatal("fatbaby_write_observation not registered")
	}
	out, err := fn(map[string]any{
		"summary":       "processor stalled",
		"severity":      "warn",
		"findings":      "no signal_generated events for 30m on ticker AAPL",
		"suggested_fix": "restart processor and check pkg/intelligence",
	})
	if err != nil {
		t.Fatalf("write returned error: %v", err)
	}
	if !strings.Contains(out, "severity=warn") {
		t.Errorf("expected severity in result, got %q", out)
	}

	latest := filepath.Join(root, "var", "emily-observations", "latest.json")
	b, err := os.ReadFile(latest)
	if err != nil {
		t.Fatalf("latest.json missing: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("latest.json not valid JSON: %v", err)
	}
	for _, k := range []string{"timestamp", "summary", "severity", "findings", "suggested_fix"} {
		if _, ok := got[k]; !ok {
			t.Errorf("latest.json missing key %q", k)
		}
	}
	if got["severity"] != "warn" {
		t.Errorf("severity = %v, want warn", got["severity"])
	}

	entries, err := os.ReadDir(filepath.Join(root, "var", "emily-observations"))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	archives := 0
	for _, e := range entries {
		if e.Name() != "latest.json" && strings.HasSuffix(e.Name(), ".json") {
			archives++
		}
	}
	if archives != 1 {
		t.Errorf("expected 1 archive file, got %d", archives)
	}
}

func TestWriteObservationDefaultsSeverity(t *testing.T) {
	d, root := newTestDispatcher(t)
	fn := d.handlers["fatbaby_write_observation"]
	if _, err := fn(map[string]any{
		"summary":  "low signal volume",
		"findings": "fewer than 5 signals/hour across all tickers",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "var", "emily-observations", "latest.json"))
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if got["severity"] != "info" {
		t.Errorf("default severity = %v, want info", got["severity"])
	}
}

func TestWriteObservationRequiresSummaryAndFindings(t *testing.T) {
	d, _ := newTestDispatcher(t)
	fn := d.handlers["fatbaby_write_observation"]
	cases := []map[string]any{
		{"summary": "", "findings": "x"},
		{"summary": "x", "findings": ""},
		{"summary": "   ", "findings": "x"},
		{"summary": "x", "findings": "   "},
	}
	for i, args := range cases {
		if _, err := fn(args); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestReadObservationMissingFile(t *testing.T) {
	d, _ := newTestDispatcher(t)
	fn := d.handlers["fatbaby_read_observation"]
	out, err := fn(map[string]any{})
	if err != nil {
		t.Fatalf("read returned error: %v", err)
	}
	if out != "no observations yet" {
		t.Errorf("got %q, want %q", out, "no observations yet")
	}
}

func TestReadObservationRoundTrip(t *testing.T) {
	d, _ := newTestDispatcher(t)
	if _, err := d.handlers["fatbaby_write_observation"](map[string]any{
		"summary":  "ticker dropped",
		"findings": "MSFT not present in watchlist",
		"severity": "error",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := d.handlers["fatbaby_read_observation"](map[string]any{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(out, "MSFT") || !strings.Contains(out, "ticker dropped") {
		t.Errorf("read output missing expected fields: %q", out)
	}
}
