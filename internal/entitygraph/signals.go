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
}

// Signal represents a governance intelligence signal generated from parsed filings.
type Signal struct {
	SignalID      string            `json:"signal_id"`
	Type          SignalType        `json:"type"`
	Ticker        string            `json:"ticker"`
	Entity        string            `json:"entity,omitempty"`
	Severity      Severity          `json:"severity"`
	Confidence    float64           `json:"confidence"`
	Score         float64           `json:"score"`
	DetectedAt    string            `json:"detected_at"`
	ValidThrough  string            `json:"valid_through"`
	Interpretation string           `json:"interpretation"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// ScoreDirectorVotes produces signals for a set of director vote results from a single filing.
func ScoreDirectorVotes(votes []VoteResult, ticker string, r Rules) []Signal {
	var out []Signal
	for _, v := range votes {
		sigs := scoreOneDirector(v, ticker, r)
		out = append(out, sigs...)
	}
	return out
}

func scoreOneDirector(v VoteResult, ticker string, r Rules) []Signal {
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
func ScoreProposals(proposals []ProposalResult, ticker string, r Rules) []Signal {
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
				DetectedAt:     today,
				ValidThrough:   nextYear,
				Interpretation: fmt.Sprintf("Proposal '%s' abstention rate %.1f%% exceeds %.0f%% threshold; may indicate shareholder confusion, protest vote, or inadequate disclosure.", descShort, abstainPct*100, r.AbstentionSpikeThreshold*100),
			})
		}
	}
	return out
}

func isCompVote(desc string) bool {
	dl := strings.ToLower(desc)
	return strings.Contains(dl, "compensation") || strings.Contains(dl, "executive") || strings.Contains(dl, "say-on-pay")
}

// ScoreAuditorChange emits an auditor_change signal when a company switches its
// public accounting firm between filings. Auditor changes are a known risk signal:
// they can indicate audit quality disputes, regulatory pressure, or pre-transaction
// restructuring.
func ScoreAuditorChange(ticker, prevAuditor, newAuditor string) Signal {
	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
	return Signal{
		SignalID:       fmt.Sprintf("auditor_change_%s", strings.ToLower(ticker)),
		Type:           SignalAuditorChange,
		Ticker:         ticker,
		Severity:       SeverityMedium,
		Confidence:     0.95,
		Score:          1.0,
		DetectedAt:     today,
		ValidThrough:   nextYear,
		Interpretation: fmt.Sprintf("Auditor changed from %q to %q. Auditor changes may indicate audit quality disputes, fee negotiations, regulatory pressure, or pre-transaction restructuring.", prevAuditor, newAuditor),
		Metadata:       map[string]string{"prev_auditor": prevAuditor, "new_auditor": newAuditor},
	}
}
