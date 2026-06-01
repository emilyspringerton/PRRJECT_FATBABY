// Package guidance implements forward-looking guidance extraction from
// earnings press releases. It detects when companies raise, lower, maintain,
// or withdraw guidance on EPS and/or revenue metrics.
package guidance

// Action classifies what the company did to its guidance.
type Action string

const (
	ActionRaised     Action = "raised"
	ActionLowered    Action = "lowered"
	ActionMaintained Action = "maintained"
	ActionInitiated  Action = "initiated"
	ActionWithdrawn  Action = "withdrawn"
	ActionUpdated    Action = "updated" // changed but direction unclear
)

// Metric is the financial metric being guided.
type Metric string

const (
	MetricEPS     Metric = "eps"
	MetricRevenue Metric = "revenue"
	MetricBoth    Metric = "both"
)

// Period is the guidance period (same shape as eps.EarningsPeriod).
type Period struct {
	FiscalQuarter string `json:"fiscal_quarter"` // Q1|Q2|Q3|Q4|FY|""
	FiscalYear    int    `json:"fiscal_year"`    // 0 = unknown
}

// GuidanceData is the structured output of Extract for one press release.
type GuidanceData struct {
	SourceIdentity string `json:"source_identity"`
	Issuer         string `json:"issuer"`
	Ticker         string `json:"ticker"`

	// Action is what the company did to its guidance. Empty when no guidance found.
	Action Action `json:"action"`

	Metric   Metric `json:"metric"`
	Period   Period `json:"period"`

	// EPS guidance range. Nil when not present.
	EPSLow  *float64 `json:"eps_low"`
	EPSHigh *float64 `json:"eps_high"`

	// Revenue guidance range (millions USD). Nil when not present.
	RevenueLow  *float64 `json:"revenue_low"`
	RevenueHigh *float64 `json:"revenue_high"`

	// RevenueUnit is the scaling factor used in the source text ("billion"→1000, "million"→1).
	RevenueUnit float64 `json:"revenue_unit,omitempty"`

	// Confidence is a 0.0–1.0 score. Below 0.60 guidance is not published.
	Confidence float64 `json:"confidence"`

	// HasGuidance is true when at least one guidance metric was extracted.
	HasGuidance bool `json:"has_guidance"`
}

// Article is a publishable guidance update ready for the newssite.
type Article struct {
	ID             string  `json:"id"`
	SourceIdentity string  `json:"source_identity"`
	Ticker         string  `json:"ticker"`
	Issuer         string  `json:"issuer"`
	Headline       string  `json:"headline"`
	Body           string  `json:"body"`
	Action         Action  `json:"action"`
	Metric         Metric  `json:"metric"`
	Period         Period  `json:"period"`
	EPSLow         *float64 `json:"eps_low,omitempty"`
	EPSHigh        *float64 `json:"eps_high,omitempty"`
	RevenueLow     *float64 `json:"revenue_low,omitempty"`
	RevenueHigh    *float64 `json:"revenue_high,omitempty"`
	Confidence     float64  `json:"confidence"`
	PublishAt      string   `json:"publish_at"` // RFC3339
}
