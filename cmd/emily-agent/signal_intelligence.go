package main

// Signal intelligence tools: let Emily read, summarise, and explain the
// governance signals produced by the entity-graph pipeline.
//
// Three tools are registered:
//   fatbaby_signal_summary   — high-level dashboard of all signals
//   fatbaby_query_signals    — filtered signal lookup
//   fatbaby_entity_graph     — director / company graph lookup

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── local types (mirror entitygraph structs; no import needed) ──────────────

type rawSignal struct {
	SignalID       string            `json:"signal_id"`
	Type           string            `json:"type"`
	Ticker         string            `json:"ticker"`
	Entity         string            `json:"entity,omitempty"`
	Severity       string            `json:"severity"`
	Confidence     float64           `json:"confidence"`
	Score          float64           `json:"score"`
	DetectedAt     string            `json:"detected_at"`
	ValidThrough   string            `json:"valid_through"`
	Interpretation string            `json:"interpretation"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type rawFiling struct {
	Ticker         string  `json:"ticker"`
	Form           string  `json:"form"`
	FilingDate     string  `json:"filing_date"`
	ApprovalPct    float64 `json:"approval_pct,omitempty"`
	ForVotes       int64   `json:"for_votes,omitempty"`
	AgainstVotes   int64   `json:"against_votes,omitempty"`
	BrokerNonVotes int64   `json:"broker_non_votes,omitempty"`
}

type rawPersonNode struct {
	CanonicalID     string      `json:"canonical_id"`
	Name            string      `json:"name"`
	Type            string      `json:"type"`
	FirstAppearance string      `json:"first_appearance"`
	LastAppearance  string      `json:"last_appearance"`
	FilingCount     int         `json:"filing_count"`
	Filings         []rawFiling `json:"filings"`
}

type rawEdge struct {
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	EdgeType  string   `json:"edge_type"`
	Companies []string `json:"companies"`
	Strength  int      `json:"strength"`
}

type rawAuditor struct {
	Ticker     string `json:"ticker"`
	Auditor    string `json:"auditor"`
	FilingDate string `json:"filing_date"`
}

// ── NDJSON loaders ──────────────────────────────────────────────────────────

func loadSignalNDJSON(path string) ([]rawSignal, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []rawSignal
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var s rawSignal
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			out = append(out, s)
		}
	}
	return out, sc.Err()
}

func loadNodeNDJSON(path string) map[string]*rawPersonNode {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]*rawPersonNode{}
	}
	if err != nil {
		return map[string]*rawPersonNode{}
	}
	defer f.Close()
	nodes := map[string]*rawPersonNode{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var n rawPersonNode
		if json.Unmarshal(sc.Bytes(), &n) == nil {
			nodes[n.CanonicalID] = &n // last entry per canonical_id wins
		}
	}
	return nodes
}

func loadEdgeNDJSON(path string) []rawEdge {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return nil
	}
	defer f.Close()
	seen := map[string]*rawEdge{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var e rawEdge
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			key := e.Source + "|" + e.Target
			if e.Source > e.Target {
				key = e.Target + "|" + e.Source
			}
			seen[key] = &e
		}
	}
	var out []rawEdge
	for _, e := range seen {
		out = append(out, *e)
	}
	return out
}

func loadAuditorNDJSON(path string) map[string]*rawAuditor {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]*rawAuditor{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var a rawAuditor
		if json.Unmarshal(sc.Bytes(), &a) == nil {
			out[a.Ticker] = &a
		}
	}
	return out
}

// ── severity ordering ───────────────────────────────────────────────────────

var severityRank = map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1}

func rankSeverity(s string) int { return severityRank[s] }

// ── tool registration ───────────────────────────────────────────────────────

func registerSignalIntelligenceTools(d *ToolDispatcher, fatbabyRoot string) {
	graphDir := filepath.Join(fatbabyRoot, "var", "entity-graph")

	// ── fatbaby_signal_summary ──────────────────────────────────────────────
	d.Register(ToolDef{
		Name:        "fatbaby_signal_summary",
		Description: "Return a high-level dashboard of all entity-graph governance signals: counts by type and severity, tickers with the most activity, and the most recent high/critical alerts with their full interpretations. Call this first when a user asks about overall risk or what signals exist.",
		Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}},
	}, func(args map[string]any) (string, error) {
		signals, err := loadSignalNDJSON(filepath.Join(graphDir, "signals.ndjson"))
		if err != nil {
			return "", err
		}
		if len(signals) == 0 {
			return "No signals yet — the entity-graph process has not run, or no 8-K filings with Item 5.07 have been processed.", nil
		}

		byType := map[string]int{}
		bySeverity := map[string]int{}
		byTicker := map[string]int{}
		for _, s := range signals {
			byType[s.Type]++
			bySeverity[s.Severity]++
			if s.Ticker != "" {
				byTicker[s.Ticker]++
		}
	}

		// Sort tickers by signal count.
		type kv struct{ k string; v int }
		var tickerList []kv
		for k, v := range byTicker {
			tickerList = append(tickerList, kv{k, v})
		}
		sort.Slice(tickerList, func(i, j int) bool { return tickerList[i].v > tickerList[j].v })

		// Find most recent high/critical signals.
		var alert []rawSignal
		for _, s := range signals {
			if rankSeverity(s.Severity) >= rankSeverity("high") {
				alert = append(alert, s)
			}
		}
		sort.Slice(alert, func(i, j int) bool { return alert[i].DetectedAt > alert[j].DetectedAt })
		if len(alert) > 5 {
			alert = alert[:5]
		}

		result := map[string]any{
			"total_signals":     len(signals),
			"by_type":           byType,
			"by_severity":       bySeverity,
			"top_tickers":       tickerList,
			"recent_high_critical": alert,
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil
	})

	// ── fatbaby_query_signals ───────────────────────────────────────────────
	d.Register(ToolDef{
		Name:        "fatbaby_query_signals",
		Description: "Search entity-graph governance signals with optional filters. Returns matching signals with their full interpretation text. Use this to answer specific questions like 'what signals do we have for SCHW?' or 'show me all activist risk signals' or 'any critical alerts in the last 30 days?'",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker":      {Type: "string", Description: "Filter by company ticker (e.g. 'SCHW'). Empty = all tickers."},
				"signal_type": {Type: "string", Description: "Filter by type: director_friction | governance_entrenchment | activist_risk | director_link | family_control | high_trust_director | director_decay | broker_nonvote_anomaly | compensation_concern | auditor_change | abstention_spike | nomination_rejection"},
				"min_severity": {Type: "string", Description: "Minimum severity to include: low | medium | high | critical. Default: low (all)."},
				"days":        {Type: "number", Description: "Only signals detected in the last N days. 0 or omitted = all time."},
				"limit":       {Type: "number", Description: "Max results to return (default 20)."},
			},
		},
	}, func(args map[string]any) (string, error) {
		signals, err := loadSignalNDJSON(filepath.Join(graphDir, "signals.ndjson"))
		if err != nil {
			return "", err
		}

		ticker, _ := args["ticker"].(string)
		sigType, _ := args["signal_type"].(string)
		minSev, _ := args["min_severity"].(string)
		days, _ := args["days"].(float64)
		limit := 20
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(math.Min(v, 100))
		}
		if minSev == "" {
			minSev = "low"
		}
		minRank := rankSeverity(minSev)

		var cutoff string
		if days > 0 {
			cutoff = time.Now().UTC().AddDate(0, 0, -int(days)).Format("2006-01-02")
		}

		var matched []rawSignal
		for _, s := range signals {
			if ticker != "" && !strings.EqualFold(s.Ticker, ticker) {
				continue
			}
			if sigType != "" && s.Type != sigType {
				continue
			}
			if rankSeverity(s.Severity) < minRank {
				continue
			}
			if cutoff != "" && s.DetectedAt < cutoff {
				continue
			}
			matched = append(matched, s)
		}

		// Sort: severity desc, then date desc.
		sort.Slice(matched, func(i, j int) bool {
			ri, rj := rankSeverity(matched[i].Severity), rankSeverity(matched[j].Severity)
			if ri != rj {
				return ri > rj
			}
			return matched[i].DetectedAt > matched[j].DetectedAt
		})
		if len(matched) > limit {
			matched = matched[:limit]
		}

		if len(matched) == 0 {
			return "No signals match the given filters.", nil
		}
		b, _ := json.MarshalIndent(matched, "", "  ")
		return fmt.Sprintf("Found %d matching signals:\n%s", len(matched), string(b)), nil
	})

	// ── fatbaby_entity_graph ────────────────────────────────────────────────
	d.Register(ToolDef{
		Name:        "fatbaby_entity_graph",
		Description: "Look up directors and companies in the entity graph. Shows approval histories, board relationships, and known auditors. Use to answer questions like 'who are the directors at SCHW?', 'what is Herringer's approval trend?', 'which directors sit on multiple boards?', or 'what auditor does JPM use?'",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker":   {Type: "string", Description: "Show all directors who have appeared in filings for this ticker."},
				"director": {Type: "string", Description: "Look up a director by name (partial, case-insensitive match). Shows all their board appearances and approval trends."},
			},
		},
	}, func(args map[string]any) (string, error) {
		ticker, _ := args["ticker"].(string)
		directorQuery, _ := args["director"].(string)

		if ticker == "" && directorQuery == "" {
			// Show graph-wide stats: node count, edge count, top connected directors.
			nodes := loadNodeNDJSON(filepath.Join(graphDir, "nodes.ndjson"))
			edges := loadEdgeNDJSON(filepath.Join(graphDir, "edges.ndjson"))
			auditors := loadAuditorNDJSON(filepath.Join(graphDir, "auditors.ndjson"))

			// Directors sorted by filing count (most cross-company first).
			type dirSum struct {
				Name        string `json:"name"`
				Companies   int    `json:"distinct_companies"`
				FilingCount int    `json:"filing_count"`
			}
			var dirs []dirSum
			for _, n := range nodes {
				tickers := map[string]bool{}
				for _, f := range n.Filings {
					tickers[f.Ticker] = true
				}
				dirs = append(dirs, dirSum{Name: n.Name, Companies: len(tickers), FilingCount: n.FilingCount})
			}
			sort.Slice(dirs, func(i, j int) bool {
				if dirs[i].Companies != dirs[j].Companies {
					return dirs[i].Companies > dirs[j].Companies
				}
				return dirs[i].FilingCount > dirs[j].FilingCount
			})
			top := dirs
			if len(top) > 10 {
				top = top[:10]
			}

			var auditorList []map[string]string
			for _, a := range auditors {
				auditorList = append(auditorList, map[string]string{"ticker": a.Ticker, "auditor": a.Auditor, "as_of": a.FilingDate})
			}

			b, _ := json.MarshalIndent(map[string]any{
				"total_directors": len(nodes),
				"total_edges":     len(edges),
				"top_directors":   top,
				"known_auditors":  auditorList,
			}, "", "  ")
			return string(b), nil
		}

		nodes := loadNodeNDJSON(filepath.Join(graphDir, "nodes.ndjson"))
		edges := loadEdgeNDJSON(filepath.Join(graphDir, "edges.ndjson"))

		// Filter nodes by ticker or director name.
		var matched []*rawPersonNode
		for _, n := range nodes {
			if ticker != "" {
				for _, f := range n.Filings {
					if strings.EqualFold(f.Ticker, ticker) {
						matched = append(matched, n)
						break
					}
				}
			} else if directorQuery != "" {
				if strings.Contains(strings.ToLower(n.Name), strings.ToLower(directorQuery)) {
					matched = append(matched, n)
				}
			}
		}
		if len(matched) == 0 {
			return "No matching directors found in the entity graph. The entity-graph process may not have run yet.", nil
		}

		// Sort by most recent filing.
		sort.Slice(matched, func(i, j int) bool {
			return matched[i].LastAppearance > matched[j].LastAppearance
		})

		// Enrich each node with approval trend and co-board partners.
		type enrichedNode struct {
			*rawPersonNode
			ApprovalTrend  string   `json:"approval_trend,omitempty"`
			CoBoardPartners []string `json:"co_board_partners,omitempty"`
		}
		var enriched []enrichedNode
		for _, n := range matched {
			// Sort filings by date, compute approval trend.
			filings := append([]rawFiling{}, n.Filings...)
			sort.Slice(filings, func(i, j int) bool { return filings[i].FilingDate < filings[j].FilingDate })
			n.Filings = filings

			trend := ""
			if len(filings) >= 2 {
				first := filings[0].ApprovalPct
				last := filings[len(filings)-1].ApprovalPct
				delta := last - first
				switch {
				case delta < -0.03:
					trend = fmt.Sprintf("declining (%.1f%% → %.1f%%, Δ%.1f pp)", first*100, last*100, delta*100)
				case delta > 0.03:
					trend = fmt.Sprintf("improving (%.1f%% → %.1f%%, Δ+%.1f pp)", first*100, last*100, delta*100)
				default:
					trend = fmt.Sprintf("stable (~%.1f%%)", last*100)
				}
			} else if len(filings) == 1 && filings[0].ApprovalPct > 0 {
				trend = fmt.Sprintf("single data point: %.1f%%", filings[0].ApprovalPct*100)
			}

			// Find co-board partners from edges.
			var partners []string
			for _, e := range edges {
				match := ""
				if e.Source == n.CanonicalID {
					match = e.Target
				} else if e.Target == n.CanonicalID {
					match = e.Source
				}
				if match != "" {
					if partner, ok := nodes[match]; ok {
						partners = append(partners, partner.Name)
					}
				}
			}
			sort.Strings(partners)

			enriched = append(enriched, enrichedNode{rawPersonNode: n, ApprovalTrend: trend, CoBoardPartners: partners})
		}

		b, _ := json.MarshalIndent(enriched, "", "  ")
		return fmt.Sprintf("Found %d director(s):\n%s", len(enriched), string(b)), nil
	})
}
