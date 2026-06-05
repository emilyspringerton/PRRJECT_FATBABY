package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

func absStore(root, name string) string { return filepath.Join(root, "var", name) }

func openStore(root, name string) (*eventstore.FileStore, string, error) {
	dir := absStore(root, name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, "store not initialised — has the process been run?", nil
	}
	fs, err := eventstore.NewFileStore(dir)
	if err != nil {
		return nil, "", err
	}
	return fs, "", nil
}

func slugify(s string, max int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteRune('-')
		}
		if b.Len() >= max {
			break
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func registerJonTools(d *ToolDispatcher, root string) {
	registerPipelineTools(d, root)
	registerSetupTools(d, root)
	registerMarketDataStubs(d)
}

// ── FatBaby pipeline readers ─────────────────────────────────────────────────

func registerPipelineTools(d *ToolDispatcher, root string) {

	// Governance signals from entity-graph
	d.Register(ToolDef{
		Name:        "jon_governance_signals",
		Description: "Read all 31 governance signal types from the entity-graph pipeline. Covers: activist risk, post-failure activist prediction, director friction/decay/tenure, CFO/leadership departure, dividend cut/raise/special, buyback authorization/suspension, late filing, insider buy/sell cluster, board decay concern, governance health index, health trend (deteriorating/improving), peer underperformance, and more. Use to find structural or catalyst divergences between governance risk and price.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker": {Type: "string", Description: "Filter to a specific ticker (optional, case-insensitive)"},
				"limit":  {Type: "number", Description: "Max signals to return (default 30)"},
			},
		},
	}, func(args map[string]any) (string, error) {
		sigPath := filepath.Join(root, "var", "entity-graph", "signals.ndjson")
		f, err := os.Open(sigPath)
		if os.IsNotExist(err) {
			return `{"error":"signals.ndjson not found — entity-graph has not run yet"}`, nil
		}
		if err != nil {
			return "", fmt.Errorf("open signals: %w", err)
		}
		defer f.Close()

		filterTicker := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", args["ticker"])))
		if filterTicker == "<NIL>" {
			filterTicker = ""
		}
		limit := 30
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		type sig struct {
			SignalID       string  `json:"signal_id"`
			Type           string  `json:"type"`
			Ticker         string  `json:"ticker"`
			Entity         string  `json:"entity,omitempty"`
			Severity       string  `json:"severity"`
			Score          float64 `json:"score"`
			Confidence     float64 `json:"confidence"`
			FilingDate     string  `json:"filing_date"`
			Interpretation string  `json:"interpretation"`
		}

		type tickerSum struct {
			Total    int            `json:"total"`
			Severity map[string]int `json:"by_severity"`
			Latest   string         `json:"latest_filing_date"`
			Health   float64        `json:"governance_health_index,omitempty"`
		}

		sums := map[string]*tickerSum{}
		var notable []sig
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			// Try wrapped format {"signal":{...}} then flat.
			var wrapped struct {
				Signal sig `json:"signal"`
			}
			var s sig
			if json.Unmarshal(sc.Bytes(), &wrapped) == nil && wrapped.Signal.SignalID != "" {
				s = wrapped.Signal
			} else if json.Unmarshal(sc.Bytes(), &s) == nil && s.SignalID != "" {
				// flat
			} else {
				continue
			}
			t := strings.ToUpper(s.Ticker)
			if filterTicker != "" && t != filterTicker {
				continue
			}
			if _, ok := sums[t]; !ok {
				sums[t] = &tickerSum{Severity: map[string]int{}}
			}
			ts := sums[t]
			ts.Total++
			ts.Severity[s.Severity]++
			if s.FilingDate > ts.Latest {
				ts.Latest = s.FilingDate
			}
			if s.Type == "governance_health_index" {
				ts.Health = s.Score
			}
			if (s.Severity == "high" || s.Severity == "critical") && len(notable) < limit {
				notable = append(notable, s)
			}
		}

		b, _ := json.MarshalIndent(map[string]any{
			"ticker_summaries": sums,
			"notable_signals":  notable,
			"total_tickers":    len(sums),
		}, "", "  ")
		return string(b), nil
	})

	// EPS oracle status
	d.Register(ToolDef{
		Name:        "jon_eps_status",
		Description: "Read EPS oracle status: precision, confirmed/contradicted cases, and recent articles. Use to assess EPS surprise divergences.",
		Parameters:  ToolParameters{Type: "object", Properties: map[string]ToolPropSchema{}},
	}, func(args map[string]any) (string, error) {
		epsDir := filepath.Join(root, "var", "eps")

		type oracleCase struct {
			Ticker  string `json:"ticker"`
			Quarter string `json:"quarter"`
			EPS     string `json:"eps"`
			Verdict string `json:"verdict"`
		}

		var cases []oracleCase
		confirmed, contradicted, pending := 0, 0, 0
		if f, err := os.Open(filepath.Join(epsDir, "oracle.ndjson")); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() {
				var c oracleCase
				if json.Unmarshal(sc.Bytes(), &c) == nil {
					cases = append(cases, c)
					switch c.Verdict {
					case "confirmed":
						confirmed++
					case "contradicts":
						contradicted++
					default:
						pending++
					}
				}
			}
			f.Close()
		}
		precision := 0.0
		resolved := confirmed + contradicted
		if resolved > 0 {
			precision = float64(confirmed) / float64(resolved)
		}

		articleCount := 0
		if f, err := os.Open(filepath.Join(epsDir, "articles.ndjson")); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if len(strings.TrimSpace(sc.Text())) > 0 {
					articleCount++
				}
			}
			f.Close()
		}

		// Return last 5 cases for context.
		recent := cases
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}
		b, _ := json.MarshalIndent(map[string]any{
			"oracle_total":       len(cases),
			"confirmed":          confirmed,
			"contradicted":       contradicted,
			"pending":            pending,
			"precision":          precision,
			"articles_published": articleCount,
			"recent_cases":       recent,
		}, "", "  ")
		return string(b), nil
	})

	// Read press releases
	d.Register(ToolDef{
		Name:        "jon_read_press_releases",
		Description: "Read recent press release bodies from the prwatch pipeline for a ticker. Use to check PR narrative vs price action.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker": {Type: "string", Description: "Ticker symbol (optional — omit for all)"},
				"limit":  {Type: "number", Description: "Max press releases to return (default 5)"},
			},
		},
	}, func(args map[string]any) (string, error) {
		filterTicker := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", args["ticker"])))
		if filterTicker == "<NIL>" {
			filterTicker = ""
		}
		limit := 5
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		store, msg, err := openStore(root, "prwatch-body")
		if msg != "" {
			return msg, nil
		}
		if err != nil {
			return "", err
		}
		defer store.Close()

		latest, _ := store.LatestSequence(context.Background())
		recs, _ := store.ReadFrom(context.Background(), 1, int(latest))

		type prBody struct {
			Ticker    string `json:"ticker"`
			Headline  string `json:"headline"`
			Body      string `json:"body"`
			FetchedAt string `json:"fetched_at"`
		}
		var results []prBody
		for i := len(recs) - 1; i >= 0 && len(results) < limit; i-- {
			r := recs[i]
			if r.Event.Type != "pr_body_fetched" {
				continue
			}
			var pr prBody
			if json.Unmarshal(r.Event.Data, &pr) != nil {
				continue
			}
			if filterTicker != "" && strings.ToUpper(pr.Ticker) != filterTicker {
				continue
			}
			if len(pr.Body) > 1000 {
				pr.Body = pr.Body[:1000] + "…"
			}
			results = append(results, pr)
		}
		b, _ := json.MarshalIndent(results, "", "  ")
		return string(b), nil
	})

	// Read 8-K filings
	d.Register(ToolDef{
		Name:        "jon_read_filings",
		Description: "Read 8-K filing text for a ticker from the secwatch pipeline. Returns the most recent source documents. Use to understand material events for divergence analysis.",
		Parameters: ToolParameters{
			Type:     "object",
			Properties: map[string]ToolPropSchema{
				"ticker": {Type: "string", Description: "Ticker symbol"},
				"limit":  {Type: "number", Description: "Max filings to return (default 3)"},
			},
			Required: []string{"ticker"},
		},
	}, func(args map[string]any) (string, error) {
		ticker := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", args["ticker"])))
		limit := 3
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		store, msg, err := openStore(root, "secwatch")
		if msg != "" {
			return msg, nil
		}
		if err != nil {
			return "", err
		}
		defer store.Close()

		latest, _ := store.LatestSequence(context.Background())
		recs, _ := store.ReadFrom(context.Background(), 1, int(latest))

		type doc struct {
			Ticker     string `json:"ticker"`
			Form       string `json:"form"`
			FilingDate string `json:"filing_date"`
			Summary    string `json:"summary,omitempty"`
			Text       string `json:"text,omitempty"`
		}
		var results []doc
		for i := len(recs) - 1; i >= 0 && len(results) < limit; i-- {
			r := recs[i]
			if r.Event.Type != "source_document_persisted" {
				continue
			}
			var d doc
			if json.Unmarshal(r.Event.Data, &d) != nil {
				continue
			}
			if strings.ToUpper(d.Ticker) != ticker {
				continue
			}
			if len(d.Text) > 1500 {
				d.Text = d.Text[:1500] + "…"
			}
			results = append(results, d)
		}
		if len(results) == 0 {
			return fmt.Sprintf(`{"message":"no source documents found for %s — run secwatch and processor first"}`, ticker), nil
		}
		b, _ := json.MarshalIndent(results, "", "  ")
		return string(b), nil
	})

	// Read forward guidance
	d.Register(ToolDef{
		Name:        "jon_read_guidance",
		Description: "Read forward guidance signals (raises/lowers/maintains) from the guidance-watcher pipeline. Use to find guidance vs price divergences.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker": {Type: "string", Description: "Filter to a specific ticker (optional)"},
				"limit":  {Type: "number", Description: "Max guidance records to return (default 10)"},
			},
		},
	}, func(args map[string]any) (string, error) {
		filterTicker := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", args["ticker"])))
		if filterTicker == "<NIL>" {
			filterTicker = ""
		}
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		guidanceDir := filepath.Join(root, "var", "guidance")
		entries, err := os.ReadDir(guidanceDir)
		if os.IsNotExist(err) {
			return `{"message":"guidance store not found — has guidance-watcher run?"}`, nil
		}
		if err != nil {
			return "", fmt.Errorf("read guidance dir: %w", err)
		}

		type guidanceRec struct {
			Ticker     string `json:"ticker"`
			Direction  string `json:"direction"`
			Period     string `json:"period,omitempty"`
			Headline   string `json:"headline,omitempty"`
			PublishedAt string `json:"published_at,omitempty"`
		}
		var results []guidanceRec

		// Scan newest-first (sort entries descending).
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
		for _, e := range entries {
			if len(results) >= limit {
				break
			}
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".ndjson") {
				continue
			}
			f, err := os.Open(filepath.Join(guidanceDir, e.Name()))
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() && len(results) < limit {
				var r guidanceRec
				if json.Unmarshal(sc.Bytes(), &r) == nil {
					if filterTicker == "" || strings.ToUpper(r.Ticker) == filterTicker {
						results = append(results, r)
					}
				}
			}
			f.Close()
		}

		b, _ := json.MarshalIndent(map[string]any{
			"guidance_records": results,
			"total":            len(results),
		}, "", "  ")
		return string(b), nil
	})

	// Signal accuracy reports — calibrate conviction by signal type
	d.Register(ToolDef{
		Name:        "jon_accuracy_report",
		Description: "Read live signal prediction accuracy from the entity-graph accuracy tracker. Returns precision (confirmed / resolved), confirmed/refuted/pending counts per signal type. Use to calibrate conviction: a signal type with high precision (>0.70) deserves higher conviction; a type with low precision or mostly pending records needs more caution.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"signal_type": {Type: "string", Description: "Filter to a specific signal type (optional, e.g. 'activist_risk', 'director_decay')"},
			},
		},
	}, func(args map[string]any) (string, error) {
		accPath := filepath.Join(root, "var", "entity-graph", "accuracy.ndjson")
		f, err := os.Open(accPath)
		if os.IsNotExist(err) {
			return `{"error":"accuracy.ndjson not found — entity-graph accuracy tracking has not run yet"}`, nil
		}
		if err != nil {
			return "", fmt.Errorf("open accuracy: %w", err)
		}
		defer f.Close()

		filterType := strings.TrimSpace(fmt.Sprintf("%v", args["signal_type"]))
		if filterType == "<nil>" || filterType == "<NIL>" {
			filterType = ""
		}

		type record struct {
			SignalID     string  `json:"signal_id"`
			Ticker       string  `json:"ticker"`
			SignalType   string  `json:"signal_type"`
			PredictedAt  string  `json:"predicted_at"`
			ValidThrough string  `json:"valid_through"`
			Outcome      string  `json:"outcome"`
			EvidenceDate string  `json:"evidence_date,omitempty"`
			EvidenceType string  `json:"evidence_type,omitempty"`
			Notes        string  `json:"notes,omitempty"`
		}
		type report struct {
			SignalType string  `json:"signal_type"`
			Total      int     `json:"total"`
			Confirmed  int     `json:"confirmed"`
			Refuted    int     `json:"refuted"`
			Pending    int     `json:"pending"`
			Precision  float64 `json:"precision"`
		}

		byType := map[string]*report{}
		var recentConfirmed []record

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var r record
			if json.Unmarshal(sc.Bytes(), &r) != nil || r.SignalID == "" {
				continue
			}
			if filterType != "" && r.SignalType != filterType {
				continue
			}
			rpt, ok := byType[r.SignalType]
			if !ok {
				rpt = &report{SignalType: r.SignalType}
				byType[r.SignalType] = rpt
			}
			rpt.Total++
			switch r.Outcome {
			case "confirmed":
				rpt.Confirmed++
				if len(recentConfirmed) < 5 {
					recentConfirmed = append(recentConfirmed, r)
				}
			case "refuted":
				rpt.Refuted++
			default:
				rpt.Pending++
			}
		}

		var reports []report
		for _, rpt := range byType {
			if rpt.Confirmed+rpt.Refuted > 0 {
				rpt.Precision = math.Round(float64(rpt.Confirmed)/float64(rpt.Confirmed+rpt.Refuted)*1000) / 1000
			}
			reports = append(reports, *rpt)
		}
		sort.Slice(reports, func(i, j int) bool {
			return reports[i].Precision > reports[j].Precision
		})

		b, _ := json.MarshalIndent(map[string]any{
			"accuracy_by_signal_type": reports,
			"recent_confirmed":        recentConfirmed,
			"total_signal_types":      len(reports),
			"note":                    "precision = confirmed / (confirmed + refuted); pending records not yet resolved",
		}, "", "  ")
		return string(b), nil
	})
}

// ── Setup audit log ───────────────────────────────────────────────────────────

func registerSetupTools(d *ToolDispatcher, root string) {

	// Log a trade setup
	d.Register(ToolDef{
		Name:        "jon_log_setup",
		Description: "Log a structured trade setup to the Jon audit trail (var/jon-setups/setups.ndjson). Call this whenever you surface a setup — every setup must be logged.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker":        {Type: "string", Description: "Ticker symbol"},
				"direction":     {Type: "string", Description: "long|short"},
				"instrument":    {Type: "string", Description: "e.g. call spread, put, strangle"},
				"thesis":        {Type: "string", Description: "One paragraph — why this trade exists"},
				"signal_stack":  {Type: "string", Description: "Data points aligned — be specific about which fatbaby signals"},
				"entry":         {Type: "string", Description: "Price level or condition"},
				"structure":     {Type: "string", Description: "Options construction: strikes, expiry, cost"},
				"max_loss":      {Type: "string", Description: "Defined maximum loss"},
				"max_gain":      {Type: "string", Description: "Defined maximum gain"},
				"breakeven":     {Type: "string", Description: "Price at expiry"},
				"exit_right":    {Type: "string", Description: "Take profit condition"},
				"exit_wrong":    {Type: "string", Description: "Stop/rotate condition"},
				"theta_profile": {Type: "string", Description: "How time decay affects this position"},
				"iv_context":    {Type: "string", Description: "IV cheap or expensive, crush risk"},
				"conviction":    {Type: "string", Description: "Low|Medium|High"},
				"notes":         {Type: "string", Description: "Anything the human needs to know"},
			},
			Required: []string{"ticker", "direction", "thesis", "conviction"},
		},
	}, func(args map[string]any) (string, error) {
		dir := filepath.Join(root, "var", "jon-setups")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create setups dir: %w", err)
		}
		now := time.Now().UTC()
		setup := map[string]any{
			"id":            "setup-" + now.Format("20060102T150405Z"),
			"logged_at":     now.Format(time.RFC3339),
			"status":        "surfaced",
		}
		for k, v := range args {
			setup[k] = v
		}
		b, err := json.Marshal(setup)
		if err != nil {
			return "", err
		}
		f, err := os.OpenFile(filepath.Join(dir, "setups.ndjson"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, err := f.Write(append(b, '\n')); err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"logged":"ok","id":"%s"}`, setup["id"]), nil
	})

	// Read logged setups
	d.Register(ToolDef{
		Name:        "jon_read_setups",
		Description: "Read logged trade setups from the Jon audit trail. Use to review open or recent setups.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker": {Type: "string", Description: "Filter by ticker (optional)"},
				"status": {Type: "string", Description: "Filter by status: surfaced|active|closed (optional)"},
				"limit":  {Type: "number", Description: "Max setups to return (default 10)"},
			},
		},
	}, func(args map[string]any) (string, error) {
		filterTicker := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", args["ticker"])))
		if filterTicker == "<NIL>" {
			filterTicker = ""
		}
		filterStatus, _ := args["status"].(string)
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}

		p := filepath.Join(root, "var", "jon-setups", "setups.ndjson")
		f, err := os.Open(p)
		if os.IsNotExist(err) {
			return `{"setups":[],"message":"no setups logged yet"}`, nil
		}
		if err != nil {
			return "", err
		}
		defer f.Close()

		var all []map[string]any
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var s map[string]any
			if json.Unmarshal(sc.Bytes(), &s) == nil {
				all = append(all, s)
			}
		}

		// Newest first, filtered.
		var results []map[string]any
		for i := len(all) - 1; i >= 0 && len(results) < limit; i-- {
			s := all[i]
			if filterTicker != "" {
				if t, ok := s["ticker"].(string); !ok || strings.ToUpper(t) != filterTicker {
					continue
				}
			}
			if filterStatus != "" {
				if st, ok := s["status"].(string); !ok || st != filterStatus {
					continue
				}
			}
			results = append(results, s)
		}
		b, _ := json.MarshalIndent(map[string]any{"setups": results, "total_logged": len(all)}, "", "  ")
		return string(b), nil
	})

	// Publish to Emily Prime
	d.Register(ToolDef{
		Name:        "jon_publish_to_prime",
		Description: "Surface a high-conviction trade setup to Emily Prime's integration layer. Emily Prime will triage it and may escalate to the CEO. Use for High conviction setups only.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker":       {Type: "string", Description: "Ticker symbol"},
				"direction":    {Type: "string", Description: "long|short"},
				"conviction":   {Type: "string", Description: "Medium|High"},
				"thesis":       {Type: "string", Description: "Why this trade exists"},
				"signal_stack": {Type: "string", Description: "What data points are aligned"},
				"setup_id":     {Type: "string", Description: "ID from jon_log_setup (optional)"},
			},
			Required: []string{"ticker", "direction", "conviction", "thesis"},
		},
	}, func(args map[string]any) (string, error) {
		primeDir := os.Getenv("EMILY_INTEGRATION_DIR")
		if primeDir == "" {
			primeDir = filepath.Join(root, "..", "EMILY", "signals", "observations")
		} else {
			primeDir = filepath.Join(primeDir, "observations")
		}
		if err := os.MkdirAll(primeDir, 0o755); err != nil {
			return "", fmt.Errorf("prime dir: %w", err)
		}
		now := time.Now().UTC()
		ticker, _ := args["ticker"].(string)
		direction, _ := args["direction"].(string)
		conviction, _ := args["conviction"].(string)
		thesis, _ := args["thesis"].(string)
		signalStack, _ := args["signal_stack"].(string)
		setupID, _ := args["setup_id"].(string)

		payload := map[string]any{
			"timestamp":        now.Format(time.RFC3339),
			"source":           "jon-stockwell",
			"observation_type": "trade_setup",
			"severity":         "info",
			"ticker":           strings.ToUpper(ticker),
			"direction":        direction,
			"conviction":       conviction,
			"summary":          fmt.Sprintf("Jon setup: %s %s %s conviction", strings.ToUpper(ticker), direction, conviction),
			"findings":         thesis,
			"signal_stack":     signalStack,
			"setup_id":         setupID,
		}
		b, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", err
		}
		slug := slugify(fmt.Sprintf("%s-%s-%s", ticker, direction, conviction), 32)
		fname := strings.ReplaceAll(now.Format(time.RFC3339), ":", "") + "-" + slug + ".json"
		outPath := filepath.Join(primeDir, fname)
		if err := os.WriteFile(outPath, b, 0o644); err != nil {
			return "", fmt.Errorf("write to prime: %w", err)
		}
		return fmt.Sprintf(`{"published_to_prime":"%s"}`, fname), nil
	})
}

// ── Market data stubs ─────────────────────────────────────────────────────────
// These return a clear "not connected" message with configuration instructions
// until a real market data API is wired up.

func registerMarketDataStubs(d *ToolDispatcher) {

	notConnected := func(tool string) (string, error) {
		apiKey := os.Getenv("MARKET_DATA_API_KEY")
		if apiKey != "" {
			return fmt.Sprintf(`{"error":"%s: MARKET_DATA_API_KEY is set but no provider is wired — set MARKET_DATA_PROVIDER (polygon|tradier|cboe)"}`, tool), nil
		}
		return fmt.Sprintf(`{"error":"%s: market data not connected","config":"set MARKET_DATA_API_KEY and MARKET_DATA_PROVIDER env vars to connect"}`, tool), nil
	}

	d.Register(ToolDef{
		Name:        "jon_options_chain",
		Description: "Fetch the options chain for a ticker (strikes, expiries, bid/ask, volume, OI, Greeks). Requires MARKET_DATA_API_KEY and MARKET_DATA_PROVIDER env vars.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker":  {Type: "string", Description: "Ticker symbol"},
				"expiry":  {Type: "string", Description: "Expiry date YYYY-MM-DD (optional — nearest if omitted)"},
				"strikes": {Type: "number", Description: "Number of strikes around ATM (default 5)"},
			},
			Required: []string{"ticker"},
		},
	}, func(args map[string]any) (string, error) { return notConnected("jon_options_chain") })

	d.Register(ToolDef{
		Name:        "jon_price_data",
		Description: "Fetch OHLCV price data for a ticker. Requires MARKET_DATA_API_KEY.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"ticker":    {Type: "string", Description: "Ticker symbol"},
				"timeframe": {Type: "string", Description: "1m|5m|1h|day|week (default: day)"},
				"bars":      {Type: "number", Description: "Number of bars (default 60)"},
			},
			Required: []string{"ticker"},
		},
	}, func(args map[string]any) (string, error) { return notConnected("jon_price_data") })

	d.Register(ToolDef{
		Name:        "jon_iv_rank",
		Description: "Fetch IV rank and IV percentile for a ticker. Requires MARKET_DATA_API_KEY.",
		Parameters: ToolParameters{
			Type:     "object",
			Properties: map[string]ToolPropSchema{
				"ticker": {Type: "string", Description: "Ticker symbol"},
			},
			Required: []string{"ticker"},
		},
	}, func(args map[string]any) (string, error) { return notConnected("jon_iv_rank") })

	// Expected move calculator — pure math, no API needed.
	d.Register(ToolDef{
		Name:        "jon_expected_move",
		Description: "Calculate the expected move for a position given IV, stock price, and days to expiry. Formula: price × IV × sqrt(DTE/365). No API required.",
		Parameters: ToolParameters{
			Type: "object",
			Properties: map[string]ToolPropSchema{
				"price":  {Type: "number", Description: "Current stock price"},
				"iv":     {Type: "number", Description: "Implied volatility as decimal (e.g. 0.35 for 35%)"},
				"dte":    {Type: "number", Description: "Days to expiry"},
				"stdev":  {Type: "number", Description: "Standard deviations for move (default 1.0)"},
			},
			Required: []string{"price", "iv", "dte"},
		},
	}, func(args map[string]any) (string, error) {
		price, _ := args["price"].(float64)
		iv, _ := args["iv"].(float64)
		dte, _ := args["dte"].(float64)
		stdev := 1.0
		if v, ok := args["stdev"].(float64); ok && v > 0 {
			stdev = v
		}
		if price <= 0 || iv <= 0 || dte <= 0 {
			return `{"error":"price, iv, and dte must all be positive"}`, nil
		}
		em := price * iv * math.Sqrt(dte/365.0) * stdev
		b, _ := json.MarshalIndent(map[string]any{
			"price":              price,
			"iv":                 iv,
			"dte":                dte,
			"stdev":              stdev,
			"expected_move":      math.Round(em*100) / 100,
			"upper_target":       math.Round((price+em)*100) / 100,
			"lower_target":       math.Round((price-em)*100) / 100,
			"expected_move_pct":  math.Round(em/price*10000) / 100,
		}, "", "  ")
		return string(b), nil
	})
}
