package entitygraph

import (
	"fmt"
	"strings"
	"time"
)

// Severity levels for signals.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// SignalType is the type tag for a governance signal.
type SignalType string

const (
	SignalDirectorFriction       SignalType = "director_friction"
	SignalHighTrustDirector      SignalType = "high_trust_director"
	SignalDirectorDecay          SignalType = "director_decay"
	SignalFamilyControl          SignalType = "family_control"
	SignalGovernanceEntrenchment SignalType = "governance_entrenchment"
	SignalBrokerNonVoteAnomaly   SignalType = "broker_nonvote_anomaly"
	SignalCompensationConcern    SignalType = "compensation_concern"
	SignalAuditorChange          SignalType = "auditor_change"
	SignalAbstentionSpike        SignalType = "abstention_spike"
	SignalNominationRejection    SignalType = "nomination_rejection"
	SignalActivistRisk           SignalType = "activist_risk"
	SignalDirectorLink           SignalType = "director_link"
	SignalGovernanceHealth       SignalType = "governance_health_index"
)

// AllSignalTypes is the canonical ordered list used to zero-fill signals_by_type
// in observations so every type shows up whether or not it fired.
var AllSignalTypes = []SignalType{
	SignalDirectorFriction,
	SignalHighTrustDirector,
	SignalDirectorDecay,
	SignalFamilyControl,
	SignalGovernanceEntrenchment,
	SignalBrokerNonVoteAnomaly,
	SignalCompensationConcern,
	SignalAuditorChange,
	SignalAbstentionSpike,
	SignalNominationRejection,
	SignalActivistRisk,
	SignalDirectorLink,
	SignalGovernanceHealth,
}

// Signal represents a governance intelligence signal generated from parsed filings.
type Signal struct {
	SignalID       string            `json:"signal_id"`
	Type           SignalType        `json:"type"`
	Ticker         string            `json:"ticker"`
	Entity         string            `json:"entity,omitempty"`
	Severity       Severity          `json:"severity"`
	Confidence     float64           `json:"confidence"`
	Score          float64           `json:"score"`
	FilingDate     string            `json:"filing_date,omitempty"` // SEC filing date; primary display date
	DetectedAt     string            `json:"detected_at"`           // pipeline processing date; shown as footnote
	ValidThrough   string            `json:"valid_through"`
	Interpretation string            `json:"interpretation"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// ScoreDirectorVotes produces signals for a set of director vote results from a single filing.
// filingDate is the SEC filing date (e.g. "2019-04-26"); pass "" for composite/derived signals.
func ScoreDirectorVotes(votes []VoteResult, ticker, filingDate string, r Rules) []Signal {
	var out []Signal
	for _, v := range votes {
		sigs := scoreOneDirector(v, ticker, filingDate, r)
		out = append(out, sigs...)
	}
	return out
}

func scoreOneDirector(v VoteResult, ticker, filingDate string, r Rules) []Signal {
	canon := Canonicalize(v.Name)
	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	var out []Signal

	switch {
	case v.ApprovalPct < r.NominationRejectionThreshold:
		// Director failed to secure majority support — critical governance signal.
		// Under majority voting standards (now common in S&P 500), sub-50% triggers
		// mandatory resignation submission to the board.
		out = append(out, Signal{
			SignalID:       fmt.Sprintf("nomination_rejection_%s_%s", canon, strings.ToLower(ticker)),
			Type:           SignalNominationRejection,
			Ticker:         ticker,
			Entity:         v.Name,
			Severity:       SeverityCritical,
			Confidence:     0.95,
			Score:          v.ApprovalPct,
			FilingDate:     filingDate,
			DetectedAt:     today,
			ValidThrough:   nextYear,
			Interpretation: fmt.Sprintf("Director received only %.1f%% approval — below %.0f%% majority threshold. Under majority voting standards the director must submit a resignation; board refusal to accept is a governance crisis indicator.", v.ApprovalPct*100, r.NominationRejectionThreshold*100),
		})

	case v.ApprovalPct < r.FrictionThreshold:
		sev := SeverityMedium
		conf := 0.78
		if v.ApprovalPct < 0.80 {
			sev = SeverityHigh
			conf = 0.85
		}
		out = append(out, Signal{
			SignalID:       fmt.Sprintf("friction_%s_%s", canon, strings.ToLower(ticker)),
			Type:           SignalDirectorFriction,
			Ticker:         ticker,
			Entity:         v.Name,
			Severity:       sev,
			Confidence:     conf,
			Score:          v.ApprovalPct,
			FilingDate:     filingDate,
			DetectedAt:     today,
			ValidThrough:   nextYear,
			Interpretation: fmt.Sprintf("Director approval at %.1f%% is below the %.0f%% friction threshold; may indicate activist targeting or board misalignment.", v.ApprovalPct*100, r.FrictionThreshold*100),
		})

	case v.ApprovalPct >= r.HighTrustMinApproval:
		out = append(out, Signal{
			SignalID:       fmt.Sprintf("high_trust_%s_%s", canon, strings.ToLower(ticker)),
			Type:           SignalHighTrustDirector,
			Ticker:         ticker,
			Entity:         v.Name,
			Severity:       SeverityLow,
			Confidence:     0.70,
			Score:          v.ApprovalPct,
			FilingDate:     filingDate,
			DetectedAt:     today,
			ValidThrough:   nextYear,
			Interpretation: fmt.Sprintf("Director approval at %.1f%% meets high-trust threshold (>%.0f%%).", v.ApprovalPct*100, r.HighTrustMinApproval*100),
		})
	}

	// Family control: keyword match on canonical name.
	for _, kw := range r.FamilyNameKeywords {
		if strings.Contains(canon, kw) {
			out = append(out, Signal{
				SignalID:       fmt.Sprintf("family_control_%s_%s", canon, strings.ToLower(ticker)),
				Type:           SignalFamilyControl,
				Ticker:         ticker,
				Entity:         v.Name,
				Severity:       SeverityMedium,
				Confidence:     0.65,
				Score:          v.ApprovalPct,
				FilingDate:     filingDate,
				DetectedAt:     today,
				ValidThrough:   nextYear,
				Interpretation: fmt.Sprintf("Director name contains founder/family keyword '%s'; may represent founder control concentration.", kw),
				Metadata:       map[string]string{"keyword": kw},
			})
			break
		}
	}

	// Broker non-vote anomaly: BNV > threshold fraction of total voted.
	totalVoted := v.ForVotes + v.AgainstVotes + v.AbstainVotes
	if totalVoted > 0 && v.BrokerNonVotes > 0 {
		bnvFrac := float64(v.BrokerNonVotes) / float64(totalVoted+v.BrokerNonVotes)
		if bnvFrac > r.BrokerNonVoteAnomalyThreshold {
			out = append(out, Signal{
				SignalID:       fmt.Sprintf("bnv_anomaly_%s_%s", canon, strings.ToLower(ticker)),
				Type:           SignalBrokerNonVoteAnomaly,
				Ticker:         ticker,
				Entity:         v.Name,
				Severity:       SeverityLow,
				Confidence:     0.60,
				Score:          bnvFrac,
				FilingDate:     filingDate,
				DetectedAt:     today,
				ValidThrough:   nextYear,
				Interpretation: fmt.Sprintf("Broker non-votes represent %.1f%% of total shares — above %.0f%% anomaly threshold.", bnvFrac*100, r.BrokerNonVoteAnomalyThreshold*100),
			})
		}
	}

	return out
}

// ScoreDirectorDecay emits a decay signal when a director's approval trend is declining.
// approvalHistory should be ordered oldest-to-newest.
func ScoreDirectorDecay(name, ticker string, approvalHistory []float64, r Rules) *Signal {
	if len(approvalHistory) < r.DecayMinYears {
		return nil
	}
	// Compute average year-over-year drop.
	drops := 0.0
	for i := 1; i < len(approvalHistory); i++ {
		drops += approvalHistory[i-1] - approvalHistory[i]
	}
	avgDrop := drops / float64(len(approvalHistory)-1)
	if avgDrop < r.DecayMinDropPP/100.0 {
		return nil
	}

	canon := Canonicalize(name)
	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	sev := SeverityLow
	conf := 0.65
	if avgDrop > 0.03 {
		sev = SeverityMedium
		conf = 0.72
	}

	return &Signal{
		SignalID:       fmt.Sprintf("decay_%s_%s", canon, strings.ToLower(ticker)),
		Type:           SignalDirectorDecay,
		Ticker:         ticker,
		Entity:         name,
		Severity:       sev,
		Confidence:     conf,
		Score:          avgDrop,
		DetectedAt:     today,
		ValidThrough:   nextYear,
		Interpretation: fmt.Sprintf("Director approval declining avg %.1f pp/year over %d data points. Continued trend suggests replacement within 12-18 months.", avgDrop*100, len(approvalHistory)),
	}
}

// ScoreProposals emits signals from non-director proposal results.
// filingDate is the SEC filing date (e.g. "2019-04-26"); pass "" for composite/derived signals.
func ScoreProposals(proposals []ProposalResult, ticker, filingDate string, r Rules) []Signal {
	var out []Signal
	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	for i, p := range proposals {
		total := p.ForVotes + p.AgainstVotes + p.AbstainVotes
		if total == 0 {
			continue
		}
		forPct := float64(p.ForVotes) / float64(total)
		descShort := p.Description
		if len(descShort) > 60 {
			descShort = descShort[:60]
		}

		// Governance entrenchment: high support, failed due to supermajority.
		if !p.Passed && p.RequiredPct > 0 && forPct >= r.EntrenchmentMinFor {
			out = append(out, Signal{
				SignalID:       fmt.Sprintf("entrenchment_%s_prop%d", strings.ToLower(ticker), i+2),
				Type:           SignalGovernanceEntrenchment,
				Ticker:         ticker,
				Severity:       SeverityHigh,
				Confidence:     0.91,
				Score:          forPct,
				FilingDate:     filingDate,
				DetectedAt:     today,
				ValidThrough:   nextYear,
				Interpretation: fmt.Sprintf("Proposal '%s' received %.1f%% shareholder support but failed due to %.0f%% of outstanding shares supermajority requirement. Board is using structural defenses against clear shareholder preference.", descShort, forPct*100, p.RequiredPct*100),
				Metadata:       map[string]string{"required_pct": fmt.Sprintf("%.2f", p.RequiredPct), "for_pct": fmt.Sprintf("%.4f", forPct)},
			})
		}

		// Compensation concern: advisory comp vote with high opposition.
		if isCompVote(p.Description) {
			againstPct := float64(p.AgainstVotes) / float64(total)
			if againstPct >= r.CompExecAlertThreshold {
				out = append(out, Signal{
					SignalID:       fmt.Sprintf("comp_concern_%s_prop%d", strings.ToLower(ticker), i+2),
					Type:           SignalCompensationConcern,
					Ticker:         ticker,
					Severity:       SeverityMedium,
					Confidence:     0.75,
					Score:          againstPct,
					FilingDate:     filingDate,
					DetectedAt:     today,
					ValidThrough:   nextYear,
					Interpretation: fmt.Sprintf("Advisory compensation vote received %.1f%% opposition. Exceeds %.0f%% alert threshold; potential ESG or pay-for-performance concern.", againstPct*100, r.CompExecAlertThreshold*100),
				})
			}
		}

		// Abstention spike: unusual abstention rate may indicate shareholder confusion or protest.
		abstainPct := float64(p.AbstainVotes) / float64(total)
		if abstainPct >= r.AbstentionSpikeThreshold {
			descShort := p.Description
			if len(descShort) > 60 {
				descShort = descShort[:60]
			}
			out = append(out, Signal{
				SignalID:       fmt.Sprintf("abstention_spike_%s_prop%d", strings.ToLower(ticker), i+2),
				Type:           SignalAbstentionSpike,
				Ticker:         ticker,
				Severity:       SeverityLow,
				Confidence:     0.60,
				Score:          abstainPct,
				FilingDate:     filingDate,
				DetectedAt:     today,
				ValidThrough:   nextYear,
				Interpretation: fmt.Sprintf("Proposal '%s' abstention rate %.1f%% exceeds %.0f%% threshold; may indicate shareholder confusion, protest vote, or inadequate disclosure.", descShort, abstainPct*100, r.AbstentionSpikeThreshold*100),
			})
		}
	}
	return out
}

// ScoreCompositeActivistRisk fires when governance_entrenchment and director_friction
// (or nomination_rejection) co-occur at the same ticker within windowDays. The
// historical pattern: board entrenchment + director dissent precedes activist 13D
// filings within 6 months in roughly 60% of documented cases.
//
// allSignals should include both current-batch and previously stored signals so the
// composite can fire even when the two components arrived in different filings.
func ScoreCompositeActivistRisk(ticker string, allSignals []Signal, windowDays int) *Signal {
	cutoff := time.Now().UTC().AddDate(0, 0, -windowDays).Format("2006-01-02")

	var (
		hasEntrenchment  bool
		hasFriction      bool
		worstFrictionPct float64 = 1.0 // lower = worse; track the most alarming director
	)
	for _, s := range allSignals {
		if s.Ticker != ticker || s.DetectedAt < cutoff {
			continue
		}
		switch s.Type {
		case SignalGovernanceEntrenchment:
			hasEntrenchment = true
		case SignalDirectorFriction, SignalNominationRejection:
			hasFriction = true
			if s.Score < worstFrictionPct {
				worstFrictionPct = s.Score
			}
		}
	}
	if !hasEntrenchment || !hasFriction {
		return nil
	}

	// Composite score: fraction of outstanding-share support that the entrenched board
	// is blocking, weighted by worst director approval deficit.
	compositeScore := 1.0 - worstFrictionPct // 0.157 for Herringer at 84.3%

	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	return &Signal{
		SignalID:       fmt.Sprintf("activist_risk_%s_%s", strings.ToLower(ticker), today),
		Type:           SignalActivistRisk,
		Ticker:         ticker,
		Severity:       SeverityHigh,
		Confidence:     0.82,
		Score:          compositeScore,
		DetectedAt:     today,
		ValidThrough:   nextYear,
		Interpretation: fmt.Sprintf("Co-occurrence of governance_entrenchment and director_friction at %s within %d days. Board is using structural defenses (supermajority threshold) while at least one director faces declining shareholder support (%.1f%% approval). Historical base rate: activist 13D filed within 6 months in ~60%% of similar co-occurrences.", ticker, windowDays, worstFrictionPct*100),
	}
}

// ScoreDirectorLinks emits director_link signals for companies that share a friction
// director with another company. Governance risk "propagates" through a director's
// board portfolio: low approval at one company implies monitoring risk at others.
// Requires multi-company graph data; returns nil if no shared directors are found.
func ScoreDirectorLinks(graph *Graph, allSignals []Signal) []Signal {
	// Index friction/rejection signals by canonical director id.
	frictionByDirector := map[string]Signal{}
	for _, s := range allSignals {
		if s.Type != SignalDirectorFriction && s.Type != SignalNominationRejection {
			continue
		}
		canon := Canonicalize(s.Entity)
		existing, ok := frictionByDirector[canon]
		if !ok || s.Score < existing.Score {
			frictionByDirector[canon] = s
		}
	}
	if len(frictionByDirector) == 0 {
		return nil
	}

	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	var out []Signal

	for canonID, frictionSig := range frictionByDirector {
		node, ok := graph.Nodes[canonID]
		if !ok {
			continue
		}
		// Collect distinct tickers other than the source of friction.
		otherTickers := map[string]bool{}
		for _, f := range node.Filings {
			if f.Ticker != frictionSig.Ticker {
				otherTickers[f.Ticker] = true
			}
		}
		for ticker := range otherTickers {
			out = append(out, Signal{
				SignalID:       fmt.Sprintf("director_link_%s_%s", canonID, strings.ToLower(ticker)),
				Type:           SignalDirectorLink,
				Ticker:         ticker,
				Entity:         node.Name,
				Severity:       SeverityLow,
				Confidence:     0.65,
				Score:          frictionSig.Score,
				DetectedAt:     today,
				ValidThrough:   nextYear,
				Interpretation: fmt.Sprintf("Director %s has friction at %s (%.1f%% approval) and also serves on the board at %s. Friction score implies potential governance risk propagation across this director's portfolio.", node.Name, frictionSig.Ticker, frictionSig.Score*100, ticker),
				Metadata:       map[string]string{"source_ticker": frictionSig.Ticker, "source_signal": frictionSig.SignalID},
			})
		}
	}
	return out
}

func isCompVote(desc string) bool {
	dl := strings.ToLower(desc)
	return strings.Contains(dl, "compensation") || strings.Contains(dl, "executive") || strings.Contains(dl, "say-on-pay")
}

// ScoreGovernanceHealth computes a composite governance health index for a ticker
// based on all signals within the trailing windowDays. Score ranges [0.0, 1.0]:
// 1.0 = excellent governance (all high-trust, no adverse signals),
// 0.0 = severe governance crisis (multiple critical adverse signals).
//
// Scoring: start at 1.0, subtract a weighted penalty for each adverse signal,
// add +0.05 per high_trust_director signal (capped at +0.20). Does not count
// other governance_health_index signals to avoid circular compounding.
// Returns nil if no signals are found for the ticker in the window.
func ScoreGovernanceHealth(ticker string, allSignals []Signal, windowDays int) *Signal {
	cutoff := time.Now().UTC().AddDate(0, 0, -windowDays).Format("2006-01-02")

	penalties := map[SignalType]float64{
		SignalNominationRejection:    0.40,
		SignalGovernanceEntrenchment: 0.30,
		SignalActivistRisk:           0.25,
		SignalAuditorChange:          0.20,
		SignalDirectorFriction:       0.20,
		SignalCompensationConcern:    0.15,
		SignalDirectorDecay:          0.10,
		SignalFamilyControl:          0.10,
		SignalDirectorLink:           0.10,
		SignalBrokerNonVoteAnomaly:   0.05,
		SignalAbstentionSpike:        0.05,
	}

	score := 1.0
	trustBonus := 0.0
	signalCount := 0

	for _, s := range allSignals {
		if s.Ticker != ticker || s.DetectedAt < cutoff || s.Type == SignalGovernanceHealth {
			continue
		}
		signalCount++
		if s.Type == SignalHighTrustDirector {
			trustBonus += 0.05
		} else if p, ok := penalties[s.Type]; ok {
			score -= p
		}
	}

	if signalCount == 0 {
		return nil
	}

	if trustBonus > 0.20 {
		trustBonus = 0.20
	}
	score += trustBonus
	if score < 0.0 {
		score = 0.0
	}
	if score > 1.0 {
		score = 1.0
	}

	sev := SeverityLow
	conf := 0.70
	var interp string
	switch {
	case score < 0.40:
		sev = SeverityHigh
		conf = 0.80
		interp = fmt.Sprintf("Composite governance health score for %s: %.2f/1.00 — severe concerns. Multiple adverse signals co-present within %d days; elevated probability of activist intervention or board restructuring.", ticker, score, windowDays)
	case score < 0.60:
		sev = SeverityMedium
		conf = 0.75
		interp = fmt.Sprintf("Composite governance health score for %s: %.2f/1.00 — moderate concerns. Adverse governance signals present within %d days; monitor for escalation.", ticker, score, windowDays)
	default:
		interp = fmt.Sprintf("Composite governance health score for %s: %.2f/1.00 — healthy governance. Minimal adverse signals within %d days.", ticker, score, windowDays)
	}

	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	return &Signal{
		SignalID:       fmt.Sprintf("governance_health_%s_%s", strings.ToLower(ticker), today),
		Type:           SignalGovernanceHealth,
		Ticker:         ticker,
		Severity:       sev,
		Confidence:     conf,
		Score:          score,
		DetectedAt:     today,
		ValidThrough:   nextYear,
		Interpretation: interp,
	}
}

// ScoreAuditorChange emits an auditor_change signal when a company switches its
// public accounting firm between filings. Auditor changes are a known risk signal:
// they can indicate audit quality disputes, regulatory pressure, or pre-transaction
// restructuring.
// filingDate is the SEC filing date of the filing where the change was detected.
func ScoreAuditorChange(ticker, prevAuditor, newAuditor, filingDate string) Signal {
	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	return Signal{
		SignalID:       fmt.Sprintf("auditor_change_%s", strings.ToLower(ticker)),
		Type:           SignalAuditorChange,
		Ticker:         ticker,
		Severity:       SeverityMedium,
		Confidence:     0.95,
		Score:          1.0,
		FilingDate:     filingDate,
		DetectedAt:     today,
		ValidThrough:   nextYear,
		Interpretation: fmt.Sprintf("Auditor changed from %q to %q. Auditor changes may indicate audit quality disputes, fee negotiations, regulatory pressure, or pre-transaction restructuring.", prevAuditor, newAuditor),
		Metadata:       map[string]string{"prev_auditor": prevAuditor, "new_auditor": newAuditor},
	}
}
