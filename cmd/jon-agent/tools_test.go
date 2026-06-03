package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpectedMove(t *testing.T) {
	d := NewToolDispatcher()
	registerMarketDataStubs(d)

	fn := d.handlers["jon_expected_move"]
	if fn == nil {
		t.Fatal("jon_expected_move not registered")
	}

	out, err := fn(map[string]any{
		"price": 100.0,
		"iv":    0.30,
		"dte":   30.0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	em, _ := res["expected_move"].(float64)
	// price * iv * sqrt(dte/365) = 100 * 0.30 * sqrt(30/365) ≈ 8.60
	expected := 100.0 * 0.30 * math.Sqrt(30.0/365.0)
	if math.Abs(em-expected) > 0.05 {
		t.Errorf("expected_move=%v want ~%v", em, expected)
	}
	upper, _ := res["upper_target"].(float64)
	lower, _ := res["lower_target"].(float64)
	if math.Abs(upper-(100+em)) > 0.1 {
		t.Errorf("upper_target=%v want ~%v", upper, 100+em)
	}
	if math.Abs(lower-(100-em)) > 0.1 {
		t.Errorf("lower_target=%v want ~%v", lower, 100-em)
	}
}

func TestExpectedMove_MultipleStdev(t *testing.T) {
	d := NewToolDispatcher()
	registerMarketDataStubs(d)
	fn := d.handlers["jon_expected_move"]

	out1, _ := fn(map[string]any{"price": 100.0, "iv": 0.25, "dte": 30.0, "stdev": 1.0})
	out2, _ := fn(map[string]any{"price": 100.0, "iv": 0.25, "dte": 30.0, "stdev": 2.0})

	var r1, r2 map[string]any
	json.Unmarshal([]byte(out1), &r1)
	json.Unmarshal([]byte(out2), &r2)

	em1, _ := r1["expected_move"].(float64)
	em2, _ := r2["expected_move"].(float64)
	if math.Abs(em2-2*em1) > 0.05 {
		t.Errorf("2-stdev move should be ~2x 1-stdev: got em1=%v em2=%v", em1, em2)
	}
}

func TestExpectedMove_BadInput(t *testing.T) {
	d := NewToolDispatcher()
	registerMarketDataStubs(d)
	fn := d.handlers["jon_expected_move"]

	out, err := fn(map[string]any{"price": 0.0, "iv": 0.30, "dte": 30.0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected error response for zero price, got: %s", out)
	}
}

func TestMarketDataStubs_NotConnected(t *testing.T) {
	d := NewToolDispatcher()
	registerMarketDataStubs(d)

	// Ensure MARKET_DATA_API_KEY is not set for this test.
	os.Unsetenv("MARKET_DATA_API_KEY")

	for _, name := range []string{"jon_options_chain", "jon_price_data", "jon_iv_rank"} {
		fn := d.handlers[name]
		if fn == nil {
			t.Errorf("%s not registered", name)
			continue
		}
		out, err := fn(map[string]any{"ticker": "AAPL"})
		if err != nil {
			t.Errorf("%s returned error: %v", name, err)
			continue
		}
		if !strings.Contains(out, "MARKET_DATA_API_KEY") {
			t.Errorf("%s response should mention MARKET_DATA_API_KEY, got: %s", name, out)
		}
	}
}

func TestLogAndReadSetups(t *testing.T) {
	tmpDir := t.TempDir()
	d := NewToolDispatcher()
	registerSetupTools(d, tmpDir)

	logFn := d.handlers["jon_log_setup"]
	readFn := d.handlers["jon_read_setups"]

	// Log a setup.
	out, err := logFn(map[string]any{
		"ticker":     "SCHW",
		"direction":  "long",
		"thesis":     "board decay concern + activist risk divergence vs flat price action",
		"conviction": "High",
	})
	if err != nil {
		t.Fatalf("log_setup error: %v", err)
	}
	if !strings.Contains(out, "logged") {
		t.Errorf("expected logged response, got: %s", out)
	}

	// Read it back.
	out2, err := readFn(map[string]any{})
	if err != nil {
		t.Fatalf("read_setups error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out2), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	setups, _ := res["setups"].([]any)
	if len(setups) != 1 {
		t.Fatalf("expected 1 setup, got %d", len(setups))
	}
	s := setups[0].(map[string]any)
	if s["ticker"] != "SCHW" {
		t.Errorf("ticker mismatch: %v", s["ticker"])
	}
	if s["conviction"] != "High" {
		t.Errorf("conviction mismatch: %v", s["conviction"])
	}
}

func TestReadSetups_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	d := NewToolDispatcher()
	registerSetupTools(d, tmpDir)

	out, err := d.handlers["jon_read_setups"](map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "setups") {
		t.Errorf("expected setups key in response: %s", out)
	}
}

func TestReadSetups_FilterByTicker(t *testing.T) {
	tmpDir := t.TempDir()
	d := NewToolDispatcher()
	registerSetupTools(d, tmpDir)
	logFn := d.handlers["jon_log_setup"]
	readFn := d.handlers["jon_read_setups"]

	logFn(map[string]any{"ticker": "AAPL", "direction": "long", "thesis": "t1", "conviction": "Medium"})
	logFn(map[string]any{"ticker": "SCHW", "direction": "short", "thesis": "t2", "conviction": "High"})

	out, _ := readFn(map[string]any{"ticker": "AAPL"})
	var res map[string]any
	json.Unmarshal([]byte(out), &res)
	setups, _ := res["setups"].([]any)
	if len(setups) != 1 {
		t.Fatalf("expected 1 AAPL setup, got %d", len(setups))
	}
}

func TestPublishToPrime(t *testing.T) {
	tmpDir := t.TempDir()
	// Set up a fake prime integration dir inside tmpDir.
	primeDir := filepath.Join(tmpDir, "signals", "observations")
	os.Setenv("EMILY_INTEGRATION_DIR", filepath.Join(tmpDir, "signals"))
	defer os.Unsetenv("EMILY_INTEGRATION_DIR")

	d := NewToolDispatcher()
	registerSetupTools(d, tmpDir)

	out, err := d.handlers["jon_publish_to_prime"](map[string]any{
		"ticker":       "SCHW",
		"direction":    "long",
		"conviction":   "High",
		"thesis":       "Board decay concern not priced in",
		"signal_stack": "board_decay_concern + activist_risk",
	})
	if err != nil {
		t.Fatalf("publish_to_prime error: %v", err)
	}
	if !strings.Contains(out, "published_to_prime") {
		t.Errorf("expected published_to_prime in response: %s", out)
	}

	// Verify the file was created.
	entries, err := os.ReadDir(primeDir)
	if err != nil {
		t.Fatalf("read prime dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in prime dir, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(primeDir, entries[0].Name()))
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal prime payload: %v", err)
	}
	if payload["source"] != "jon-stockwell" {
		t.Errorf("source mismatch: %v", payload["source"])
	}
	if payload["ticker"] != "SCHW" {
		t.Errorf("ticker mismatch: %v", payload["ticker"])
	}
}
