package processor

import (
	"context"
	"testing"
)

// TestHeuristicProvider_NeverFails is the whole point of this provider
// (2026-08-05, founder: "we dont need the llm in the critical path... figure
// out a way around it"): AnalyzeText must never return an error, for any
// input, so a filing can never be silently dropped the way the LLM-backed
// providers just dropped every filing during a real billing lapse.
func TestHeuristicProvider_NeverFails(t *testing.T) {
	p := NewHeuristicProvider()
	inputs := []string{
		"source_type=sec_8k\nform=8-K\n\nsome real filing text",
		"source_type=press_release\nform=\n\nsome press release text",
		"",
		"no prefix at all, just raw text",
	}
	for _, in := range inputs {
		sig, err := p.AnalyzeText(context.Background(), in)
		if err != nil {
			t.Fatalf("AnalyzeText(%q) returned an error, must never: %v", in, err)
		}
		if sig == nil {
			t.Fatalf("AnalyzeText(%q) returned a nil signal", in)
		}
		if sig.SignalType == "" {
			t.Errorf("AnalyzeText(%q) returned an empty SignalType", in)
		}
	}
}

func TestSignalTypeForForm_KnownForms(t *testing.T) {
	cases := []struct{ form, wantType string }{
		{"8-K", "MaterialEvent"},
		{"8-K/A", "MaterialEvent"},
		{"10-Q", "PeriodicReport"},
		{"10-K", "PeriodicReport"},
		{"DEF 14A", "ProxyStatement"},
		{"4", "InsiderTransaction"},
		{"NT 10-K", "LateFilingNotice"},
		{"SC 13D", "OwnershipChange"},
		{"totally-unknown-form", "Other"},
	}
	for _, c := range cases {
		got, importance := signalTypeForForm(c.form)
		if got != c.wantType {
			t.Errorf("signalTypeForForm(%q) type = %q, want %q", c.form, got, c.wantType)
		}
		if importance < 1 || importance > 10 {
			t.Errorf("signalTypeForForm(%q) importance = %d, want 1-10", c.form, importance)
		}
	}
}

func TestHeuristicProvider_MarksItself(t *testing.T) {
	p := NewHeuristicProvider()
	sig, err := p.AnalyzeText(context.Background(), "source_type=sec_8k\nform=8-K\n\ntext")
	if err != nil {
		t.Fatalf("AnalyzeText: %v", err)
	}
	if sig.RawMetadata["provider"] != "heuristic" {
		t.Errorf("RawMetadata[provider] = %q, want \"heuristic\" -- downstream/UI needs to be able to tell heuristic signals apart from real LLM analysis", sig.RawMetadata["provider"])
	}
	if sig.Sentiment != 0 {
		t.Errorf("Sentiment = %v, want 0 -- this provider must not fabricate a sentiment score it never measured", sig.Sentiment)
	}
}
