package processor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func TestIntentForSource(t *testing.T) {
	cases := []struct {
		src, form string
		wantWords []string // intent must contain all
	}{
		{"edgar", "8-K", []string{"fraud", "signal"}},
		{"edgar", "10-K", []string{"strategic", "foresight"}},
		{"edgar", "SC 13D", []string{"wealth", "governance"}},
		{"edgar", "DEF 14A", []string{"governance", "truth"}},
		{"edgar", "S-1", []string{"intelligence", "signal"}},
		{"pr_newswire", "", []string{"reputation", "foresight"}},
		{"business_wire", "", []string{"reputation", "foresight"}},
		{"unknown", "", []string{"intelligence", "signal"}},
	}
	for _, tc := range cases {
		intent := intentForSource(tc.src, tc.form)
		for _, w := range tc.wantWords {
			if !strings.Contains(intent, w) {
				t.Errorf("intentForSource(%q,%q) missing word %q in %q", tc.src, tc.form, w, intent)
			}
		}
	}
}

func TestParseSourceMeta(t *testing.T) {
	text := "source_type=edgar\nform=8-K\n\nSome filing content here."
	src, form := parseSourceMeta(text)
	if src != "edgar" {
		t.Errorf("got source_type=%q, want edgar", src)
	}
	if form != "8-K" {
		t.Errorf("got form=%q, want 8-K", form)
	}
}

func TestParseSourceMetaMissing(t *testing.T) {
	src, form := parseSourceMeta("raw text with no metadata prefix")
	if src != "" || form != "" {
		t.Errorf("expected empty source/form for plain text, got %q %q", src, form)
	}
}

func TestParseSignalFromText(t *testing.T) {
	raw := `Here is the analysis:
{"id":"test","ticker":"AAPL","signal_type":"Earnings","importance":8,"sentiment":0.5,"summary":"Good earnings","impact_analysis":"Strong beat"}`
	sig, err := parseSignalFromText(raw)
	if err != nil {
		t.Fatalf("parseSignalFromText: %v", err)
	}
	if sig.Ticker != "AAPL" {
		t.Errorf("ticker=%q, want AAPL", sig.Ticker)
	}
	if sig.Importance != 8 {
		t.Errorf("importance=%d, want 8", sig.Importance)
	}
}

func TestParseSignalFromTextNoJSON(t *testing.T) {
	_, err := parseSignalFromText("This is not a JSON signal at all.")
	if err == nil {
		t.Fatal("expected error for non-JSON output")
	}
}

func TestArchetypeProviderFallback(t *testing.T) {
	// When engine is unreachable, fallback must be called.
	called := false
	fallback := &stubProvider{fn: func(ctx context.Context, text string) (*intelligence.Signal, error) {
		called = true
		return &intelligence.Signal{Ticker: "FB_FALLBACK"}, nil
	}}

	ap := NewArchetypeProvider("http://localhost:1", nil, fallback)
	sig, err := ap.AnalyzeText(context.Background(), "source_type=edgar\nform=8-K\n\nsome text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("fallback was not called")
	}
	if sig.Ticker != "FB_FALLBACK" {
		t.Errorf("got ticker %q, want FB_FALLBACK", sig.Ticker)
	}
}

func TestArchetypeProviderSuccess(t *testing.T) {
	// Stub archetype-engine server returns a valid InvokeResponse.
	sigJSON := `{"id":"t1","ticker":"NVDA","signal_type":"Earnings","importance":9,"sentiment":0.8,"summary":"Beat","impact_analysis":"Big beat"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoke" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"output":          sigJSON,
			"e1_output":       sigJSON,
			"e2_output":       "divergent view",
			"resonance_state": "Golden Band",
			"phase_delta_deg": 28.5,
			"e1_weight":       0.6,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	ap := NewArchetypeProvider(srv.URL, nil, nil)
	sig, err := ap.AnalyzeText(context.Background(), "source_type=edgar\nform=10-K\n\nannual filing content")
	if err != nil {
		t.Fatalf("AnalyzeText: %v", err)
	}
	if sig.Ticker != "NVDA" {
		t.Errorf("ticker=%q, want NVDA", sig.Ticker)
	}
	if sig.RawMetadata["resonance_state"] != "Golden Band" {
		t.Errorf("resonance_state=%q, want Golden Band", sig.RawMetadata["resonance_state"])
	}
	if sig.RawMetadata["provider"] != "archetype" {
		t.Errorf("provider=%q, want archetype", sig.RawMetadata["provider"])
	}
}

// stubProvider is a test double for Provider.
type stubProvider struct {
	fn func(ctx context.Context, text string) (*intelligence.Signal, error)
}

func (s *stubProvider) AnalyzeText(ctx context.Context, text string) (*intelligence.Signal, error) {
	return s.fn(ctx, text)
}
