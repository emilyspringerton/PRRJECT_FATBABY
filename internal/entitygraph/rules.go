package entitygraph

import (
	"encoding/json"
	"os"
)

// Rules holds the signal scoring thresholds used by the scorer.
// Claude Code can modify config/entity-graph-rules.json between runs
// as part of the recursive self-improvement loop; the entity-graph
// process re-reads it on each processing batch.
type Rules struct {
	// FrictionThreshold is the maximum approval % for a "friction director" signal.
	// Default 0.85 (85%). Directors below this are flagged.
	FrictionThreshold float64 `json:"friction_threshold"`

	// HighTrustMinApproval is the minimum approval % for "high trust director".
	// Default 0.95 (95%).
	HighTrustMinApproval float64 `json:"high_trust_min_approval"`

	// HighTrustMinFilings is the minimum number of filings required before
	// granting "high trust" status. Default 1 (relaxed for Phase 1 bootstrap).
	HighTrustMinFilings int `json:"high_trust_min_filings"`

	// DecayMinYears is the minimum number of YoY data points required to
	// emit a director_decay signal. Default 2.
	DecayMinYears int `json:"decay_min_years"`

	// DecayMinDropPP is the minimum per-year approval drop (percentage points)
	// to trigger a director_decay signal. Default 2.0 pp.
	DecayMinDropPP float64 `json:"decay_min_drop_pp"`

	// EntrenchmentMinFor is the minimum "for" percentage (of votes cast) for
	// a failed governance vote to be flagged as entrenchment. Default 0.80.
	EntrenchmentMinFor float64 `json:"entrenchment_min_for"`

	// BrokerNonVoteAnomalyThreshold: broker NV as a fraction of total shares
	// voted; above this triggers the broker_nonvote_anomaly signal. Default 0.15.
	BrokerNonVoteAnomalyThreshold float64 `json:"broker_nonvote_anomaly_threshold"`

	// CompExecAlertThreshold: "against" fraction of advisory comp vote that
	// triggers a compensation_concern signal. Default 0.30.
	CompExecAlertThreshold float64 `json:"comp_exec_alert_threshold"`

	// FamilyNameKeywords is a list of substrings that suggest a family/founder
	// director seat (matched against canonical name). The check is supplemental;
	// direct board seat context overrides this when available.
	FamilyNameKeywords []string `json:"family_name_keywords"`

	// AbstentionSpikeThreshold: abstention votes as a fraction of total votes cast;
	// above this triggers an abstention_spike signal indicating possible shareholder
	// confusion or protest. Default 0.10 (10%).
	AbstentionSpikeThreshold float64 `json:"abstention_spike_threshold"`

	// NominationRejectionThreshold: director approval below this fraction triggers
	// a nomination_rejection (critical) signal. Under majority voting standards,
	// sub-threshold directors must submit resignations. Default 0.50 (50%).
	NominationRejectionThreshold float64 `json:"nomination_rejection_threshold"`

	// ActivistRiskWindowDays: lookback window (in days) used by ScoreCompositeActivistRisk
	// to check whether governance_entrenchment and director_friction co-occur.
	// Default 365 (1 year) — typical activist campaign gestation period.
	ActivistRiskWindowDays int `json:"activist_risk_window_days"`

	// GovernanceHealthWindowDays: lookback window (in days) used by ScoreGovernanceHealth
	// when aggregating all signal types into a composite health index.
	// Default 365 — matches activist_risk window so both composites use the same view.
	GovernanceHealthWindowDays int `json:"governance_health_window_days"`

	// AccelerationDecayPPThreshold: single-year approval drop (in percentage points)
	// that triggers a director_decay signal even when DecayMinYears data points are
	// not yet available. Catches the Herringer pattern early: a sharp single-year drop
	// (e.g. 4 pp) is a leading indicator before multi-year trend confirmation.
	// Default 4.0 pp. Set to 0 to use default.
	AccelerationDecayPPThreshold float64 `json:"acceleration_decay_pp_threshold"`

	// AbstentionOutlierMultiplier: a director's abstain rate must exceed this multiple
	// of the filing-median abstain rate AND 1% absolute to trigger an abstention_outlier
	// signal. Identifies targeted protest voting directed at a specific director, as
	// distinct from the across-the-board abstention_spike on proposals.
	// Default 2.5. Set to 0 to use default.
	AbstentionOutlierMultiplier float64 `json:"abstention_outlier_multiplier"`

	// CompVoteKeywords is the set of lowercase substrings that identify an advisory
	// compensation vote in a proposal description. Matched case-insensitively.
	// Default: ["compensation", "executive", "say-on-pay"].
	// Override to add "remuneration", jurisdiction-specific phrasings, etc.
	CompVoteKeywords []string `json:"comp_vote_keywords"`

	// MinBoardDecayCount is the minimum number of distinct directors at a ticker
	// with active decay signals to trigger a board_decay_concern composite signal.
	// Default 3.
	MinBoardDecayCount int `json:"min_board_decay_count"`

	// BoardDecayConcernWindowDays is the lookback window (in days) used by
	// ScoreBoardDecayConcern when counting concurrent director_decay signals.
	// Default 730 (2 proxy seasons).
	BoardDecayConcernWindowDays int `json:"board_decay_concern_window_days"`

	// LongTenureYearsThreshold is the number of consecutive years a director must
	// have served at a single company before ScoreLongTenure fires. ISS/Glass Lewis
	// typically flag tenure >12 years as an independence concern. Default 12.
	LongTenureYearsThreshold int `json:"long_tenure_years_threshold"`

	// PeerGovernanceUnderperformThreshold is the minimum gap (as a fraction of the
	// 0-1 health index) between a ticker's score and its sector median before
	// ScorePeerGovernanceRank fires a governance_peer_underperformer signal.
	// Default 0.15 (15 points on the 0-100 scale).
	PeerGovernanceUnderperformThreshold float64 `json:"peer_governance_underperform_threshold"`

	// GovernanceHealthTrendMinDelta is the minimum absolute change in governance health
	// score between batches to emit a governance_deteriorating or governance_improving
	// signal. Default 0.10 (10 points). Set smaller to catch gradual drift; larger to
	// suppress noise on volatile single-filing tickers.
	GovernanceHealthTrendMinDelta float64 `json:"governance_health_trend_min_delta"`
}

// DefaultRules returns the baseline thresholds defined in the northstar spec.
func DefaultRules() Rules {
	return Rules{
		FrictionThreshold:             0.85,
		HighTrustMinApproval:          0.95,
		HighTrustMinFilings:           1,
		DecayMinYears:                 2,
		DecayMinDropPP:                2.0,
		EntrenchmentMinFor:            0.80,
		BrokerNonVoteAnomalyThreshold: 0.15,
		CompExecAlertThreshold:        0.30,
		FamilyNameKeywords:            []string{"schwab", "walton", "mars", "buffett"},
		AbstentionSpikeThreshold:      0.10,
		NominationRejectionThreshold:  0.50,
		ActivistRiskWindowDays:        365,
		GovernanceHealthWindowDays:    365,
		AccelerationDecayPPThreshold:  4.0,
		AbstentionOutlierMultiplier:   2.5,
		CompVoteKeywords:              []string{"compensation", "executive", "say-on-pay", "remuneration", "advisory vote on pay"},
		MinBoardDecayCount:                  3,
		BoardDecayConcernWindowDays:         730,
		LongTenureYearsThreshold:            12,
		PeerGovernanceUnderperformThreshold: 0.15,
		GovernanceHealthTrendMinDelta:       0.10,
	}
}

// LoadRules reads rules from a JSON file, falling back to defaults on any error.
func LoadRules(path string) Rules {
	defaults := DefaultRules()
	if path == "" {
		return defaults
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	var r Rules
	if err := json.Unmarshal(data, &r); err != nil {
		return defaults
	}
	// Fill zero-values with defaults so partial config files work.
	if r.FrictionThreshold == 0 {
		r.FrictionThreshold = defaults.FrictionThreshold
	}
	if r.HighTrustMinApproval == 0 {
		r.HighTrustMinApproval = defaults.HighTrustMinApproval
	}
	if r.HighTrustMinFilings == 0 {
		r.HighTrustMinFilings = defaults.HighTrustMinFilings
	}
	if r.DecayMinYears == 0 {
		r.DecayMinYears = defaults.DecayMinYears
	}
	if r.DecayMinDropPP == 0 {
		r.DecayMinDropPP = defaults.DecayMinDropPP
	}
	if r.EntrenchmentMinFor == 0 {
		r.EntrenchmentMinFor = defaults.EntrenchmentMinFor
	}
	if r.BrokerNonVoteAnomalyThreshold == 0 {
		r.BrokerNonVoteAnomalyThreshold = defaults.BrokerNonVoteAnomalyThreshold
	}
	if r.CompExecAlertThreshold == 0 {
		r.CompExecAlertThreshold = defaults.CompExecAlertThreshold
	}
	if len(r.FamilyNameKeywords) == 0 {
		r.FamilyNameKeywords = defaults.FamilyNameKeywords
	}
	if r.AbstentionSpikeThreshold == 0 {
		r.AbstentionSpikeThreshold = defaults.AbstentionSpikeThreshold
	}
	if r.NominationRejectionThreshold == 0 {
		r.NominationRejectionThreshold = defaults.NominationRejectionThreshold
	}
	if r.ActivistRiskWindowDays == 0 {
		r.ActivistRiskWindowDays = defaults.ActivistRiskWindowDays
	}
	if r.GovernanceHealthWindowDays == 0 {
		r.GovernanceHealthWindowDays = defaults.GovernanceHealthWindowDays
	}
	if r.AccelerationDecayPPThreshold == 0 {
		r.AccelerationDecayPPThreshold = defaults.AccelerationDecayPPThreshold
	}
	if r.AbstentionOutlierMultiplier == 0 {
		r.AbstentionOutlierMultiplier = defaults.AbstentionOutlierMultiplier
	}
	if len(r.CompVoteKeywords) == 0 {
		r.CompVoteKeywords = defaults.CompVoteKeywords
	}
	if r.MinBoardDecayCount == 0 {
		r.MinBoardDecayCount = defaults.MinBoardDecayCount
	}
	if r.BoardDecayConcernWindowDays == 0 {
		r.BoardDecayConcernWindowDays = defaults.BoardDecayConcernWindowDays
	}
	if r.LongTenureYearsThreshold == 0 {
		r.LongTenureYearsThreshold = defaults.LongTenureYearsThreshold
	}
	if r.PeerGovernanceUnderperformThreshold == 0 {
		r.PeerGovernanceUnderperformThreshold = defaults.PeerGovernanceUnderperformThreshold
	}
	if r.GovernanceHealthTrendMinDelta == 0 {
		r.GovernanceHealthTrendMinDelta = defaults.GovernanceHealthTrendMinDelta
	}
	return r
}
