package edition

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// Ranked is a governance signal annotated with ready-to-render display fields.
type Ranked struct {
	Signal      entitygraph.Signal
	Rank        float64
	Headline    string
	Kicker      string
	KickerClass string
	Byline      string
	Dateline    string
	Section     string
	Deck        string
}

// SectionFor maps a SignalType to a URL slug.
func SectionFor(t entitygraph.SignalType) string {
	switch t {
	case entitygraph.SignalGovernanceHealth, entitygraph.SignalGovernanceEntrenchment, entitygraph.SignalFamilyControl:
		return "governance"
	case entitygraph.SignalActivistRisk, entitygraph.SignalDirectorFriction, entitygraph.SignalNominationRejection, entitygraph.SignalDirectorLink:
		return "activism"
	case entitygraph.SignalHighTrustDirector, entitygraph.SignalDirectorDecay, entitygraph.SignalAbstentionSpike:
		return "boardroom"
	case entitygraph.SignalAuditorChange:
		return "auditor"
	case entitygraph.SignalCompensationConcern, entitygraph.SignalBrokerNonVoteAnomaly:
		return "pay"
	default:
		return "filings"
	}
}

// Rank scores and sorts live signals. today is YYYY-MM-DD.
// Signals with ValidThrough < today are excluded. Duplicate signal IDs are deduplicated.
func Rank(signals []entitygraph.Signal, today string) []Ranked {
	seen := map[string]bool{}
	var out []Ranked
	for _, s := range signals {
		if s.ValidThrough != "" && s.ValidThrough < today {
			continue
		}
		if seen[s.SignalID] {
			continue
		}
		seen[s.SignalID] = true

		rank := severityWeight(s.Severity) * s.Confidence
		if s.DetectedAt != "" {
			if t, err := time.Parse("2006-01-02", s.DetectedAt); err == nil {
				days := time.Since(t).Hours() / 24
				if days > 0 {
					rank *= 1.0 / (1.0 + days*0.02)
				}
			}
		}

		out = append(out, Ranked{
			Signal:      s,
			Rank:        rank,
			Headline:    GenerateHeadline(s),
			Kicker:      buildKicker(s),
			KickerClass: kickerClass(s),
			Byline:      "By the Entity-Graph Desk",
			Dateline:    buildDateline(s),
			Section:     SectionFor(s.Type),
			Deck:        s.Interpretation,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rank > out[j].Rank })
	return out
}

// GenerateHeadline produces a news-style headline for a signal.
// Degrades gracefully when fields like Entity or Metadata are missing.
func GenerateHeadline(s entitygraph.Signal) string {
	entity := s.Entity
	ticker := strings.ToUpper(s.Ticker)

	switch s.Type {
	case entitygraph.SignalNominationRejection:
		if entity != "" {
			return fmt.Sprintf("%s fails to win majority at %s", entity, ticker)
		}
		return fmt.Sprintf("Director fails to win majority at %s", ticker)

	case entitygraph.SignalActivistRisk:
		return fmt.Sprintf("%s shows activist setup: entrenchment meets friction", ticker)

	case entitygraph.SignalDirectorFriction:
		opposition := fmt.Sprintf("%.0f%%", (1-s.Score)*100)
		if entity != "" {
			return fmt.Sprintf("%s draws %s opposition at %s", entity, opposition, ticker)
		}
		return fmt.Sprintf("Director draws %s opposition at %s", opposition, ticker)

	case entitygraph.SignalGovernanceEntrenchment:
		return fmt.Sprintf("%s board defense blocks majority-backed proposal", ticker)

	case entitygraph.SignalDirectorDecay:
		if entity != "" {
			return fmt.Sprintf("Support for %s keeps sliding at %s", entity, ticker)
		}
		return fmt.Sprintf("Director support eroding at %s", ticker)

	case entitygraph.SignalAuditorChange:
		if newAud := s.Metadata["new_auditor"]; newAud != "" {
			return fmt.Sprintf("%s switches auditors to %s", ticker, newAud)
		}
		return fmt.Sprintf("%s auditor change detected", ticker)

	case entitygraph.SignalCompensationConcern:
		return fmt.Sprintf("Investors push back on %s executive pay", ticker)

	case entitygraph.SignalBrokerNonVoteAnomaly:
		return fmt.Sprintf("Unusual broker non-vote levels at %s", ticker)

	case entitygraph.SignalAbstentionSpike:
		return fmt.Sprintf("Abstentions spike on %s proposal", ticker)

	case entitygraph.SignalFamilyControl:
		return fmt.Sprintf("Founder-family grip persists on %s board", ticker)

	case entitygraph.SignalHighTrustDirector:
		if entity != "" {
			return fmt.Sprintf("%s re-elected with strong backing at %s", entity, ticker)
		}
		return fmt.Sprintf("Director wins strong backing at %s", ticker)

	case entitygraph.SignalDirectorLink:
		if entity != "" {
			return fmt.Sprintf("%s's friction spans multiple boards", entity)
		}
		return fmt.Sprintf("Friction director links %s to broader network", ticker)

	case entitygraph.SignalGovernanceHealth:
		return fmt.Sprintf("%s governance health: %s", ticker, scoreGrade(s.Score))

	default:
		return fmt.Sprintf("%s: %s signal detected", ticker, string(s.Type))
	}
}

// severityWeight converts severity to a numeric ranking multiplier.
func severityWeight(s entitygraph.Severity) float64 {
	switch s {
	case entitygraph.SeverityCritical:
		return 4.0
	case entitygraph.SeverityHigh:
		return 3.0
	case entitygraph.SeverityMedium:
		return 2.0
	default:
		return 1.0
	}
}

func buildKicker(s entitygraph.Signal) string {
	section := strings.ToUpper(SectionFor(s.Type))
	switch s.Severity {
	case entitygraph.SeverityCritical:
		return "CRITICAL · " + section
	case entitygraph.SeverityHigh:
		return "HIGH · " + section
	default:
		return section
	}
}

func kickerClass(s entitygraph.Signal) string {
	switch s.Severity {
	case entitygraph.SeverityCritical:
		return "kicker-critical"
	case entitygraph.SeverityHigh:
		return "kicker-high"
	case entitygraph.SeverityMedium:
		return "kicker-medium"
	default:
		return "kicker-low"
	}
}

func buildDateline(s entitygraph.Signal) string {
	ticker := strings.ToUpper(s.Ticker)
	date := s.DetectedAt
	if date == "" {
		return ticker + " —"
	}
	if t, err := time.Parse("2006-01-02", date); err == nil {
		date = t.Format("2 January 2006")
	}
	return fmt.Sprintf("%s — %s.", ticker, date)
}

func scoreGrade(score float64) string {
	switch {
	case score >= 0.85:
		return "A (excellent)"
	case score >= 0.70:
		return "B (good)"
	case score >= 0.55:
		return "C (moderate)"
	case score >= 0.40:
		return "D (concerning)"
	default:
		return "F (severe)"
	}
}
