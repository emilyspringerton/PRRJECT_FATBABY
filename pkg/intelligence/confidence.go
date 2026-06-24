package intelligence

// SignalQuality holds the provenance and confidence metadata for one signal.
// It is computed at signal-generation time and stored alongside the signal.
type SignalQuality struct {
	// Score is the overall signal confidence: 0.0–1.0.
	// ≥ 0.80: institutional-grade (quantitative, reconciled)
	// 0.60–0.79: standard (qualitative, LLM-derived)
	// < 0.60: low confidence (sparse input, incomplete extraction)
	Score float64 `json:"score"`

	// SourceTrust is the base trust level assigned to the source type.
	// "sec_8k": 0.90, "sec_10q": 0.90, "sec_10k": 0.90
	// "press_release": 0.75
	// "unknown": 0.50
	SourceTrust float64 `json:"source_trust"`

	// Completeness is the fraction of expected fields that are non-empty (0–1).
	Completeness float64 `json:"completeness"`

	// QuantitativeBonus is non-zero when the signal type carries a numeric value
	// that was validated against the source text.
	QuantitativeBonus float64 `json:"quantitative_bonus,omitempty"`

	// Flags records any quality issues detected during scoring.
	Flags []string `json:"flags,omitempty"`
}

// ScoreSignal computes a SignalQuality for a Signal.
// sourceType should be the SourceDocument.SourceType that generated this signal
// (e.g. "sec_8k", "press_release"). Pass "" when unknown.
func ScoreSignal(s Signal, sourceType string) SignalQuality {
	q := SignalQuality{}

	// Base source trust.
	q.SourceTrust = sourceTrust(sourceType)

	// Completeness check: score, summary, impact_analysis, ticker, signal_type.
	total := 5
	have := 0
	if s.Ticker != "" {
		have++
	}
	if s.SignalType != "" {
		have++
	}
	if s.Summary != "" {
		have++
	}
	if s.ImpactAnalysis != "" {
		have++
	}
	if s.Importance > 0 {
		have++
	}
	q.Completeness = float64(have) / float64(total)

	// Quantitative bonus: numeric signal types with non-zero importance score get a bonus.
	q.QuantitativeBonus = quantBonus(s.SignalType)

	// Composite score.
	q.Score = q.SourceTrust*0.50 + q.Completeness*0.30 + q.QuantitativeBonus*0.20

	// Clamp.
	if q.Score > 1.0 {
		q.Score = 1.0
	}

	// Flag low confidence.
	if q.Score < 0.60 {
		q.Flags = append(q.Flags, "low_confidence")
	}
	if q.Completeness < 0.60 {
		q.Flags = append(q.Flags, "incomplete_extraction")
	}
	if s.Ticker == "" {
		q.Flags = append(q.Flags, "missing_ticker")
	}
	if s.SignalType == "" {
		q.Flags = append(q.Flags, "missing_signal_type")
	}

	return q
}

// sourceTrust returns the base reliability factor for a given source type.
func sourceTrust(sourceType string) float64 {
	switch sourceType {
	case "sec_8k", "sec_10q", "sec_10k", "sec_def14a", "sec_form4":
		return 0.90
	case "press_release":
		return 0.75
	case "sec_nt10k", "sec_nt10q":
		return 0.80
	default:
		return 0.50
	}
}

// quantBonus returns the quantitative quality bonus for a given signal type.
// Signals that represent validated numeric events score highest.
func quantBonus(signalType string) float64 {
	switch signalType {
	case "eps_beat", "eps_miss", "eps_in_line":
		return 1.0
	case "guidance_raise", "guidance_lower", "guidance_maintained":
		return 0.90
	case "dividend_raise", "dividend_cut", "special_dividend":
		return 0.90
	case "buyback_authorization", "buyback_suspension":
		return 0.85
	case "insider_buy", "insider_sell_cluster":
		return 0.85
	case "late_filing_nt10k", "late_filing_nt10q":
		return 0.80
	case "merger_acquisition", "spin_off", "restructuring":
		return 0.75
	case "product_launch", "partnership", "regulatory_approval":
		return 0.60
	default:
		return 0.50
	}
}

// Grade returns a letter grade for a confidence score.
func Grade(score float64) string {
	switch {
	case score >= 0.88:
		return "A"
	case score >= 0.76:
		return "B"
	case score >= 0.64:
		return "C"
	case score >= 0.52:
		return "D"
	default:
		return "F"
	}
}
