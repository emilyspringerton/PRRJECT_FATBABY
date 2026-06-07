package entitygraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Observation is the payload written to var/emily-observations/latest.json
// after each entity-graph processing run. It is picked up by observation-watcher
// and fed to Claude Code for recursive self-improvement.
type Observation struct {
	Timestamp string `json:"timestamp"`
	// Source identifies the emitting subsystem; observation-watcher uses this
	// to select the appropriate refinement prompt template.
	Source             string           `json:"source"` // always "entity-graph"
	Status             string           `json:"status"` // ok | needs_attention | error
	Subject            string           `json:"subject"`
	FilingsProcessed   int              `json:"filings_processed"`
	DirectorsFound     int              `json:"directors_found"`
	ProposalsProcessed int              `json:"proposals_processed"`
	SignalsGenerated   int              `json:"signals_generated"`
	SignalsByType      map[string]int   `json:"signals_by_type"`
	Gaps               []string         `json:"gaps"`
	ParseErrors        []ParseError     `json:"parse_errors,omitempty"`
	HighSeverity       []SignalSummary  `json:"high_severity_signals,omitempty"`
	// AccuracyScores holds retrospective accuracy reports for signal types
	// whose predictions can be validated against real-world events (e.g.
	// activist_risk vs observed 13D filings). Populated when accuracy records
	// exist in var/entity-graph/accuracy.ndjson.
	AccuracyScores     []AccuracyReport `json:"accuracy_scores,omitempty"`
	RequestForClaude   string           `json:"request_for_claude,omitempty"`
}

// ParseError records a filing that could not be fully parsed.
type ParseError struct {
	Ticker          string `json:"ticker"`
	Identity        string `json:"identity"`
	Error           string `json:"error"`
}

// SignalSummary is a compact view of a signal for the observation payload.
type SignalSummary struct {
	SignalID  string   `json:"signal_id"`
	Type      string   `json:"type"`
	Ticker    string   `json:"ticker"`
	Entity    string   `json:"entity,omitempty"`
	Severity  Severity `json:"severity"`
	Score     float64  `json:"score"`
}

// BuildObservation assembles an observation from a processing run.
// proposalsProcessed is the total number of non-director proposals parsed across
// all filings in the batch; it is included in the observation so Claude can
// distinguish "proposals not found in text" from "proposals found but no signal fired".
// accuracyReports (optional) carries retrospective accuracy summaries from
// CorrelateActivistRisk / BuildAccuracyReports; pass nil when no records exist.
func BuildObservation(
	processed int,
	signals []Signal,
	parseErrors []ParseError,
	nodeCount int,
	proposalsProcessed int,
	accuracyReports []AccuracyReport,
) Observation {
	// Zero-fill all known signal types so the observation always shows complete coverage,
	// making it easy to distinguish "this signal was evaluated and didn't fire" from
	// "this signal type doesn't exist yet".
	byType := make(map[string]int, len(AllSignalTypes))
	for _, t := range AllSignalTypes {
		byType[string(t)] = 0
	}
	var highSev []SignalSummary
	for _, s := range signals {
		byType[string(s.Type)]++
		if s.Severity == SeverityHigh || s.Severity == SeverityCritical {
			highSev = append(highSev, SignalSummary{
				SignalID: s.SignalID,
				Type:     string(s.Type),
				Ticker:   s.Ticker,
				Entity:   s.Entity,
				Severity: s.Severity,
				Score:    s.Score,
			})
		}
	}

	gaps := detectGaps(signals, nodeCount, byType, proposalsProcessed, accuracyReports)

	status := "ok"
	if len(parseErrors) > 0 || len(gaps) > 0 {
		status = "needs_attention"
	}

	obs := Observation{
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		Source:             "entity-graph",
		Status:             status,
		Subject:            fmt.Sprintf("Entity graph run: %d filings, %d directors, %d proposals, %d signals", processed, nodeCount, proposalsProcessed, len(signals)),
		FilingsProcessed:   processed,
		DirectorsFound:     nodeCount,
		ProposalsProcessed: proposalsProcessed,
		SignalsGenerated:   len(signals),
		SignalsByType:      byType,
		Gaps:               gaps,
		ParseErrors:        parseErrors,
		HighSeverity:       highSev,
		AccuracyScores:     accuracyReports,
	}

	if len(parseErrors) > 0 || len(gaps) > 0 {
		obs.RequestForClaude = buildRefinementRequest(len(parseErrors), len(gaps), nodeCount, len(signals), len(highSev), proposalsProcessed, gaps, accuracyReports)
	}

	return obs
}

func buildRefinementRequest(parseErrors, gapCount, nodeCount, signalCount, highSevCount, proposalsProcessed int, gaps []string, accuracyReports []AccuracyReport) string {
	base := fmt.Sprintf(
		"Entity-graph run completed with %d parse errors and %d gaps identified. "+
			"Directors: %d, Proposals parsed: %d, Signals: %d (%d high/critical). ",
		parseErrors, gapCount, nodeCount, proposalsProcessed, signalCount, highSevCount,
	)
	if proposalsProcessed == 0 && nodeCount > 0 {
		base += "IMPORTANT: 0 proposals were parsed despite directors being found — the proposal-splitter regex may not be matching the filing text. " +
			"Inspect internal/entitygraph/parser.go extractProposals() and consider whether the 'Proposal N' pattern matches the live filing format. "
	}
	for _, g := range gaps {
		base += "Gap: " + g + ". "
	}
	// RSI: include low-precision signal types so Claude can recommend recalibration.
	var lowPrecision []string
	for _, r := range accuracyReports {
		resolved := r.Confirmed + r.Refuted
		if resolved >= 5 && r.Precision < 0.40 {
			lowPrecision = append(lowPrecision, fmt.Sprintf("%s (precision=%.0f%%, n=%d resolved)", r.SignalType, r.Precision*100, resolved))
		}
	}
	if len(lowPrecision) > 0 {
		sort.Strings(lowPrecision)
		base += fmt.Sprintf("RSI RECALIBRATION NEEDED: %d signal type(s) have <40%% precision with sufficient resolved outcomes: %v. "+
			"Consider raising thresholds, shortening ValidThrough windows, or removing weak signals from the governance health model. "+
			"Penalty weights have been automatically halved (AccuracyAdjustedPenalties) but threshold-level changes require updating config/entity-graph-rules.json. ",
			len(lowPrecision), lowPrecision)
	}
	base += "Refine thresholds in config/entity-graph-rules.json or parser patterns in internal/entitygraph/parser.go as appropriate."
	return base
}

// detectGaps inspects parsing completeness and returns gap descriptions for actual
// pipeline failures. Signal absences (no friction, no entrenchment, no family control)
// are not gaps — they are expected outcomes for well-governed companies.
// accuracyReports (optional) drives RSI gap reporting: signal types with poor empirical
// precision are flagged so the observation-watcher/Claude can recommend recalibration.
func detectGaps(signals []Signal, nodeCount int, byType map[string]int, proposalsProcessed int, accuracyReports []AccuracyReport) []string {
	var gaps []string
	if nodeCount == 0 {
		gaps = append(gaps, "No directors extracted — Item 5.07 parsing may have failed for this filing")
	}
	if nodeCount > 0 && proposalsProcessed == 0 {
		gaps = append(gaps, "0 proposals parsed despite directors found — proposal-splitter regex likely did not match filing text format; inspect extractProposals() in parser.go")
	}
	// RSI: flag signal types with low empirical precision.
	for _, r := range accuracyReports {
		resolved := r.Confirmed + r.Refuted
		if resolved >= 5 && r.Precision < 0.40 {
			gaps = append(gaps, fmt.Sprintf("low-precision signal '%s': %.0f%% confirmed across %d resolved predictions — consider threshold recalibration", r.SignalType, r.Precision*100, resolved))
		}
	}
	return gaps
}

// PublishObservation writes obs to <dir>/latest.json and an archive sibling.
func PublishObservation(obs Observation, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(obs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	latest := filepath.Join(dir, "latest.json")
	if err := os.WriteFile(latest, data, 0o644); err != nil {
		return fmt.Errorf("write latest.json: %w", err)
	}
	ts := time.Now().UTC().Format("2006-01-02T15-04-05")
	archive := filepath.Join(dir, ts+".json")
	if err := os.WriteFile(archive, data, 0o644); err != nil {
		return fmt.Errorf("write archive %s: %w", archive, err)
	}
	return nil
}
