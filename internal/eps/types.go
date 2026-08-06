// Package eps implements GAAP EPS extraction from earnings press releases,
// validation gates, and the oracle reconciliation types used to drive the
// recursive self-improvement loop described in docs/headlines/eps.md.
package eps

// ExtractionFlag is a diagnostic tag attached to an EarningsData when extraction
// is uncertain or a validation gate fires.
type ExtractionFlag string

const (
	// Extraction quality flags.
	FlagLowConfidence      ExtractionFlag = "low_confidence"          // confidence < 0.70
	FlagMultipleCandidates ExtractionFlag = "multiple_eps_candidates" // ambiguous source text
	FlagNoGAAP             ExtractionFlag = "no_gaap_eps_found"       // only non-GAAP present
	FlagAdjustedOnly       ExtractionFlag = "adjusted_only"           // release lacks GAAP line

	// Validation gate flags.
	FlagSignMismatch    ExtractionFlag = "sign_mismatch"     // loss language ≠ positive sign
	FlagReconcileFail   ExtractionFlag = "reconcile_fail"    // net_income/shares ≠ eps
	FlagPeriodAmbiguous ExtractionFlag = "period_ambiguous"  // quarter unclear from text
	FlagTraceFailure    ExtractionFlag = "trace_failure"     // headline number not in source
	FlagPriorPeriod     ExtractionFlag = "prior_period_risk" // likely grabbed comparison figure
)

// EarningsPeriod identifies the fiscal period an earnings release covers.
type EarningsPeriod struct {
	FiscalQuarter string `json:"fiscal_quarter"` // Q1 | Q2 | Q3 | Q4 | FY
	FiscalYear    int    `json:"fiscal_year"`
	// PeriodEnd is the last day of the reported period in YYYY-MM-DD format.
	// Extracted from the release when possible; estimated from quarter when not.
	PeriodEnd string `json:"period_end"`
	// IsFiscalCalendarAligned is false when the issuer's fiscal quarter does not
	// align with the calendar quarter (e.g. Apple's Q1 ends in December).
	IsFiscalCalendarAligned bool `json:"is_fiscal_calendar_aligned"`
}

// EarningsData is the structured output of eps.Extract for one earnings release.
// Nil pointer fields mean the value was not found or could not be reliably extracted.
type EarningsData struct {
	// SourceIdentity links back to the intelligence.SourceDocument this was extracted from.
	SourceIdentity string         `json:"source_identity"`
	Issuer         string         `json:"issuer"`
	Ticker         string         `json:"ticker"`
	Period         EarningsPeriod `json:"period"`

	// EPSGAAPDiluted is the headline number: GAAP diluted EPS for the current period.
	// This is the number that drives the headline when present.
	EPSGAAPDiluted *float64 `json:"eps_gaap_diluted"`
	EPSGAAPBasic   *float64 `json:"eps_gaap_basic"`

	// EPSAdjusted is the non-GAAP / "adjusted" / "core" / "pro forma" figure.
	// Body-only; never published as the headline without explicit labeling.
	EPSAdjusted *float64 `json:"eps_adjusted"`

	// EPSBasis records whether the EPS is on a total-net or continuing-operations basis.
	// "total" | "continuing_ops" | ""
	EPSBasis string `json:"eps_basis"`

	Currency      string   `json:"currency"`       // ISO 4217; "USD" when not specified
	NetIncome     *float64 `json:"net_income"`     // in millions USD
	SharesDiluted *float64 `json:"shares_diluted"` // in millions
	Revenue       *float64 `json:"revenue"`        // in millions USD

	// YoYEPS is the same-period prior-year diluted EPS for YoY comparison.
	YoYEPS *float64 `json:"yoy_eps"`

	// ExtractionConfidence is a [0.0, 1.0] score produced by Extract.
	// Below 0.70 the flag FlagLowConfidence is set and auto-publish is blocked.
	ExtractionConfidence float64          `json:"extraction_confidence"`
	ExtractionFlags      []ExtractionFlag `json:"extraction_flags,omitempty"`
}

// HasFlag returns true if the given flag is present in ExtractionFlags.
func (e *EarningsData) HasFlag(f ExtractionFlag) bool {
	for _, flag := range e.ExtractionFlags {
		if flag == f {
			return true
		}
	}
	return false
}

func (e *EarningsData) addFlag(f ExtractionFlag) {
	if !e.HasFlag(f) {
		e.ExtractionFlags = append(e.ExtractionFlags, f)
	}
}

// HeadlineEPS returns the number that should appear in the headline:
// EPSGAAPDiluted when present, EPSAdjusted (labeled) when GAAP is nil.
// Returns (value, isGAAP).
func (e *EarningsData) HeadlineEPS() (float64, bool) {
	if e.EPSGAAPDiluted != nil {
		return *e.EPSGAAPDiluted, true
	}
	if e.EPSAdjusted != nil {
		return *e.EPSAdjusted, false
	}
	return 0, false
}

// OracleVerdict is the result of reconciling an EarningsData against the filed 8-K.
type OracleVerdict string

const (
	VerdictConfirmed   OracleVerdict = "confirmed"   // filed EPS matches headline
	VerdictContradicts OracleVerdict = "contradicts" // filed EPS differs from headline
	VerdictPending     OracleVerdict = "pending"     // 8-K not yet filed or not found
	VerdictUnavailable OracleVerdict = "unavailable" // 8-K exists but EPS not extractable
	// VerdictUnresolvable marks a case recorded without a ticker: the
	// reconciler has no identity to match a future 8-K against, so it can
	// never resolve no matter how long it waits. Distinct from Pending
	// (which genuinely will resolve once the company files) so the two
	// aren't indistinguishable in the oracle store (S159-01).
	VerdictUnresolvable OracleVerdict = "unresolvable"
)

// OracleCase is one labeled entry in the oracle store.
// Persisted in var/eps/oracle.ndjson.
type OracleCase struct {
	CaseID         string         `json:"case_id"`
	SourceIdentity string         `json:"source_identity"`
	Ticker         string         `json:"ticker"`
	Period         EarningsPeriod `json:"period"`
	// ExtractedEPS is the headline EPS from the press release.
	ExtractedEPS *float64 `json:"extracted_eps"`
	// FiledEPS is the GAAP diluted EPS from the matching 8-K/Item 2.02.
	FiledEPS       *float64      `json:"filed_eps"`
	FiledAccession string        `json:"filed_accession,omitempty"`
	Verdict        OracleVerdict `json:"verdict"`
	// Delta is FiledEPS - ExtractedEPS; nil when either is nil.
	Delta      *float64 `json:"delta,omitempty"`
	RecordedAt string   `json:"recorded_at"`
	Notes      string   `json:"notes,omitempty"`
}
