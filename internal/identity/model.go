package identity

type SecurityRef struct {
	Exchange    string  `json:"exchange,omitempty"`
	Symbol      string  `json:"symbol,omitempty"`
	CIK         string  `json:"cik,omitempty"`
	Source      string  `json:"source"`
	Confidence  float32 `json:"confidence"`
	MatchedText string  `json:"matched_text,omitempty"`
	// SkuldmarkID is the real SKULDMARK-25 instrument identifier (see the
	// SKULDMARK repo), minted when Exchange/Symbol/CIK are all known. Empty,
	// not guessed, when any of those three is missing or unresolvable --
	// see internal/skuldmarkid.
	SkuldmarkID string `json:"skuldmark_id,omitempty"`
}

type DiscoveryIdentity struct {
	PrimaryTicker *SecurityRef  `json:"primary_ticker,omitempty"`
	AllTickers    []SecurityRef `json:"all_tickers,omitempty"`
}
