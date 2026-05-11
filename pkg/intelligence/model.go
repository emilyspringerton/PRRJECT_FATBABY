package intelligence

import "time"

type Signal struct {
	ID             string            `json:"id"`
	Ticker         string            `json:"ticker"`
	Timestamp      time.Time         `json:"timestamp"`
	SignalType     string            `json:"signal_type"`
	Importance     int               `json:"importance"`
	Sentiment      float64           `json:"sentiment"`
	Summary        string            `json:"summary"`
	ImpactAnalysis string            `json:"impact_analysis"`
	RawMetadata    map[string]string `json:"raw_metadata,omitempty"`
}
