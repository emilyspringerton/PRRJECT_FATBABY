package prreaction

import "time"

// ScheduledEvent is emitted once per tracked release (event type
// "price_reaction_scheduled") -- the baseline record naming what's being
// tracked and from when.
type ScheduledEvent struct {
	Identity    string    `json:"identity"` // filing identity ("CIK:ACCESSION") or PR discovery ID
	Ticker      string    `json:"ticker"`
	Kind        string    `json:"kind"` // "filing" or "press_release"
	Headline    string    `json:"headline,omitempty"`
	Form        string    `json:"form,omitempty"`
	ReleaseTime time.Time `json:"release_time"`
}

// SampleEvent is emitted once per (release, offset) pair actually sampled
// (event type "price_reaction_sample").
type SampleEvent struct {
	Identity      string    `json:"identity"`
	Ticker        string    `json:"ticker"`
	Offset        Offset    `json:"offset"`
	TargetTime    time.Time `json:"target_time"`
	SampledAt     time.Time `json:"sampled_at"`
	Price         float64   `json:"price"`
	BaselinePrice float64   `json:"baseline_price,omitempty"` // the release's own t0 price; 0 on the t0 sample itself
	PctChange     float64   `json:"pct_change,omitempty"`     // (Price-BaselinePrice)/BaselinePrice*100; 0 on the t0 sample itself
}
