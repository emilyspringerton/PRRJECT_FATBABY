// entity-graph polls the secwatch event store for source_document_persisted
// events, extracts director vote data from 8-K Item 5.07 sections, builds a
// persistent entity graph (nodes + edges), and emits governance signals.
//
// On each run it writes to:
//
//	var/entity-graph/nodes.ndjson    — PersonNode records (append)
//	var/entity-graph/edges.ndjson    — Edge records (append)
//	var/entity-graph/signals.ndjson  — Signal records (append)
//
// After each batch it publishes an Observation to var/emily-observations/ so
// the observation-watcher / Emily feedback loop can pick it up.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "secwatch event store root")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "output directory for graph NDJSON files")
	schd13Dir := flag.String("schd13-dir", filepath.Join("var", "schd13"), "directory containing schd13-watcher filings.ndjson for accuracy tracking")
	obsDir := flag.String("obs-dir", filepath.Join("var", "emily-observations"), "observation output directory")
	rulesPath := flag.String("rules", filepath.Join("config", "entity-graph-rules.json"), "signal scoring rules (hot-reloaded each batch)")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "how often to poll the event store")
	batchSize := flag.Int("batch-size", 256, "max events to read per poll")
	cursorPath := flag.String("cursor", filepath.Join("var", "entity-graph", ".cursor"), "file storing last-processed sequence number")
	oneShot := flag.Bool("one-shot", false, "process one batch and exit (useful for cron or Emily's one-shot runner)")
	flag.Parse()

	logger := log.New(os.Stdout, "entity-graph ", log.LstdFlags|log.LUTC)

	if err := os.MkdirAll(*graphDir, 0o755); err != nil {
		logger.Fatalf("mkdir %s: %v", *graphDir, err)
	}

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open event store: %v", err)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Printf("starting poll_interval=%s store=%s graph_dir=%s one_shot=%v", *pollInterval, *storeRoot, *graphDir, *oneShot)

	cursor := loadCursor(*cursorPath, logger)

	for {
		cursor = runBatch(ctx, store, logger, runConfig{
			graphDir:   *graphDir,
			schd13Dir:  *schd13Dir,
			obsDir:     *obsDir,
			rulesPath:  *rulesPath,
			cursorPath: *cursorPath,
			batchSize:  *batchSize,
			cursor:     cursor,
		})

		if *oneShot {
			logger.Printf("one-shot complete")
			return
		}

		select {
		case <-ctx.Done():
			logger.Printf("shutting down")
			return
		case <-time.After(*pollInterval):
		}
	}
}

type runConfig struct {
	graphDir   string
	schd13Dir  string
	obsDir     string
	rulesPath  string
	cursorPath string
	batchSize  int
	cursor     uint64
}

func runBatch(ctx context.Context, store eventstore.EventStore, logger *log.Logger, cfg runConfig) uint64 {
	rules := entitygraph.LoadRules(cfg.rulesPath)

	recs, err := store.ReadFrom(ctx, cfg.cursor, cfg.batchSize)
	if err != nil {
		logger.Printf("read events cursor=%d err=%v", cfg.cursor, err)
		return cfg.cursor
	}
	if len(recs) == 0 {
		return cfg.cursor
	}

	// Compact nodes.ndjson before loading to remove accumulated duplicates.
	if err := entitygraph.CompactNodes(cfg.graphDir); err != nil {
		logger.Printf("compact nodes err=%v (non-fatal)", err)
	}

	graph := entitygraph.NewGraph()
	if err := graph.LoadNodesFromDir(cfg.graphDir); err != nil {
		logger.Printf("load existing nodes err=%v", err)
	}
	if err := graph.LoadAuditorsFromDir(cfg.graphDir); err != nil {
		logger.Printf("load existing auditors err=%v", err)
	}

	// Load historical signals for composite scoring (activist_risk, director_link).
	historicalSignals, err := entitygraph.LoadSignals(cfg.graphDir)
	if err != nil {
		logger.Printf("load historical signals err=%v", err)
	}

	var (
		allSignals         []entitygraph.Signal
		parseErrors        []entitygraph.ParseError
		processed          int
		totalProposals     int
		seenSourceDocs     int
	)

	for _, r := range recs {
		if r.Event.Type != "source_document_persisted" {
			continue
		}
		seenSourceDocs++
		var doc intelligence.SourceDocument
		if err := json.Unmarshal(r.Event.Data, &doc); err != nil {
			logger.Printf("unmarshal source_document seq=%d err=%v", r.Sequence, err)
			continue
		}
		// Accept documents labelled as 8-K via any of three signals:
		// 1. doc.Form set correctly by the processor (preferred, going forward)
		// 2. doc.SourceType == "sec_8k" (set by processor when form_type is populated)
		// 3. URL contains "8-k" (fallback for historical docs persisted before the
		//    form_type → form fix — all those docs have form="" source_type="press_release"
		//    even though they are genuine 8-K filings)
		is8K := doc.Form == "8-K" ||
			doc.SourceType == "sec_8k" ||
			strings.Contains(strings.ToUpper(doc.DocumentURL), "8-K")
		if !is8K {
			continue
		}

		logger.Printf("processing seq=%d ticker=%s identity=%s chars=%d", r.Sequence, doc.Ticker, doc.Identity, doc.CleanedCharCount)
		processed++

		result, err := entitygraph.ParseItem507(doc.CleanedText)
		if err != nil {
			if errors.Is(err, entitygraph.ErrItem507NotFound) {
				// Most 8-K subtypes (earnings, officer appointments, M&A) don't contain
				// Item 5.07 — this is expected, not a parse failure.
				logger.Printf("skip_no_507 seq=%d ticker=%s identity=%s", r.Sequence, doc.Ticker, doc.Identity)
				processed-- // don't count as processed; it contributed nothing
			} else {
				logger.Printf("parse_item507 seq=%d identity=%s err=%v", r.Sequence, doc.Identity, err)
				parseErrors = append(parseErrors, entitygraph.ParseError{
					Ticker:   doc.Ticker,
					Identity: doc.Identity,
					Error:    err.Error(),
				})
			}
			continue
		}

		if len(result.DirectorVotes) == 0 {
			logger.Printf("no_directors seq=%d identity=%s", r.Sequence, doc.Identity)
			continue
		}

		// Prefer the SEC filing date from the document metadata; fall back to
		// PersistedAt only for legacy docs that pre-date the FilingDate field.
		// Never use time.Now() — that would stamp backfilled historical filings
		// with today's date, making 2019 filings appear as breaking news.
		filingDate := doc.FilingDate
		if filingDate == "" {
			filingDate = doc.PersistedAt.Format("2006-01-02")
			if doc.PersistedAt.IsZero() {
				filingDate = time.Now().UTC().Format("2006-01-02")
			}
		}
		var canonIDs []string

		for _, vote := range result.DirectorVotes {
			app := entitygraph.FilingAppearance{
				Ticker:         doc.Ticker,
				Form:           doc.Form,
				FilingDate:     filingDate,
				ApprovalPct:    vote.ApprovalPct,
				ForVotes:       vote.ForVotes,
				AgainstVotes:   vote.AgainstVotes,
				AbstainVotes:   vote.AbstainVotes,
				BrokerNonVotes: vote.BrokerNonVotes,
			}
			node := graph.UpsertPerson(vote.Name, entitygraph.NodeDirector, app)
			canonIDs = append(canonIDs, node.CanonicalID)
			logger.Printf("upserted person=%s canonical=%s approval=%.3f ticker=%s", vote.Name, node.CanonicalID, vote.ApprovalPct, doc.Ticker)
		}

		graph.BuildEdgesFromFiling(canonIDs, doc.Ticker)

		dirSigs := entitygraph.ScoreDirectorVotes(result.DirectorVotes, doc.Ticker, filingDate, rules)
		propSigs := entitygraph.ScoreProposals(result.Proposals, doc.Ticker, filingDate, rules)

		// Tag all per-filing signals with their source identity for traceability.
		for i := range dirSigs {
			if dirSigs[i].Metadata == nil {
				dirSigs[i].Metadata = map[string]string{}
			}
			dirSigs[i].Metadata["source_identity"] = doc.Identity
		}
		for i := range propSigs {
			if propSigs[i].Metadata == nil {
				propSigs[i].Metadata = map[string]string{}
			}
			propSigs[i].Metadata["source_identity"] = doc.Identity
		}

		allSignals = append(allSignals, dirSigs...)
		allSignals = append(allSignals, propSigs...)
		totalProposals += len(result.Proposals)

		// Auditor tracking: detect firm changes across filings.
		if result.Auditor != "" {
			changed, prev := graph.TrackAuditor(doc.Ticker, result.Auditor, filingDate)
			if changed {
				sig := entitygraph.ScoreAuditorChange(doc.Ticker, prev, result.Auditor, filingDate)
				allSignals = append(allSignals, sig)
				logger.Printf("auditor_change ticker=%s prev=%q new=%q", doc.Ticker, prev, result.Auditor)
			} else {
				logger.Printf("auditor ticker=%s firm=%q (unchanged)", doc.Ticker, result.Auditor)
			}
		}

		logger.Printf("signals ticker=%s dir_signals=%d prop_signals=%d proposals=%d", doc.Ticker, len(dirSigs), len(propSigs), len(result.Proposals))
	}

	// Score director decay using multi-filing approval history from the graph.
	// Only nodes with 2+ filings at the same ticker produce a decay signal.
	decaySigs := scoreDecayFromGraph(graph, rules, logger)
	allSignals = append(allSignals, decaySigs...)

	// Composite signals: combine current batch with historical for cross-filing detection.
	combined := append(historicalSignals, allSignals...)

	// activist_risk: governance_entrenchment + director_friction co-occurrence per ticker.
	batchTickers := collectTickers(allSignals)
	for ticker := range batchTickers {
		if sig := entitygraph.ScoreCompositeActivistRisk(ticker, combined, rules.ActivistRiskWindowDays); sig != nil {
			logger.Printf("activist_risk ticker=%s score=%.3f", ticker, sig.Score)
			allSignals = append(allSignals, *sig)
		}
	}

	// director_link: propagate friction scores to other tickers via shared directors.
	linkSigs := entitygraph.ScoreDirectorLinks(graph, combined)
	if len(linkSigs) > 0 {
		logger.Printf("director_link signals=%d", len(linkSigs))
		allSignals = append(allSignals, linkSigs...)
	}

	// governance_health_index: composite health score per ticker.
	// Re-compute combined to include director_link signals before scoring.
	combined = append(combined, linkSigs...)
	for ticker := range batchTickers {
		if sig := entitygraph.ScoreGovernanceHealth(ticker, combined, rules.GovernanceHealthWindowDays); sig != nil {
			logger.Printf("governance_health ticker=%s score=%.3f severity=%s", ticker, sig.Score, sig.Severity)
			allSignals = append(allSignals, *sig)
		}
	}

	newCursor := recs[len(recs)-1].Sequence + 1

	if seenSourceDocs > 0 && processed == 0 {
		logger.Printf("WARNING: saw %d source_document_persisted records but found 0 8-K documents to process — check form/source_type/url detection logic", seenSourceDocs)
	}

	// Retrospective accuracy: correlate activist_risk predictions with 13D filings
	// loaded from the schd13-watcher's output directory.
	var accuracyReports []entitygraph.AccuracyReport
	if cfg.schd13Dir != "" {
		schd13Filings, err := entitygraph.LoadSchd13Filings(cfg.schd13Dir)
		if err != nil {
			logger.Printf("load schd13 filings err=%v (non-fatal)", err)
		}
		if len(schd13Filings) > 0 {
			allHistorical, _ := entitygraph.LoadSignals(cfg.graphDir)
			accuracyRecords := entitygraph.CorrelateActivistRisk(append(allHistorical, allSignals...), schd13Filings)
			if len(accuracyRecords) > 0 {
				if err := entitygraph.WriteAccuracyRecords(cfg.graphDir, accuracyRecords); err != nil {
					logger.Printf("write accuracy records err=%v", err)
				}
				accuracyReports = entitygraph.BuildAccuracyReports(accuracyRecords)
				logger.Printf("accuracy records=%d reports=%d", len(accuracyRecords), len(accuracyReports))
			}
		}
	}

	if processed > 0 {
		if err := graph.FlushNodes(cfg.graphDir); err != nil {
			logger.Printf("flush nodes err=%v", err)
		}
		if err := graph.FlushEdges(cfg.graphDir); err != nil {
			logger.Printf("flush edges err=%v", err)
		}
		if err := graph.FlushAuditors(cfg.graphDir); err != nil {
			logger.Printf("flush auditors err=%v", err)
		}
		if len(allSignals) > 0 {
			if err := entitygraph.WriteSignals(cfg.graphDir, allSignals); err != nil {
				logger.Printf("write signals err=%v", err)
			}
		}
		logger.Printf("batch complete processed=%d directors=%d signals=%d parse_errors=%d", processed, len(graph.Nodes), len(allSignals), len(parseErrors))

		obs := entitygraph.BuildObservation(processed, allSignals, parseErrors, len(graph.Nodes), totalProposals, accuracyReports)
		if err := entitygraph.PublishObservation(obs, cfg.obsDir); err != nil {
			logger.Printf("publish observation err=%v", err)
		} else {
			logger.Printf("observation published dir=%s status=%s", cfg.obsDir, obs.Status)
		}
	}

	saveCursor(cfg.cursorPath, newCursor, logger)
	return newCursor
}

// collectTickers returns the set of unique tickers present in a signal slice.
func collectTickers(signals []entitygraph.Signal) map[string]bool {
	tickers := make(map[string]bool)
	for _, s := range signals {
		if s.Ticker != "" {
			tickers[s.Ticker] = true
		}
	}
	return tickers
}

// scoreDecayFromGraph computes director_decay signals from multi-filing approval
// histories stored in the in-memory graph. For each node with 2+ appearances at
// the same ticker, it sorts filings by date and calls ScoreDirectorDecay.
func scoreDecayFromGraph(graph *entitygraph.Graph, rules entitygraph.Rules, logger *log.Logger) []entitygraph.Signal {
	var out []entitygraph.Signal
	for _, node := range graph.Nodes {
		// Group filings by ticker.
		byTicker := map[string][]entitygraph.FilingAppearance{}
		for _, f := range node.Filings {
			byTicker[f.Ticker] = append(byTicker[f.Ticker], f)
		}
		for ticker, filings := range byTicker {
			if len(filings) < rules.DecayMinYears {
				continue
			}
			sort.Slice(filings, func(i, j int) bool {
				return filings[i].FilingDate < filings[j].FilingDate
			})
			history := make([]float64, len(filings))
			for i, f := range filings {
				history[i] = f.ApprovalPct
			}
			sig := entitygraph.ScoreDirectorDecay(node.Name, ticker, history, rules)
			if sig != nil {
				logger.Printf("decay signal director=%s ticker=%s filings=%d avg_drop=%.2f%%", node.Name, ticker, len(filings), sig.Score*100)
				out = append(out, *sig)
			}
		}
	}
	return out
}

func loadCursor(path string, logger *log.Logger) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 1
	}
	var seq uint64
	if err := json.Unmarshal(data, &seq); err != nil {
		logger.Printf("cursor parse err=%v, starting from 1", err)
		return 1
	}
	return seq
}

func saveCursor(path string, seq uint64, logger *log.Logger) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Printf("cursor mkdir err=%v", err)
		return
	}
	data, _ := json.Marshal(seq)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logger.Printf("cursor write err=%v", err)
	}
}
