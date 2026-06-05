package entitygraph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GroundTruth records whether a signal's prediction was validated by a real event.
type GroundTruth string

const (
	// GTConfirmed: the predicted event occurred within the signal's ValidThrough window.
	GTConfirmed GroundTruth = "confirmed"
	// GTRefuted: the prediction window expired with no matching event observed.
	GTRefuted GroundTruth = "refuted"
	// GTPending: the signal is still within its prediction window; outcome unknown.
	GTPending GroundTruth = "pending"
)

// AccuracyRecord tracks ground truth for a single signal prediction.
// Persisted in var/entity-graph/accuracy.ndjson as append-only NDJSON.
type AccuracyRecord struct {
	SignalID      string      `json:"signal_id"`
	Ticker        string      `json:"ticker"`
	SignalType    SignalType  `json:"signal_type"`
	PredictedAt   string      `json:"predicted_at"`
	ValidThrough  string      `json:"valid_through"`
	Outcome       GroundTruth `json:"outcome"`
	EvidenceDate  string      `json:"evidence_date,omitempty"`
	EvidenceType  string      `json:"evidence_type,omitempty"` // "activist_13d", etc.
	Notes         string      `json:"notes,omitempty"`
	RecordedAt    string      `json:"recorded_at"`
}

// AccuracyReport summarises prediction performance for one signal type.
type AccuracyReport struct {
	SignalType       SignalType `json:"signal_type"`
	TotalPredictions int       `json:"total_predictions"`
	Confirmed        int       `json:"confirmed"`
	Refuted          int       `json:"refuted"`
	Pending          int       `json:"pending"`
	// Precision = confirmed / (confirmed + refuted); 0 when no resolved predictions.
	Precision float64 `json:"precision"`
}

// Schd13Filing represents a Schedule 13D or 13G filing discovered on EDGAR.
// Persisted in var/schd13/filings.ndjson.
type Schd13Filing struct {
	Ticker     string `json:"ticker"`
	TargetCIK  string `json:"target_cik"`
	FilingDate string `json:"filing_date"` // YYYY-MM-DD
	FilingType string `json:"filing_type"` // "SC 13D", "SC 13G", "SC 13D/A"
	FilerName  string `json:"filer_name"`
	Accession  string `json:"accession"`
}

// CorrelateActivistRisk checks whether activist_risk signals preceded 13D/13G filings.
// For each activist_risk signal it searches filings for the same ticker:
//   - If a 13D/13D-A is found with FilingDate in [PredictedAt, ValidThrough], outcome = confirmed.
//   - If the ValidThrough window has passed with no matching filing, outcome = refuted.
//   - Otherwise outcome = pending.
//
// Returns one AccuracyRecord per activist_risk signal in the input.
func CorrelateActivistRisk(signals []Signal, filings []Schd13Filing) []AccuracyRecord {
	// Index 13D/13D-A filing dates by ticker for fast lookup.
	filingDatesByTicker := map[string][]string{}
	for _, f := range filings {
		if f.FilingType == "SC 13D" || f.FilingType == "SC 13D/A" {
			filingDatesByTicker[f.Ticker] = append(filingDatesByTicker[f.Ticker], f.FilingDate)
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalActivistRisk {
			continue
		}

		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, fd := range filingDatesByTicker[s.Ticker] {
			if fd >= s.DetectedAt && fd <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = fd
				notes = fmt.Sprintf("SC 13D filed %s — within activist_risk prediction window [%s, %s]", fd, s.DetectedAt, s.ValidThrough)
				break
			}
		}

		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("Prediction window [%s, %s] expired; no SC 13D filed for %s", s.DetectedAt, s.ValidThrough, s.Ticker)
		}

		records = append(records, AccuracyRecord{
			SignalID:     s.SignalID,
			Ticker:       s.Ticker,
			SignalType:   s.Type,
			PredictedAt:  s.DetectedAt,
			ValidThrough: s.ValidThrough,
			Outcome:      outcome,
			EvidenceDate: evidenceDate,
			EvidenceType: "activist_13d",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// BuildAccuracyReports aggregates AccuracyRecords into per-signal-type summaries.
func BuildAccuracyReports(records []AccuracyRecord) []AccuracyReport {
	byType := map[SignalType]*AccuracyReport{}
	for _, r := range records {
		rpt, ok := byType[r.SignalType]
		if !ok {
			rpt = &AccuracyReport{SignalType: r.SignalType}
			byType[r.SignalType] = rpt
		}
		rpt.TotalPredictions++
		switch r.Outcome {
		case GTConfirmed:
			rpt.Confirmed++
		case GTRefuted:
			rpt.Refuted++
		case GTPending:
			rpt.Pending++
		}
	}
	var out []AccuracyReport
	for _, rpt := range byType {
		if rpt.Confirmed+rpt.Refuted > 0 {
			rpt.Precision = float64(rpt.Confirmed) / float64(rpt.Confirmed+rpt.Refuted)
		}
		out = append(out, *rpt)
	}
	return out
}

// LoadSchd13Filings reads all Schd13Filing records from <dir>/filings.ndjson.
func LoadSchd13Filings(dir string) ([]Schd13Filing, error) {
	path := filepath.Join(dir, "filings.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var filings []Schd13Filing
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec Schd13Filing
		if json.Unmarshal(sc.Bytes(), &rec) == nil {
			filings = append(filings, rec)
		}
	}
	return filings, sc.Err()
}

// WriteSchd13Filings appends filing records to <dir>/filings.ndjson.
func WriteSchd13Filings(dir string, filings []Schd13Filing) error {
	if len(filings) == 0 {
		return nil
	}
	return appendNDJSON(filepath.Join(dir, "filings.ndjson"), func(enc *json.Encoder) error {
		for i := range filings {
			if err := enc.Encode(&filings[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// CorrelateDecayDeparture checks whether director_decay signals were validated by a
// subsequent leadership departure at the same company. For each director_decay signal
// it searches for a matching leadership_departure or cfo_departure signal at the same
// ticker with a filing date inside [PredictedAt, ValidThrough].
//
// Entity matching is name-substring based to handle canonical vs display name variance
// (e.g. "Frank Herringer" vs "frank-c-herringer"). Returns one AccuracyRecord per
// director_decay signal in the input.
func CorrelateDecayDeparture(signals []Signal) []AccuracyRecord {
	type departure struct {
		entity string
		date   string
	}
	departuresByTicker := map[string][]departure{}
	for _, s := range signals {
		if s.Type == SignalLeadershipDeparture || s.Type == SignalCFODeparture {
			departuresByTicker[s.Ticker] = append(departuresByTicker[s.Ticker], departure{
				entity: strings.ToLower(s.Entity),
				date:   s.FilingDate,
			})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalDirectorDecay {
			continue
		}

		outcome := GTPending
		evidenceDate := ""
		notes := ""

		entityLower := strings.ToLower(s.Entity)
		for _, dep := range departuresByTicker[s.Ticker] {
			if entityLower == "" || dep.entity == "" {
				continue
			}
			entityMatch := entityLower == dep.entity ||
				strings.Contains(dep.entity, entityLower) ||
				strings.Contains(entityLower, dep.entity)
			if !entityMatch {
				continue
			}
			if dep.date >= s.DetectedAt && dep.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = dep.date
				notes = fmt.Sprintf("leadership_departure for %s at %s on %s — within director_decay window [%s, %s]",
					s.Entity, s.Ticker, dep.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}

		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("director_decay window [%s, %s] expired for %s at %s; no matching departure",
				s.DetectedAt, s.ValidThrough, s.Entity, s.Ticker)
		}

		records = append(records, AccuracyRecord{
			SignalID:     s.SignalID,
			Ticker:       s.Ticker,
			SignalType:   s.Type,
			PredictedAt:  s.DetectedAt,
			ValidThrough: s.ValidThrough,
			Outcome:      outcome,
			EvidenceDate: evidenceDate,
			EvidenceType: "leadership_departure",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateAuditorChangeFilingRisk checks whether auditor_change signals were
// followed by a late_filing or eps_filing_revision signal at the same ticker
// within the auditor_change signal's ValidThrough window. An auditor change
// followed by a late filing or EPS revision confirms the restatement-risk
// thesis that Jon uses when auditor_change and late_filing co-occur.
//
// Returns one AccuracyRecord per auditor_change signal in the input.
func CorrelateAuditorChangeFilingRisk(signals []Signal) []AccuracyRecord {
	type filingEvent struct{ date string }
	riskByTicker := map[string][]filingEvent{}
	for _, s := range signals {
		if s.Type == SignalLateFiling || s.Type == SignalEPSFilingRevision {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			riskByTicker[s.Ticker] = append(riskByTicker[s.Ticker], filingEvent{date: d})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalAuditorChange {
			continue
		}

		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range riskByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("filing risk event at %s on %s — within auditor_change window [%s, %s]",
					s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}

		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("auditor_change window [%s, %s] expired for %s; no late_filing or eps_revision observed",
				s.DetectedAt, s.ValidThrough, s.Ticker)
		}

		records = append(records, AccuracyRecord{
			SignalID:     s.SignalID,
			Ticker:       s.Ticker,
			SignalType:   s.Type,
			PredictedAt:  s.DetectedAt,
			ValidThrough: s.ValidThrough,
			Outcome:      outcome,
			EvidenceDate: evidenceDate,
			EvidenceType: "late_filing_or_eps_revision",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateInsiderBuyCapitalReturn checks whether insider_buy signals were followed
// by a buyback_authorization or dividend_raise signal at the same ticker within the
// insider_buy signal's ValidThrough window. An insider purchase followed by capital
// return confirms the buy was directionally correct (management signaling confidence
// in cash flow). Returns one AccuracyRecord per insider_buy signal in the input.
func CorrelateInsiderBuyCapitalReturn(signals []Signal) []AccuracyRecord {
	type event struct{ date string }
	returnByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalBuybackAuthorization || s.Type == SignalDividendRaise {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			returnByTicker[s.Ticker] = append(returnByTicker[s.Ticker], event{date: d})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalInsiderBuy {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range returnByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("capital return event at %s on %s — within insider_buy window [%s, %s]",
					s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("insider_buy window [%s, %s] expired for %s; no buyback or dividend_raise observed",
				s.DetectedAt, s.ValidThrough, s.Ticker)
		}
		records = append(records, AccuracyRecord{
			SignalID:     s.SignalID,
			Ticker:       s.Ticker,
			SignalType:   s.Type,
			PredictedAt:  s.DetectedAt,
			ValidThrough: s.ValidThrough,
			Outcome:      outcome,
			EvidenceDate: evidenceDate,
			EvidenceType: "buyback_or_dividend_raise",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateInsiderSellDistress checks whether insider_sell_cluster signals were
// followed by a dividend_cut, cfo_departure, or late_filing signal at the same
// ticker within the insider_sell_cluster signal's ValidThrough window. An insider
// sell cluster followed by a distress signal confirms the cluster was a leading
// indicator. Returns one AccuracyRecord per insider_sell_cluster signal.
func CorrelateInsiderSellDistress(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	distressByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDividendCut || s.Type == SignalCFODeparture || s.Type == SignalLateFiling {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			distressByTicker[s.Ticker] = append(distressByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalInsiderSellCluster {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range distressByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("distress event (%s) at %s on %s — within insider_sell_cluster window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("insider_sell_cluster window [%s, %s] expired for %s; no distress signal observed",
				s.DetectedAt, s.ValidThrough, s.Ticker)
		}
		records = append(records, AccuracyRecord{
			SignalID:     s.SignalID,
			Ticker:       s.Ticker,
			SignalType:   s.Type,
			PredictedAt:  s.DetectedAt,
			ValidThrough: s.ValidThrough,
			Outcome:      outcome,
			EvidenceDate: evidenceDate,
			EvidenceType: "dividend_cut_cfo_departure_or_late_filing",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// LoadAccuracyRecords reads all AccuracyRecord entries from <dir>/accuracy.ndjson.
func LoadAccuracyRecords(dir string) ([]AccuracyRecord, error) {
	path := filepath.Join(dir, "accuracy.ndjson")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	var records []AccuracyRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r AccuracyRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			records = append(records, r)
		}
	}
	return records, sc.Err()
}

// WriteAccuracyRecords appends accuracy records to <dir>/accuracy.ndjson.
func WriteAccuracyRecords(dir string, records []AccuracyRecord) error {
	if len(records) == 0 {
		return nil
	}
	return appendNDJSON(filepath.Join(dir, "accuracy.ndjson"), func(enc *json.Encoder) error {
		for i := range records {
			if err := enc.Encode(&records[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// ── Governance health history ─────────────────────────────────────────────────
// The health history tracks the most recent governance_health_index score per
// ticker so ScoreGovernanceHealthTrend can compare the new score to the previous
// one and emit deterioration/improvement signals.

// HealthSnapshot is one entry in the health history file.
type HealthSnapshot struct {
	Ticker    string  `json:"ticker"`
	Score     float64 `json:"score"`
	RecordedAt string `json:"recorded_at"` // YYYY-MM-DD
}

// LoadHealthHistory reads the latest snapshot per ticker from
// <dir>/health_history.ndjson. Returns an empty map when the file doesn't exist.
func LoadHealthHistory(dir string) (map[string]HealthSnapshot, error) {
	p := filepath.Join(dir, "health_history.ndjson")
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return map[string]HealthSnapshot{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open health_history: %w", err)
	}
	defer f.Close()

	// Keep only the most recent entry per ticker (last-write-wins).
	latest := map[string]HealthSnapshot{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var s HealthSnapshot
		if json.Unmarshal(sc.Bytes(), &s) == nil && s.Ticker != "" {
			latest[s.Ticker] = s
		}
	}
	return latest, sc.Err()
}

// AppendHealthSnapshot appends new health snapshots to <dir>/health_history.ndjson.
// Callers pass the current scores after a processing batch. The file is
// append-only; LoadHealthHistory handles last-write-wins deduplication.
func AppendHealthSnapshot(dir string, snapshots []HealthSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	return appendNDJSON(filepath.Join(dir, "health_history.ndjson"), func(enc *json.Encoder) error {
		for i := range snapshots {
			if err := enc.Encode(&snapshots[i]); err != nil {
				return err
			}
		}
		return nil
	})
}
