package processor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

// HeuristicProvider extracts a real (if less nuanced than an LLM's) Signal
// straight from the filing/press-release's own known metadata (source_type,
// form) -- no external API call, so it can never fail, stall, or silently
// drop a filing the way the LLM-backed providers just did.
//
// Founder, live, 2026-08-05, diagnosing why newssite content stopped
// updating: "we dont need the llm in the critical path of the data...
// figure out a way around it for now i dont want to be relying on
// something we never do and the system assumes works" -> "like i never
// really asked for the llm sentiment anyway codex or you just kinda put it
// in randomly... we dont want llm generated data like that in the critical
// path for now."
//
// The real, confirmed failure mode this replaces as the default: worker.go's
// handleOne persists the real source_document_persisted event BEFORE
// calling Provider.AnalyzeText, but if AnalyzeText fails (as it does for
// every single filing while the Anthropic account is out of credit --
// confirmed live), the function returns early: no signal_generated event
// is ever appended, and the filing is never retried (the poll loop's cursor
// already advanced past it by then). A single billing lapse silently drops
// every filing seen while it lasts, permanently -- exactly what "the system
// assumes works" describes. This provider never returns an error, so that
// failure mode is structurally impossible now, independent of whether any
// LLM key is configured, valid, or funded.
//
// Not a permanent replacement for real analysis -- HaikuProvider/
// ArchetypeProvider still exist, opt-in via -enable-llm (cmd/processor),
// for whenever the founder wants richer summaries back in the loop. This is
// the honest, always-available floor underneath that: cmd/main.go's own
// default now, not a last-resort stub nobody reaches.
type HeuristicProvider struct{}

func NewHeuristicProvider() *HeuristicProvider { return &HeuristicProvider{} }

// signalTypeForForm mirrors sourceTypeForForm's own SEC-form switch (worker.go)
// but names the SIGNAL's own category, not the source document's storage kind --
// deliberately a second switch, not a shared one, since the two enums are
// allowed to diverge (a future non-SEC source_type wouldn't have a "form" at
// all) even though today's form set happens to line up.
func signalTypeForForm(form string) (signalType string, importance int) {
	switch strings.ToUpper(strings.TrimSpace(form)) {
	case "8-K", "8-K/A":
		return "MaterialEvent", 7
	case "10-Q", "10-Q/A":
		return "PeriodicReport", 4
	case "10-K", "10-K/A":
		return "PeriodicReport", 5
	case "DEF 14A", "DEFA14A":
		return "ProxyStatement", 5
	case "4":
		return "InsiderTransaction", 6
	case "NT 10-K", "NT 10-Q":
		return "LateFilingNotice", 6
	case "SC 13D", "SC 13D/A":
		return "OwnershipChange", 7
	case "SC 13G", "SC 13G/A":
		return "OwnershipChange", 5
	default:
		return "Other", 3
	}
}

func (p *HeuristicProvider) AnalyzeText(ctx context.Context, text string) (*intelligence.Signal, error) {
	_ = ctx
	sourceType, form := parseSourceMeta(text)

	var signalType string
	var importance int
	if form != "" {
		signalType, importance = signalTypeForForm(form)
	} else if sourceType == "press_release" {
		signalType, importance = "PressRelease", 4
	} else {
		signalType, importance = "Other", 3
	}

	label := form
	if label == "" {
		label = sourceType
	}
	if label == "" {
		label = "document"
	}

	return &intelligence.Signal{
		SignalType: signalType,
		Importance: importance,
		// 0.0, not a guess dressed up as measurement -- real sentiment needs
		// real text analysis this provider deliberately doesn't attempt.
		Sentiment:      0,
		Summary:        fmt.Sprintf("%s filed (%s).", label, time.Now().UTC().Format("2006-01-02")),
		ImpactAnalysis: fmt.Sprintf("Classified by form/source type only (%s) -- no narrative analysis performed.", label),
		RawMetadata:    map[string]string{"provider": "heuristic"},
	}, nil
}
