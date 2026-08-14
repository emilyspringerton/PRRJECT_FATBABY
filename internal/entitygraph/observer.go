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
// directorsThisBatch is the count of director votes extracted from this batch only
// (not total graph nodes); it drives gap detection for the "0 proposals" condition.
// accuracyReports (optional) carries retrospective accuracy summaries from
// CorrelateActivistRisk / BuildAccuracyReports; pass nil when no records exist.
func BuildObservation(
	processed int,
	signals []Signal,
	parseErrors []ParseError,
	nodeCount int,
	proposalsProcessed int,
	directorsThisBatch int,
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

	gaps := detectGaps(signals, nodeCount, byType, processed, proposalsProcessed, directorsThisBatch, accuracyReports)

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
// directorsThisBatch is the number of director votes found in THIS batch only (not
// accumulated graph total); gap conditions use this so that batches of non-proxy 8-Ks
// (earnings, officer appointments) do not spuriously trigger the "0 proposals" gap.
// accuracyReports (optional) drives RSI gap reporting: signal types with poor empirical
// precision are flagged so the observation-watcher/Claude can recommend recalibration.
func detectGaps(signals []Signal, nodeCount int, byType map[string]int, processed int, proposalsProcessed int, directorsThisBatch int, accuracyReports []AccuracyReport) []string {
	var gaps []string
	// "No directors" gap fires only when we actually processed proxy filings (Item 5.07
	// present) but extracted zero director votes — a genuine parser failure. Using
	// processed>0 instead of nodeCount==0 avoids false positives when all 8-Ks in
	// the batch are non-proxy (earnings, 5.02-only, M&A) and the historical graph
	// already has nodes from previous runs.
	if processed > 0 && directorsThisBatch == 0 {
		gaps = append(gaps, "No directors extracted from proxy filings — Item 5.07 vote table parsing may have failed; inspect extractDirectorVotes() in parser.go")
	}
	// "0 proposals" gap fires only when this batch found directors (proxy filings with
	// vote data) but no non-director proposals. Using directorsThisBatch ensures the
	// gap only fires when the current batch actually contained proxy filings, not
	// whenever the accumulated graph has historical nodes.
	if directorsThisBatch > 0 && proposalsProcessed == 0 {
		gaps = append(gaps, "0 proposals parsed despite directors found — proposal-splitter regex likely did not match filing text format; inspect extractProposals() in parser.go")
	} else if directorsThisBatch > 0 && processed >= 2 && float64(proposalsProcessed)/float64(processed) < 0.5 {
		gaps = append(gaps, fmt.Sprintf(
			"Low proposal yield: %d of %d proxy filings yielded non-director proposals (%.0f%% rate) — possible unrecognized proposal header format; inspect extractProposals() in parser.go",
			proposalsProcessed, processed, float64(proposalsProcessed)/float64(processed)*100,
		))
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
	return publishObservationAt(obs, dir, time.Now().UTC().Format("2006-01-02T15-04-05"))
}

// publishObservationAt is PublishObservation with the archive base
// timestamp passed in explicitly, so the collision-avoidance loop below is
// directly testable without depending on real wall-clock timing.
//
// Real bug found and fixed 2026-08-14: this used to write straight to
// <ts>.json with no collision check, unlike emily observe's own writer
// (internal/obs/writer.go, which added this exact guard 2026-08-09 after
// losing observations the same way). Two observations landing in the same
// second -- entity-graph's own periodic run and a `emily observe` CLI call,
// or two entity-graph runs close together -- silently clobbered each other;
// confirmed live, a real founder directive lost this way mid-session.
// Second-precision timestamps collide often enough under real load that
// this isn't a hypothetical.
func publishObservationAt(obs Observation, dir string, ts string) error {
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
	archiveName := ts + ".json"
	archive := filepath.Join(dir, archiveName)
	for i := 2; ; i++ {
		if _, err := os.Stat(archive); os.IsNotExist(err) {
			break
		}
		archiveName = fmt.Sprintf("%s-%d.json", ts, i)
		archive = filepath.Join(dir, archiveName)
	}
	if err := os.WriteFile(archive, data, 0o644); err != nil {
		return fmt.Errorf("write archive %s: %w", archive, err)
	}
	return nil
}
