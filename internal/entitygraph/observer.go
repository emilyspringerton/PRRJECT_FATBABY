package entitygraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Observation is the payload written to var/emily-observations/latest.json
// after each entity-graph processing run. It is picked up by observation-watcher
// and fed to Claude Code for recursive self-improvement.
type Observation struct {
	Timestamp        string          `json:"timestamp"`
	// Source identifies the emitting subsystem; observation-watcher uses this
	// to select the appropriate refinement prompt template.
	Source           string          `json:"source"` // always "entity-graph"
	Status           string          `json:"status"` // ok | needs_attention | error
	Subject          string          `json:"subject"`
	FilingsProcessed int             `json:"filings_processed"`
	DirectorsFound   int             `json:"directors_found"`
	SignalsGenerated int             `json:"signals_generated"`
	SignalsByType    map[string]int  `json:"signals_by_type"`
	Gaps             []string        `json:"gaps"`
	ParseErrors      []ParseError    `json:"parse_errors,omitempty"`
	HighSeverity     []SignalSummary `json:"high_severity_signals,omitempty"`
	RequestForClaude string          `json:"request_for_claude,omitempty"`
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
func BuildObservation(
	processed int,
	signals []Signal,
	parseErrors []ParseError,
	nodeCount int,
) Observation {
	byType := map[string]int{}
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

	status := "ok"
	if len(parseErrors) > 0 {
		status = "needs_attention"
	}

	gaps := detectGaps(signals, nodeCount, byType)

	obs := Observation{
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		Source:           "entity-graph",
		Status:           status,
		Subject:          fmt.Sprintf("Entity graph run: %d filings, %d directors, %d signals", processed, nodeCount, len(signals)),
		FilingsProcessed: processed,
		DirectorsFound:   nodeCount,
		SignalsGenerated: len(signals),
		SignalsByType:    byType,
		Gaps:             gaps,
		ParseErrors:      parseErrors,
		HighSeverity:     highSev,
	}

	if len(parseErrors) > 0 || len(gaps) > 0 {
		obs.RequestForClaude = fmt.Sprintf(
			"Entity-graph run completed with %d parse errors and %d gaps identified. "+
				"Given %d directors and %d signals (%d high/critical severity), what parsing rules or signal thresholds should be refined?",
			len(parseErrors), len(gaps), nodeCount, len(signals), len(highSev),
		)
	}

	return obs
}

// detectGaps inspects signal coverage and returns human-readable gap descriptions.
func detectGaps(signals []Signal, nodeCount int, byType map[string]int) []string {
	var gaps []string
	if nodeCount == 0 {
		gaps = append(gaps, "No directors extracted — Item 5.07 parsing may have failed")
	}
	if byType[string(SignalDirectorFriction)] == 0 && nodeCount > 3 {
		gaps = append(gaps, "No friction signals on a board with 4+ directors — friction threshold may be too low")
	}
	if byType[string(SignalGovernanceEntrenchment)] == 0 {
		gaps = append(gaps, "No entrenchment signals detected — proxy vote sections may not be parsed yet")
	}
	if byType[string(SignalFamilyControl)] == 0 && nodeCount > 0 {
		gaps = append(gaps, "No family control signals — family_name_keywords in rules may need expansion")
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
