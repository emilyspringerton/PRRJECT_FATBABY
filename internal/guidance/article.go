package guidance

import (
	"fmt"
	"strings"
	"time"
)

const minConfidence = 0.60

// Generate builds a publishable Article from GuidanceData.
// Returns (Article{}, false) when data doesn't meet publish criteria.
func Generate(g *GuidanceData, now time.Time) (Article, bool) {
	if !g.HasGuidance || g.Confidence < minConfidence {
		return Article{}, false
	}
	issuer := g.Issuer
	if issuer == "" {
		issuer = g.Ticker
	}
	if issuer == "" {
		return Article{}, false
	}

	headline := buildHeadline(issuer, g)
	if headline == "" {
		return Article{}, false
	}
	body := buildBody(g)
	id := fmt.Sprintf("guidance-%s-%s", strings.ToLower(g.Ticker), now.UTC().Format("20060102T150405Z"))

	return Article{
		ID:             id,
		SourceIdentity: g.SourceIdentity,
		Ticker:         g.Ticker,
		Issuer:         issuer,
		Headline:       headline,
		Body:           body,
		Action:         g.Action,
		Metric:         g.Metric,
		Period:         g.Period,
		EPSLow:         g.EPSLow,
		EPSHigh:        g.EPSHigh,
		RevenueLow:     g.RevenueLow,
		RevenueHigh:    g.RevenueHigh,
		Confidence:     g.Confidence,
		PublishAt:      now.UTC().Format(time.RFC3339),
	}, true
}

func buildHeadline(issuer string, g *GuidanceData) string {
	period := formatPeriod(g.Period)
	action := formatAction(g.Action)

	switch g.Metric {
	case MetricBoth:
		return fmt.Sprintf("%s %s %s guidance on EPS and revenue", issuer, action, period)
	case MetricEPS:
		if g.EPSLow != nil && g.EPSHigh != nil {
			if *g.EPSLow == *g.EPSHigh {
				return fmt.Sprintf("%s %s %s EPS guidance to $%.2f", issuer, action, period, *g.EPSLow)
			}
			return fmt.Sprintf("%s %s %s EPS guidance to $%.2f–$%.2f", issuer, action, period, *g.EPSLow, *g.EPSHigh)
		}
		return fmt.Sprintf("%s %s %s EPS guidance", issuer, action, period)
	case MetricRevenue:
		if g.RevenueLow != nil && g.RevenueHigh != nil {
			lo, hi, unit := *g.RevenueLow, *g.RevenueHigh, g.RevenueUnit
			label := "M"
			if unit == 1000 {
				lo /= 1000
				hi /= 1000
				label = "B"
			}
			return fmt.Sprintf("%s %s %s revenue guidance to $%.1f–$%.1f%s", issuer, action, period, lo, hi, label)
		}
		return fmt.Sprintf("%s %s %s revenue guidance", issuer, action, period)
	default:
		if g.Action == ActionWithdrawn {
			return fmt.Sprintf("%s withdraws %s guidance", issuer, period)
		}
	}
	return ""
}

func buildBody(g *GuidanceData) string {
	var sb strings.Builder
	period := formatPeriod(g.Period)
	action := formatAction(g.Action)
	fmt.Fprintf(&sb, "The company %s its %s guidance", action, period)

	if g.EPSLow != nil && g.EPSHigh != nil {
		if *g.EPSLow == *g.EPSHigh {
			fmt.Fprintf(&sb, ". EPS guidance: $%.2f per diluted share", *g.EPSLow)
		} else {
			fmt.Fprintf(&sb, ". EPS guidance: $%.2f to $%.2f per diluted share", *g.EPSLow, *g.EPSHigh)
		}
	}
	if g.RevenueLow != nil && g.RevenueHigh != nil {
		lo, hi, unit := *g.RevenueLow, *g.RevenueHigh, g.RevenueUnit
		label := "M"
		if unit == 1000 {
			lo /= 1000
			hi /= 1000
			label = "B"
		}
		fmt.Fprintf(&sb, ". Revenue guidance: $%.1f%s to $%.1f%s", lo, label, hi, label)
	}
	fmt.Fprintf(&sb, ". Extraction confidence: %.0f%%.", g.Confidence*100)
	return sb.String()
}

func formatPeriod(p Period) string {
	q := p.FiscalQuarter
	y := p.FiscalYear
	switch {
	case q != "" && y > 0:
		return fmt.Sprintf("%s %d", q, y)
	case q != "":
		return q
	case y > 0:
		return fmt.Sprintf("FY%d", y)
	default:
		return "full-year"
	}
}

func formatAction(a Action) string {
	switch a {
	case ActionRaised:
		return "raises"
	case ActionLowered:
		return "lowers"
	case ActionMaintained:
		return "maintains"
	case ActionInitiated:
		return "initiates"
	case ActionWithdrawn:
		return "withdraws"
	default:
		return "updates"
	}
}
