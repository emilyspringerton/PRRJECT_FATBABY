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

// CorrelateCFODepartureDistress checks whether cfo_departure signals were followed
// by a dividend_cut, late_filing, or eps_filing_revision at the same ticker within
// the cfo_departure signal's ValidThrough window. A CFO departure followed by a
// financial distress signal confirms it was a leading indicator. Returns one
// AccuracyRecord per cfo_departure signal.
func CorrelateCFODepartureDistress(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	distressByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDividendCut || s.Type == SignalLateFiling || s.Type == SignalEPSFilingRevision {
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
		if s.Type != SignalCFODeparture {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range distressByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("distress event (%s) at %s on %s — within cfo_departure window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("cfo_departure window [%s, %s] expired for %s; no distress signal observed",
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
			EvidenceType: "dividend_cut_late_filing_or_eps_revision",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateDirectorFrictionEscalation checks whether director_friction signals were
// followed by a compensation_concern, abstention_spike, abstention_outlier, or
// nomination_rejection at the same ticker within the director_friction signal's
// ValidThrough window. Director friction followed by a vote-quality or compensation
// signal confirms the board tension escalated into a measurable proxy event.
// Returns one AccuracyRecord per director_friction signal.
func CorrelateDirectorFrictionEscalation(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	escalationByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalCompensationConcern || s.Type == SignalAbstentionSpike ||
			s.Type == SignalAbstentionOutlier || s.Type == SignalNominationRejection {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			escalationByTicker[s.Ticker] = append(escalationByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalDirectorFriction {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range escalationByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("governance escalation (%s) at %s on %s — within director_friction window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("director_friction window [%s, %s] expired for %s; no escalation signal observed",
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
			EvidenceType: "compensation_concern_abstention_or_nomination_rejection",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateDividendCutDeterioration checks whether dividend_cut signals were followed
// by a cfo_departure, late_filing, or eps_filing_revision at the same ticker within
// the dividend_cut signal's ValidThrough window. A dividend cut followed by further
// financial distress confirms it was the leading edge of a deterioration cascade.
// Returns one AccuracyRecord per dividend_cut signal.
func CorrelateDividendCutDeterioration(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	deteriorationByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalCFODeparture || s.Type == SignalLateFiling || s.Type == SignalEPSFilingRevision {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			deteriorationByTicker[s.Ticker] = append(deteriorationByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalDividendCut {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range deteriorationByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("deterioration cascade (%s) at %s on %s — within dividend_cut window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("dividend_cut window [%s, %s] expired for %s; no further deterioration observed",
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
			EvidenceType: "cfo_departure_late_filing_or_eps_revision",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateLateFilingDistress checks whether late_filing signals were followed by a
// cfo_departure, dividend_cut, or eps_filing_revision at the same ticker within the
// late_filing signal's ValidThrough window. A late filing followed by an additional
// distress signal confirms the filing delay was symptomatic of deeper trouble.
// Returns one AccuracyRecord per late_filing signal.
func CorrelateLateFilingDistress(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	distressByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalCFODeparture || s.Type == SignalDividendCut || s.Type == SignalEPSFilingRevision {
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
		if s.Type != SignalLateFiling {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range distressByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("distress signal (%s) at %s on %s — within late_filing window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("late_filing window [%s, %s] expired for %s; no further distress signal observed",
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
			EvidenceType: "cfo_departure_dividend_cut_or_eps_revision",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateLeadershipDepartureDistress checks whether leadership_departure signals
// were followed by a dividend_cut, late_filing, or cfo_departure at the same ticker
// within the leadership_departure signal's ValidThrough window. A senior leadership
// exit followed by a financial distress signal confirms the departure was a leading
// indicator of deterioration. Returns one AccuracyRecord per leadership_departure signal.
func CorrelateLeadershipDepartureDistress(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	distressByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDividendCut || s.Type == SignalLateFiling || s.Type == SignalCFODeparture {
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
		if s.Type != SignalLeadershipDeparture {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range distressByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("distress signal (%s) at %s on %s — within leadership_departure window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("leadership_departure window [%s, %s] expired for %s; no distress signal observed",
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
			EvidenceType: "dividend_cut_late_filing_or_cfo_departure",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateBuybackSuspensionDistress checks whether buyback_suspension signals were
// followed by a dividend_cut, late_filing, or cfo_departure at the same ticker within
// the buyback_suspension signal's ValidThrough window. A buyback suspension is a
// management signal that capital is constrained; subsequent distress confirms it was
// a leading indicator. Returns one AccuracyRecord per buyback_suspension signal.
func CorrelateBuybackSuspensionDistress(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	distressByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDividendCut || s.Type == SignalLateFiling || s.Type == SignalCFODeparture {
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
		if s.Type != SignalBuybackSuspension {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range distressByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("distress signal (%s) at %s on %s — within buyback_suspension window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("buyback_suspension window [%s, %s] expired for %s; no distress signal observed",
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
			EvidenceType: "dividend_cut_late_filing_or_cfo_departure",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateAbstentionSpikeEscalation checks whether abstention_spike signals were
// followed by a nomination_rejection or director_friction at the same ticker within
// the abstention_spike signal's ValidThrough window. An abstention spike is an early
// proxy-season signal; subsequent governance action confirms it was a leading indicator.
// Returns one AccuracyRecord per abstention_spike signal.
func CorrelateAbstentionSpikeEscalation(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	escalationByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalNominationRejection || s.Type == SignalDirectorFriction {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			escalationByTicker[s.Ticker] = append(escalationByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalAbstentionSpike {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range escalationByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("governance escalation (%s) at %s on %s — within abstention_spike window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("abstention_spike window [%s, %s] expired for %s; no governance escalation observed",
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
			EvidenceType: "nomination_rejection_or_director_friction",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateBoardDecayConcernDeterioration checks whether board_decay_concern signals
// were followed by a director_friction, cfo_departure, or late_filing at the same
// ticker within the board_decay_concern signal's ValidThrough window. Board decay is
// a structural governance warning; subsequent personnel or filing distress confirms
// the concern was predictive. Returns one AccuracyRecord per board_decay_concern signal.
func CorrelateBoardDecayConcernDeterioration(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	deteriorationByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDirectorFriction || s.Type == SignalCFODeparture || s.Type == SignalLateFiling {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			deteriorationByTicker[s.Ticker] = append(deteriorationByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalBoardDecayConcern {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range deteriorationByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("deterioration signal (%s) at %s on %s — within board_decay_concern window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("board_decay_concern window [%s, %s] expired for %s; no deterioration signal observed",
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
			EvidenceType: "director_friction_cfo_departure_or_late_filing",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateDividendRaiseCapitalCluster checks whether dividend_raise signals were
// followed by a buyback_authorization or insider_buy at the same ticker within
// the dividend_raise signal's ValidThrough window. Management confidence signals
// tend to cluster: a dividend raise followed by a buyback authorization or insider
// purchase confirms the positive capital allocation posture. Returns one AccuracyRecord
// per dividend_raise signal.
func CorrelateDividendRaiseCapitalCluster(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	capitalByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalBuybackAuthorization || s.Type == SignalInsiderBuy {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			capitalByTicker[s.Ticker] = append(capitalByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalDividendRaise {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range capitalByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("capital return signal (%s) at %s on %s — within dividend_raise window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("dividend_raise window [%s, %s] expired for %s; no capital return cluster observed",
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
			EvidenceType: "buyback_authorization_or_insider_buy",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateGovernanceDeterioratingDistress checks whether governance_deteriorating
// signals were followed by a cfo_departure, director_friction, or late_filing at the
// same ticker within the governance_deteriorating signal's ValidThrough window. A
// composite deterioration score followed by a concrete event confirms the scoring
// model was predictive. Returns one AccuracyRecord per governance_deteriorating signal.
func CorrelateGovernanceDeterioratingDistress(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	distressByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalCFODeparture || s.Type == SignalDirectorFriction || s.Type == SignalLateFiling {
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
		if s.Type != SignalGovernanceDeterioration {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range distressByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("distress signal (%s) at %s on %s — within governance_deteriorating window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("governance_deteriorating window [%s, %s] expired for %s; no distress signal observed",
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
			EvidenceType: "cfo_departure_director_friction_or_late_filing",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateGovernanceImprovingCapitalReturn checks whether governance_improving signals
// were followed by a dividend_raise or buyback_authorization at the same ticker within
// the governance_improving signal's ValidThrough window. Governance improvement should
// translate into shareholder-friendly capital allocation; confirmation validates the
// composite scoring model on the positive side. Returns one AccuracyRecord per
// governance_improving signal.
func CorrelateGovernanceImprovingCapitalReturn(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	capitalByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDividendRaise || s.Type == SignalBuybackAuthorization {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			capitalByTicker[s.Ticker] = append(capitalByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalGovernanceImproving {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range capitalByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("capital return signal (%s) at %s on %s — within governance_improving window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("governance_improving window [%s, %s] expired for %s; no capital return signal observed",
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
			EvidenceType: "dividend_raise_or_buyback_authorization",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateGovernanceEntrenchmentVoteQuality checks whether governance_entrenchment
// signals were followed by a compensation_concern or abstention_spike at the same
// ticker within the governance_entrenchment signal's ValidThrough window. Entrenched
// boards tend to face proxy-season pushback on pay; subsequent vote-quality signals
// confirm the entrenchment was legible to shareholders. Returns one AccuracyRecord
// per governance_entrenchment signal.
func CorrelateGovernanceEntrenchmentVoteQuality(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	voteQualityByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalCompensationConcern || s.Type == SignalAbstentionSpike {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			voteQualityByTicker[s.Ticker] = append(voteQualityByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalGovernanceEntrenchment {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range voteQualityByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("vote quality signal (%s) at %s on %s — within governance_entrenchment window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("governance_entrenchment window [%s, %s] expired for %s; no vote quality signal observed",
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
			EvidenceType: "compensation_concern_or_abstention_spike",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateAbstentionOutlierNominationRejection checks whether abstention_outlier
// signals were followed by a nomination_rejection at the same ticker within the
// abstention_outlier signal's ValidThrough window. An outlier abstention pattern
// targeting a specific director is an early signal of organized shareholder opposition;
// a subsequent nomination rejection confirms it materialized. Returns one AccuracyRecord
// per abstention_outlier signal.
func CorrelateAbstentionOutlierNominationRejection(signals []Signal) []AccuracyRecord {
	type event struct{ date string }
	rejectionByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalNominationRejection {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			rejectionByTicker[s.Ticker] = append(rejectionByTicker[s.Ticker], event{date: d})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalAbstentionOutlier {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range rejectionByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("nomination_rejection at %s on %s — within abstention_outlier window [%s, %s]",
					s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("abstention_outlier window [%s, %s] expired for %s; no nomination_rejection observed",
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
			EvidenceType: "nomination_rejection",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelatePostFailureActivistPrediction checks whether post_failure_activist_prediction
// signals were followed by an activist_risk signal at the same ticker within the
// prediction signal's ValidThrough window. A post-failure activist prediction is the
// pipeline's forward model for re-engagement; a subsequent activist_risk signal confirms
// the model was correct. Returns one AccuracyRecord per post_failure_activist_prediction signal.
func CorrelatePostFailureActivistPrediction(signals []Signal) []AccuracyRecord {
	type event struct{ date string }
	activistByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalActivistRisk {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			activistByTicker[s.Ticker] = append(activistByTicker[s.Ticker], event{date: d})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalPostFailureActivistPrediction {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range activistByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("activist_risk confirmed at %s on %s — within post_failure_activist_prediction window [%s, %s]",
					s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("post_failure_activist_prediction window [%s, %s] expired for %s; no activist_risk observed",
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
			EvidenceType: "activist_risk",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateBuybackAuthorizationInsiderBuy checks whether buyback_authorization signals
// were followed by an insider_buy at the same ticker within the buyback_authorization
// signal's ValidThrough window. When management announces a buyback and insiders then
// also purchase personally, the double signal confirms genuine management confidence
// rather than financial engineering. Returns one AccuracyRecord per buyback_authorization signal.
func CorrelateBuybackAuthorizationInsiderBuy(signals []Signal) []AccuracyRecord {
	type event struct{ date string }
	insiderByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalInsiderBuy {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			insiderByTicker[s.Ticker] = append(insiderByTicker[s.Ticker], event{date: d})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalBuybackAuthorization {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range insiderByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("insider_buy at %s on %s — within buyback_authorization window [%s, %s]",
					s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("buyback_authorization window [%s, %s] expired for %s; no insider_buy observed",
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
			EvidenceType: "insider_buy",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateBrokerNonVoteAnomalyDirectorFriction checks whether broker_nonvote_anomaly
// signals were followed by a director_friction signal at the same ticker within the
// broker_nonvote_anomaly signal's ValidThrough window. Anomalous broker non-vote patterns
// — where votes are withheld rather than cast — are an early institutional signal of
// board dissatisfaction; subsequent director friction confirms it. Returns one AccuracyRecord
// per broker_nonvote_anomaly signal.
func CorrelateBrokerNonVoteAnomalyDirectorFriction(signals []Signal) []AccuracyRecord {
	type event struct{ date string }
	frictionByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDirectorFriction {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			frictionByTicker[s.Ticker] = append(frictionByTicker[s.Ticker], event{date: d})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalBrokerNonVoteAnomaly {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range frictionByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("director_friction at %s on %s — within broker_nonvote_anomaly window [%s, %s]",
					s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("broker_nonvote_anomaly window [%s, %s] expired for %s; no director_friction observed",
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
			EvidenceType: "director_friction",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateSpecialDividendCapitalReturn checks whether special_dividend signals were
// followed by a buyback_authorization or insider_buy at the same ticker within the
// special_dividend signal's ValidThrough window. A special dividend signals management
// confidence in excess cash; a subsequent buyback authorization or insider purchase
// confirms the capital-return posture was sustained. Returns one AccuracyRecord per
// special_dividend signal.
func CorrelateSpecialDividendCapitalReturn(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	capitalByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalBuybackAuthorization || s.Type == SignalInsiderBuy {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			capitalByTicker[s.Ticker] = append(capitalByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalSpecialDividend {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range capitalByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("capital return signal (%s) at %s on %s — within special_dividend window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("special_dividend window [%s, %s] expired for %s; no capital return cluster observed",
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
			EvidenceType: "buyback_authorization_or_insider_buy",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateEPSFilingRevisionDistress checks whether eps_filing_revision signals were
// followed by a cfo_departure, dividend_cut, or late_filing at the same ticker within
// the eps_filing_revision signal's ValidThrough window. An EPS revision — a restatement
// or material correction — is an early marker of financial control breakdown; a
// subsequent departure or dividend action confirms the deterioration cascade.
// Returns one AccuracyRecord per eps_filing_revision signal.
func CorrelateEPSFilingRevisionDistress(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	distressByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalCFODeparture || s.Type == SignalDividendCut || s.Type == SignalLateFiling {
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
		if s.Type != SignalEPSFilingRevision {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range distressByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("distress signal (%s) at %s on %s — within eps_filing_revision window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("eps_filing_revision window [%s, %s] expired for %s; no further distress signal observed",
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
			EvidenceType: "cfo_departure_dividend_cut_or_late_filing",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateCompensationConcernEscalation checks whether compensation_concern signals were
// followed by an abstention_spike or nomination_rejection at the same ticker within the
// compensation_concern signal's ValidThrough window. A compensation concern flag —
// typically an ISS or Glass Lewis objection to pay practices — is a leading governance
// signal; shareholder pushback at the next vote confirms that the concern was material.
// Returns one AccuracyRecord per compensation_concern signal.
func CorrelateCompensationConcernEscalation(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	pushbackByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalAbstentionSpike || s.Type == SignalNominationRejection {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			pushbackByTicker[s.Ticker] = append(pushbackByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalCompensationConcern {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range pushbackByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("shareholder pushback (%s) at %s on %s — within compensation_concern window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("compensation_concern window [%s, %s] expired for %s; no shareholder pushback observed",
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
			EvidenceType: "abstention_spike_or_nomination_rejection",
			Notes:        notes,
			RecordedAt:   today,
		})
	}
	return records
}

// CorrelateNominationRejectionFriction checks whether nomination_rejection signals were
// followed by a director_friction or abstention_spike at the same ticker within the
// nomination_rejection signal's ValidThrough window. A failed director nomination is
// often the first visible symptom of deeper board friction; a subsequent friction or
// abstention pattern at the same ticker confirms it was structural rather than isolated.
// Returns one AccuracyRecord per nomination_rejection signal.
func CorrelateNominationRejectionFriction(signals []Signal) []AccuracyRecord {
	type event struct{ date, kind string }
	frictionByTicker := map[string][]event{}
	for _, s := range signals {
		if s.Type == SignalDirectorFriction || s.Type == SignalAbstentionSpike {
			d := s.FilingDate
			if d == "" {
				d = s.DetectedAt
			}
			frictionByTicker[s.Ticker] = append(frictionByTicker[s.Ticker], event{date: d, kind: string(s.Type)})
		}
	}

	today := time.Now().UTC().Format("2006-01-02")
	var records []AccuracyRecord

	for _, s := range signals {
		if s.Type != SignalNominationRejection {
			continue
		}
		outcome := GTPending
		evidenceDate := ""
		notes := ""

		for _, ev := range frictionByTicker[s.Ticker] {
			if ev.date >= s.DetectedAt && ev.date <= s.ValidThrough {
				outcome = GTConfirmed
				evidenceDate = ev.date
				notes = fmt.Sprintf("friction signal (%s) at %s on %s — within nomination_rejection window [%s, %s]",
					ev.kind, s.Ticker, ev.date, s.DetectedAt, s.ValidThrough)
				break
			}
		}
		if outcome == GTPending && today > s.ValidThrough {
			outcome = GTRefuted
			notes = fmt.Sprintf("nomination_rejection window [%s, %s] expired for %s; no structural friction signal observed",
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
			EvidenceType: "director_friction_or_abstention_spike",
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
