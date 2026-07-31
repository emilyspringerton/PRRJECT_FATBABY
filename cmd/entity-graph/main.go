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
	"database/sql"
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

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/mongowriter"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/secwatch"
)

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "secwatch event store root")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "output directory for graph NDJSON files")
	schd13Dir := flag.String("schd13-dir", filepath.Join("var", "schd13"), "directory containing schd13-watcher filings.ndjson for accuracy tracking")
	obsDir := flag.String("obs-dir", filepath.Join("var", "emily-observations"), "observation output directory")
	rulesPath := flag.String("rules", filepath.Join("config", "entity-graph-rules.json"), "signal scoring rules (hot-reloaded each batch)")
	watchlistPath := flag.String("watchlist", filepath.Join("config", "watchlist.json"), "watchlist JSON for sector peer comparison")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "how often to poll the event store")
	batchSize := flag.Int("batch-size", 256, "max events to read per poll")
	cursorPath := flag.String("cursor", filepath.Join("var", "entity-graph", ".cursor"), "file storing last-processed sequence number")
	oneShot := flag.Bool("one-shot", false, "process one batch and exit (useful for cron or Emily's one-shot runner)")
	mongoURL := flag.String("mongo-url", os.Getenv("MONGODB_URL"), "MongoDB connection string (default: $MONGODB_URL); empty = no MongoDB write")
	mongoDB := flag.String("mongo-db", envOr("MONGODB_DB", "fatbaby"), "MongoDB database name")
	filingIndexPath := flag.String("filing-index-db", "", "incremental filing-date/form index (default: <graph-dir>/filings-index.db) -- see docs/northstar/replay-fragility.md §4c")
	accuracyIndexPath := flag.String("accuracy-index-db", "", "incremental deduplicated accuracy-verdict index (default: <graph-dir>/accuracy-index.db) -- see docs/northstar/replay-fragility.md §4c Phase 2b")
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

	filingDBPath := *filingIndexPath
	if filingDBPath == "" {
		filingDBPath = filepath.Join(*graphDir, "filings-index.db")
	}
	filingDB, err := openFilingIndexDB(filingDBPath)
	if err != nil {
		logger.Fatalf("open filing index: %v", err)
	}
	defer filingDB.Close()
	if err := ensureFilingIndexBackfilled(ctx, filingDB, store, logger); err != nil {
		logger.Fatalf("backfill filing index: %v", err)
	}

	accuracyDBPath := *accuracyIndexPath
	if accuracyDBPath == "" {
		accuracyDBPath = filepath.Join(*graphDir, "accuracy-index.db")
	}
	accuracyDB, err := openAccuracyIndexDB(accuracyDBPath)
	if err != nil {
		logger.Fatalf("open accuracy index: %v", err)
	}
	defer accuracyDB.Close()
	if err := ensureAccuracyIndexBackfilled(accuracyDB, *graphDir, logger); err != nil {
		logger.Fatalf("backfill accuracy index: %v", err)
	}

	// Optional MongoDB write (S20-04). Nil client = graceful no-op.
	var mongoClient *mongo.Client
	if *mongoURL != "" {
		mc, err := mongowriter.Connect(ctx, *mongoURL)
		if err != nil {
			logger.Printf("WARNING: MongoDB connect failed (%v); continuing without MongoDB write", err)
		} else {
			mongoClient = mc
			defer mongoClient.Disconnect(ctx) //nolint:errcheck
			logger.Printf("MongoDB connected db=%s", *mongoDB)
		}
	}

	logger.Printf("starting poll_interval=%s store=%s graph_dir=%s one_shot=%v mongo=%v", *pollInterval, *storeRoot, *graphDir, *oneShot, mongoClient != nil)

	// Graph-lifetime hoist (Phase 2c, docs/northstar/replay-fragility.md §4c
	// item 3 -- "the riskiest," landing last and alone with Phase 2b's
	// accuracy-report equivalence check as its regression gate). These used
	// to be reloaded from scratch inside runBatch on every 30s poll --
	// NewGraph/LoadNodesFromDir/LoadAuditorsFromDir/LoadSignals/
	// LoadHealthHistory/CompactNodes. Now loaded exactly once here. graph is
	// a *Graph (already mutated in place and flushed incrementally by
	// FlushNodes/FlushEdges -- no change needed there). historicalSignals
	// and healthHistory are updated in memory after each batch (mirroring
	// exactly what a fresh reload would have picked up from what that batch
	// just wrote to disk) instead of being re-read from the files they were
	// just written to.
	if err := entitygraph.CompactNodes(*graphDir); err != nil {
		logger.Printf("compact nodes err=%v (non-fatal)", err)
	}
	graph := entitygraph.NewGraph()
	if err := graph.LoadNodesFromDir(*graphDir); err != nil {
		logger.Printf("load existing nodes err=%v", err)
	}
	if err := graph.LoadAuditorsFromDir(*graphDir); err != nil {
		logger.Printf("load existing auditors err=%v", err)
	}
	historicalSignals, err := entitygraph.LoadSignals(*graphDir)
	if err != nil {
		logger.Printf("load historical signals err=%v", err)
	}
	historicalSignals = entitygraph.DeduplicateSignals(historicalSignals)
	healthHistory, err := entitygraph.LoadHealthHistory(*graphDir)
	if err != nil {
		logger.Printf("load health history err=%v (non-fatal)", err)
	}
	if healthHistory == nil {
		healthHistory = map[string]entitygraph.HealthSnapshot{}
	}

	cursor := loadCursor(*cursorPath, logger)

	for {
		cursor, historicalSignals = runBatch(ctx, store, logger, runConfig{
			graphDir:      *graphDir,
			schd13Dir:     *schd13Dir,
			obsDir:        *obsDir,
			rulesPath:     *rulesPath,
			watchlistPath: *watchlistPath,
			cursorPath:    *cursorPath,
			batchSize:     *batchSize,
			cursor:        cursor,
			mongoClient:   mongoClient,
			mongoDB:       *mongoDB,
			filingDB:      filingDB,
			accuracyDB:    accuracyDB,
			graph:         graph,
			healthHistory: healthHistory,
			historicalSignals: historicalSignals,
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
	graphDir      string
	schd13Dir     string
	obsDir        string
	rulesPath     string
	watchlistPath string
	cursorPath    string
	batchSize     int
	cursor        uint64
	mongoClient   *mongo.Client
	mongoDB       string
	filingDB      *sql.DB
	accuracyDB    *sql.DB
	graph         *entitygraph.Graph
	healthHistory map[string]entitygraph.HealthSnapshot
	historicalSignals []entitygraph.Signal
}

// runBatch returns the new cursor and the updated historicalSignals slice
// (threaded the same way cursor already was -- see the graph-lifetime hoist
// comment in main()). cfg.graph and cfg.healthHistory are mutated in place
// (pointer and map respectively) and don't need to be returned.
func runBatch(ctx context.Context, store eventstore.EventStore, logger *log.Logger, cfg runConfig) (uint64, []entitygraph.Signal) {
	rules := entitygraph.LoadRules(cfg.rulesPath)

	recs, err := store.ReadFrom(ctx, cfg.cursor, cfg.batchSize)
	if err != nil {
		logger.Printf("read events cursor=%d err=%v", cfg.cursor, err)
		return cfg.cursor, cfg.historicalSignals
	}

	// Phase 3 checkpoint freshness heartbeat (docs/northstar/replay-fragility.md
	// §4c/§9 Phase 3) -- touch both index checkpoints' meta.snapshot_at every
	// successful poll tick, not just when this batch happens to produce new
	// filing/accuracy rows, mirroring how CheckPollerHealth's log-mtime
	// heartbeat already treats "checked, nothing new" as proof of life for
	// headless pollers with no HTTP endpoint.
	touchFilingIndexSnapshot(cfg.filingDB, logger)
	touchAccuracyIndexSnapshot(cfg.accuracyDB, logger)

	if len(recs) == 0 {
		return cfg.cursor, cfg.historicalSignals
	}

	// Filing date/form recovery index -- was a full event-store scan every
	// batch (buildFilingIndexes), now an incrementally-maintained SQLite
	// table (see filingindex.go): backfilled once at startup, updated here
	// with only this batch's own filing_discovered records (already fetched
	// above, no extra store read), read back in full (cheap: local SQLite,
	// not a store scan).
	//
	// Recovers, for legacy docs:
	// 1. The SEC filing date for source documents that pre-date FilingDate.
	// 2. The form type (8-K vs other) for docs where form="" source_type="press_release"
	//    due to a historical processor bug (legacy docs persisted before the form_type→form fix).
	upsertFilingIndexFromBatch(cfg.filingDB, recs, logger)
	filingDates, filingForms, err := loadFilingIndexes(cfg.filingDB)
	if err != nil {
		logger.Printf("load filing index err=%v (continuing with empty index this batch)", err)
		filingDates, filingForms = map[string]string{}, map[string]string{}
	}

	// graph, historicalSignals, and prevHealthHistory used to be reloaded from
	// scratch here on every batch -- now hoisted to process start (main()),
	// see the graph-lifetime hoist comment there. graph mutates in place and
	// flushes incrementally as before; prevHealthHistory is a map (mutated in
	// place, visible to the caller without needing to be returned).
	graph := cfg.graph
	historicalSignals := cfg.historicalSignals
	prevHealthHistory := cfg.healthHistory

	// RSI: load historical accuracy records to calibrate governance health penalty weights.
	// AccuracyAdjustedPenalties scales down weights for signal types whose empirical
	// precision is below the configured threshold, closing the RSI feedback loop.
	prevAccuracyRecords, _ := loadAccuracyIndex(cfg.accuracyDB)
	prevAccuracyReports := entitygraph.BuildAccuracyReports(prevAccuracyRecords)
	healthPenalties := entitygraph.AccuracyAdjustedPenalties(
		entitygraph.DefaultGovernanceHealthPenalties(),
		prevAccuracyReports,
		rules.MinResolvedForCalibration,
	)
	if len(prevAccuracyRecords) > 0 {
		logger.Printf("rsi_calibration loaded accuracy_records=%d reports=%d calibrated_penalties=%d",
			len(prevAccuracyRecords), len(prevAccuracyReports), len(healthPenalties))
	}

	var (
		allSignals          []entitygraph.Signal
		parseErrors         []entitygraph.ParseError
		processed           int
		totalProposals      int
		seenSourceDocs      int
		directorsThisBatch  int
		is8KCount           int // S159-02: distinct from `processed`, which gets decremented back
		                        // down for legitimate 8-Ks with no Item 5.07 content (most
		                        // subtypes -- earnings, appointments, M&A -- don't have it, and
		                        // that's expected, not a detection failure). is8KCount only
		                        // tracks whether the classifier itself found any 8-Ks at all.
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
		// Accept documents labelled as 8-K via any of four signals:
		// 1. doc.Form set correctly by the processor (preferred, going forward)
		// 2. doc.SourceType == "sec_8k" (set by processor when form_type is populated)
		// 3. URL contains "8-k" (rarely set for EDGAR docs — kept for completeness)
		// 4. filing_discovered event for this identity has EffectiveForm() == "8-K"
		//    (recovers 1104+ historical docs where processor emitted form="" source_type="press_release"
		//    due to a legacy bug before the form_type→form fix)
		is8K := doc.Form == "8-K" ||
			doc.SourceType == "sec_8k" ||
			strings.Contains(strings.ToUpper(doc.DocumentURL), "8-K") ||
			filingForms[doc.Identity] == "8-K"
		if !is8K {
			continue
		}
		is8KCount++

		logger.Printf("processing seq=%d ticker=%s identity=%s chars=%d", r.Sequence, doc.Ticker, doc.Identity, doc.CleanedCharCount)
		processed++

		// Always try Item 5.02 (leadership changes) regardless of 507 outcome.
		// 5.02 and 5.07 appear in different 8-K subtypes; processing both costs nothing.
		if lResult, lErr := entitygraph.ParseItem502(doc.CleanedText); lErr == nil {
			lFilingDate := doc.FilingDate
			if lFilingDate == "" {
				if date, ok := filingDates[doc.Identity]; ok {
					lFilingDate = date
				} else if !doc.PersistedAt.IsZero() {
					lFilingDate = doc.PersistedAt.Format("2006-01-02")
				}
			}
			lSignals := entitygraph.ScoreLeadershipChange(lResult, doc.Ticker, lFilingDate)
			if len(lSignals) > 0 {
				allSignals = append(allSignals, lSignals...)
				logger.Printf("item502 seq=%d ticker=%s departures=%d appointments=%d signals=%d", r.Sequence, doc.Ticker, len(lResult.Departures), len(lResult.Appointments), len(lSignals))
			}
		}

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

		// Resolve the SEC filing date. Priority order:
		// 1. doc.FilingDate from the source_document_persisted payload (set by
		//    processor from the filing_discovered event going forward).
		// 2. The filing_discovered event index built at batch start (recovers
		//    the correct date for historical docs stored without FilingDate).
		// 3. doc.PersistedAt as a last resort for truly undated legacy docs.
		//
		// Never use time.Now() — that stamps backfilled historical filings
		// with today's date, making 2019 filings appear as breaking news.
		filingDate := doc.FilingDate
		if filingDate == "" {
			if date, ok := filingDates[doc.Identity]; ok {
				filingDate = date
				logger.Printf("filing_date recovered from index identity=%s date=%s", doc.Identity, date)
			} else if !doc.PersistedAt.IsZero() {
				filingDate = doc.PersistedAt.Format("2006-01-02")
			} else {
				filingDate = time.Now().UTC().Format("2006-01-02")
			}
		}

		// Score proposals and track auditor regardless of whether director votes
		// were found — some older filings (AAPL 2011–2014) have a 3-column director
		// table that the 4-column regex misses on first pass, but their proposals are
		// fully parseable and should not be silently dropped.
		propSigs := entitygraph.ScoreProposals(result.Proposals, doc.Ticker, filingDate, rules)
		for i := range propSigs {
			if propSigs[i].Metadata == nil {
				propSigs[i].Metadata = map[string]string{}
			}
			propSigs[i].Metadata["source_identity"] = doc.Identity
		}
		allSignals = append(allSignals, propSigs...)
		totalProposals += len(result.Proposals)

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

		if len(result.DirectorVotes) == 0 {
			logger.Printf("no_directors seq=%d identity=%s proposals=%d", r.Sequence, doc.Identity, len(result.Proposals))
			continue
		}

		directorsThisBatch += len(result.DirectorVotes)
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
		dirSigs = entitygraph.FilterHighTrustByMinFilings(dirSigs, graph, doc.Ticker, rules.HighTrustMinFilings)

		// Tag director signals with source identity.
		for i := range dirSigs {
			if dirSigs[i].Metadata == nil {
				dirSigs[i].Metadata = map[string]string{}
			}
			dirSigs[i].Metadata["source_identity"] = doc.Identity
		}

		allSignals = append(allSignals, dirSigs...)

		logger.Printf("signals ticker=%s dir_signals=%d prop_signals=%d proposals=%d", doc.Ticker, len(dirSigs), len(propSigs), len(result.Proposals))
	}

	// Score director decay using multi-filing approval history from the graph.
	// Only nodes with 2+ filings at the same ticker produce a decay signal.
	decaySigs := scoreDecayFromGraph(graph, rules, logger)
	allSignals = append(allSignals, decaySigs...)

	// Director long-tenure: flag directors whose board service exceeds the ISS/Glass Lewis
	// independence threshold. Runs against the full graph (not just this batch).
	tenureSigs := entitygraph.ScoreLongTenure(graph, rules)
	if len(tenureSigs) > 0 {
		logger.Printf("director_long_tenure signals=%d", len(tenureSigs))
		allSignals = append(allSignals, tenureSigs...)
	}

	// board_decay_concern: fires when MinBoardDecayCount or more directors at a ticker
	// have concurrent director_decay signals — a stronger activist predictor than any
	// single director's decline. Must run after decay signals are collected.
	for ticker := range collectTickers(allSignals) {
		if sig := entitygraph.ScoreBoardDecayConcern(ticker, allSignals, rules); sig != nil {
			logger.Printf("board_decay_concern ticker=%s score=%.3f severity=%s", ticker, sig.Score, sig.Severity)
			allSignals = append(allSignals, *sig)
		}
	}

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

	// post_failure_activist_prediction: fires when a recent governance_entrenchment
	// exists for a ticker, even without concurrent friction. Earlier and lower-confidence
	// than activist_risk; catches the window between a failed structural vote and the
	// onset of director-level friction that activist pressure typically produces.
	for ticker := range batchTickers {
		if sig := entitygraph.ScorePostFailureActivistPrediction(ticker, combined, rules.PostFailureActivistWindowDays); sig != nil {
			logger.Printf("post_failure_activist_prediction ticker=%s score=%.3f severity=%s", ticker, sig.Score, sig.Severity)
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
	// Uses accuracy-calibrated penalties (healthPenalties) so historically
	// low-precision signals contribute reduced weight to the composite score.
	combined = append(combined, linkSigs...)
	// Deduplicate before governance health scoring. historicalSignals is kept
	// deduped (loaded once in main(), re-deduped after each batch's writes --
	// see the graph-lifetime hoist), but the current batch may re-generate
	// signals with the same signal_id (e.g. director_long_tenure_{name}_{ticker}
	// is date-free and identical across runs). Without this second dedup,
	// combined has 2x copies and the doubled penalties drive health scores to
	// 0 for clean companies with many long-tenured directors.
	combined = entitygraph.DeduplicateSignals(combined)
	healthScores := map[string]float64{}
	for ticker := range batchTickers {
		if sig := entitygraph.ScoreGovernanceHealthWithPenalties(ticker, combined, rules.GovernanceHealthWindowDays, healthPenalties); sig != nil {
			logger.Printf("governance_health ticker=%s score=%.3f severity=%s", ticker, sig.Score, sig.Severity)
			allSignals = append(allSignals, *sig)
			healthScores[ticker] = sig.Score
		}
	}

	// governance_health_trend: emit deteriorating/improving signals by comparing each
	// ticker's new health score to its previous snapshot. Persist new snapshots after.
	var newHealthSnapshots []entitygraph.HealthSnapshot
	for ticker, score := range healthScores {
		minDelta := rules.GovernanceHealthTrendMinDelta
		if prev, ok := prevHealthHistory[ticker]; ok {
			if sig := entitygraph.ScoreGovernanceHealthTrend(ticker, score, prev.Score, minDelta); sig != nil {
				logger.Printf("governance_health_trend ticker=%s delta=%.3f severity=%s", ticker, sig.Score, sig.Severity)
				allSignals = append(allSignals, *sig)
			}
		}
		newHealthSnapshots = append(newHealthSnapshots, entitygraph.HealthSnapshot{
			Ticker:     ticker,
			Score:      score,
			RecordedAt: time.Now().UTC().Format("2006-01-02"),
		})
	}
	if len(newHealthSnapshots) > 0 {
		if err := entitygraph.AppendHealthSnapshot(cfg.graphDir, newHealthSnapshots); err != nil {
			logger.Printf("append health snapshots err=%v (non-fatal)", err)
		}
		// Mirror the append into the long-lived in-memory map (graph-lifetime
		// hoist) -- a fresh LoadHealthHistory next batch would have picked up
		// exactly these new per-ticker snapshots; this keeps that behavior
		// without re-reading the file. prevHealthHistory is cfg.healthHistory
		// (a map, reference type) so this mutation is visible to main()'s
		// loop without needing to be returned.
		for _, snap := range newHealthSnapshots {
			prevHealthHistory[snap.Ticker] = snap
		}
	}

	// Peer governance rank: compare each ticker's health score to its sector median.
	// Load sector map from watchlist; skip gracefully if unavailable.
	if len(healthScores) >= 2 && cfg.watchlistPath != "" {
		if wl, wErr := secwatch.LoadWatchlist(cfg.watchlistPath); wErr == nil {
			sectorMap := make(map[string]string, len(wl.Entries))
			for _, e := range wl.Entries {
				sectorMap[e.Ticker] = e.Sector
			}
			peerSigs := entitygraph.ScorePeerGovernanceRank(healthScores, sectorMap, rules)
			if len(peerSigs) > 0 {
				logger.Printf("governance_peer_underperformer signals=%d", len(peerSigs))
				allSignals = append(allSignals, peerSigs...)
			}
		} else {
			logger.Printf("load watchlist for peer scoring err=%v (non-fatal)", wErr)
		}
	}

	newCursor := recs[len(recs)-1].Sequence + 1

	// S159-02 (EMILY/BACKLOG.md): this used to warn on processed==0, which fires just as often
	// for a genuine detection gap as for the completely normal case of a batch full of real
	// 8-Ks that simply don't contain Item 5.07 (most subtypes don't -- earnings, appointments,
	// M&A). Investigated every historical firing of the old warning, including the exact case
	// the backlog item named (2026-07-17 23:14:33, seq=108601): pulled the raw
	// source_document_persisted event straight from var/secwatch/events/2026-07-17.ndjson --
	// a Netflix 10-Q (`"form":"10-Q","source_type":"press_release"`), correctly rejected by all
	// 4 signals because it genuinely isn't an 8-K. Every other firing checked the same way:
	// either a lone non-8-K document (routine -- most SEC filings on any given day aren't
	// 8-Ks) or a batch where real 8-Ks were found and processed, just without Item 5.07
	// content. Zero real detection gaps found anywhere in this pipeline's history, so neither
	// branch below is actionable -- both demoted from WARNING to info. seenSourceDocs/
	// is8KCount/processed stay split three ways (rather than collapsing back to one message)
	// so a real future regression -- e.g. is8KCount dropping to 0 for a batch that's actually
	// full of 8-Ks -- is still distinguishable in the logs from routine off-cycle batches.
	if seenSourceDocs > 0 && is8KCount == 0 {
		logger.Printf("info: saw %d source_document_persisted record(s) this batch, none were 8-Ks (routine for small/off-cycle batches)", seenSourceDocs)
	} else if is8KCount > 0 && processed == 0 {
		logger.Printf("info: found %d 8-K document(s) this batch, none contained Item 5.07 (voting results) — expected for most 8-K subtypes, not a detection issue", is8KCount)
	}

	// Retrospective accuracy: correlate activist_risk predictions with 13D filings
	// loaded from the schd13-watcher's output directory.
	var accuracyReports []entitygraph.AccuracyReport
	if cfg.schd13Dir != "" {
		schd13Filings, err := entitygraph.LoadSchd13Filings(cfg.schd13Dir)
		if err != nil {
			logger.Printf("load schd13 filings err=%v (non-fatal)", err)
		}
		// NOTE: intentionally still a fresh disk reload, not the hoisted
		// historicalSignals above -- out of scope for the graph-lifetime
		// hoist (Phase 2c), which only moved the specific functions named in
		// docs/northstar/replay-fragility.md §4c item 3 to keep that change
		// minimal and behavior-preserving. This is the same class of
		// per-batch reload cost and a legitimate follow-up, not fixed here.
		allHistorical, _ := entitygraph.LoadSignals(cfg.graphDir)
		allForAccuracy := append(allHistorical, allSignals...)

		var accuracyRecords []entitygraph.AccuracyRecord
		if len(schd13Filings) > 0 {
			accuracyRecords = append(accuracyRecords, entitygraph.CorrelateActivistRisk(allForAccuracy, schd13Filings)...)
		}
		// Correlate director_decay signals with subsequent leadership departures.
		decayRecords := entitygraph.CorrelateDecayDeparture(allForAccuracy)
		accuracyRecords = append(accuracyRecords, decayRecords...)

		// Correlate auditor_change signals with subsequent late_filing or eps_filing_revision.
		auditorRiskRecords := entitygraph.CorrelateAuditorChangeFilingRisk(allForAccuracy)
		accuracyRecords = append(accuracyRecords, auditorRiskRecords...)

		// Correlate insider_buy with subsequent buyback_authorization or dividend_raise.
		insiderBuyRecords := entitygraph.CorrelateInsiderBuyCapitalReturn(allForAccuracy)
		accuracyRecords = append(accuracyRecords, insiderBuyRecords...)

		// Correlate insider_sell_cluster with subsequent dividend_cut, cfo_departure, or late_filing.
		insiderSellRecords := entitygraph.CorrelateInsiderSellDistress(allForAccuracy)
		accuracyRecords = append(accuracyRecords, insiderSellRecords...)

		// Correlate cfo_departure with subsequent dividend_cut, late_filing, or eps_filing_revision.
		cfoDeparturRecords := entitygraph.CorrelateCFODepartureDistress(allForAccuracy)
		accuracyRecords = append(accuracyRecords, cfoDeparturRecords...)

		// Correlate director_friction with subsequent compensation_concern, abstention, or nomination_rejection.
		dirFrictionRecords := entitygraph.CorrelateDirectorFrictionEscalation(allForAccuracy)
		accuracyRecords = append(accuracyRecords, dirFrictionRecords...)

		// Correlate dividend_cut with subsequent cfo_departure, late_filing, or eps_filing_revision.
		divCutRecords := entitygraph.CorrelateDividendCutDeterioration(allForAccuracy)
		accuracyRecords = append(accuracyRecords, divCutRecords...)

		// Correlate late_filing with subsequent cfo_departure, dividend_cut, or eps_filing_revision.
		lateFilingRecords := entitygraph.CorrelateLateFilingDistress(allForAccuracy)
		accuracyRecords = append(accuracyRecords, lateFilingRecords...)

		// Correlate leadership_departure with subsequent dividend_cut, late_filing, or cfo_departure.
		leadershipDepRecords := entitygraph.CorrelateLeadershipDepartureDistress(allForAccuracy)
		accuracyRecords = append(accuracyRecords, leadershipDepRecords...)

		// Correlate buyback_suspension with subsequent dividend_cut, late_filing, or cfo_departure.
		buybackSuspRecords := entitygraph.CorrelateBuybackSuspensionDistress(allForAccuracy)
		accuracyRecords = append(accuracyRecords, buybackSuspRecords...)

		// Correlate abstention_spike with subsequent nomination_rejection or director_friction.
		abstentionSpikeRecords := entitygraph.CorrelateAbstentionSpikeEscalation(allForAccuracy)
		accuracyRecords = append(accuracyRecords, abstentionSpikeRecords...)

		// Correlate board_decay_concern with subsequent director_friction, cfo_departure, or late_filing.
		boardDecayRecords := entitygraph.CorrelateBoardDecayConcernDeterioration(allForAccuracy)
		accuracyRecords = append(accuracyRecords, boardDecayRecords...)

		// Correlate dividend_raise with subsequent buyback_authorization or insider_buy.
		divRaiseRecords := entitygraph.CorrelateDividendRaiseCapitalCluster(allForAccuracy)
		accuracyRecords = append(accuracyRecords, divRaiseRecords...)

		// Correlate governance_deteriorating with subsequent cfo_departure, director_friction, or late_filing.
		govDetRecords := entitygraph.CorrelateGovernanceDeterioratingDistress(allForAccuracy)
		accuracyRecords = append(accuracyRecords, govDetRecords...)

		// Correlate governance_improving with subsequent dividend_raise or buyback_authorization.
		govImpRecords := entitygraph.CorrelateGovernanceImprovingCapitalReturn(allForAccuracy)
		accuracyRecords = append(accuracyRecords, govImpRecords...)

		// Correlate governance_entrenchment with subsequent compensation_concern or abstention_spike.
		govEntrRecords := entitygraph.CorrelateGovernanceEntrenchmentVoteQuality(allForAccuracy)
		accuracyRecords = append(accuracyRecords, govEntrRecords...)

		// Correlate abstention_outlier with subsequent nomination_rejection.
		abstOutlierRecords := entitygraph.CorrelateAbstentionOutlierNominationRejection(allForAccuracy)
		accuracyRecords = append(accuracyRecords, abstOutlierRecords...)

		// Correlate post_failure_activist_prediction with subsequent activist_risk.
		postFailureRecords := entitygraph.CorrelatePostFailureActivistPrediction(allForAccuracy)
		accuracyRecords = append(accuracyRecords, postFailureRecords...)

		// Correlate buyback_authorization with subsequent insider_buy (double confidence signal).
		bbAuthRecords := entitygraph.CorrelateBuybackAuthorizationInsiderBuy(allForAccuracy)
		accuracyRecords = append(accuracyRecords, bbAuthRecords...)

		// Correlate broker_nonvote_anomaly with subsequent director_friction.
		brokerNonVoteRecords := entitygraph.CorrelateBrokerNonVoteAnomalyDirectorFriction(allForAccuracy)
		accuracyRecords = append(accuracyRecords, brokerNonVoteRecords...)

		// Correlate special_dividend with subsequent buyback_authorization or insider_buy.
		specDivRecords := entitygraph.CorrelateSpecialDividendCapitalReturn(allForAccuracy)
		accuracyRecords = append(accuracyRecords, specDivRecords...)

		// Correlate eps_filing_revision with subsequent cfo_departure, dividend_cut, or late_filing.
		epsRevRecords := entitygraph.CorrelateEPSFilingRevisionDistress(allForAccuracy)
		accuracyRecords = append(accuracyRecords, epsRevRecords...)

		// Correlate compensation_concern with subsequent abstention_spike or nomination_rejection.
		compConcernRecords := entitygraph.CorrelateCompensationConcernEscalation(allForAccuracy)
		accuracyRecords = append(accuracyRecords, compConcernRecords...)

		// Correlate nomination_rejection with subsequent director_friction or abstention_spike.
		nomRejRecords := entitygraph.CorrelateNominationRejectionFriction(allForAccuracy)
		accuracyRecords = append(accuracyRecords, nomRejRecords...)

		// Correlate high_trust_director with subsequent governance_improving or buyback_authorization.
		highTrustRecords := entitygraph.CorrelateHighTrustDirectorStability(allForAccuracy)
		accuracyRecords = append(accuracyRecords, highTrustRecords...)

		// Correlate family_control with subsequent governance_entrenchment or compensation_concern.
		familyCtrlRecords := entitygraph.CorrelateFamilyControlEntrenchment(allForAccuracy)
		accuracyRecords = append(accuracyRecords, familyCtrlRecords...)

		// Correlate director_link with subsequent director_friction or abstention_spike.
		dirLinkRecords := entitygraph.CorrelateDirectorLinkContagion(allForAccuracy)
		accuracyRecords = append(accuracyRecords, dirLinkRecords...)

		// Correlate governance_peer_underperformer with subsequent governance_deteriorating or board_decay_concern.
		peerUnderRecords := entitygraph.CorrelateGovernancePeerUnderperformerDeterioration(allForAccuracy)
		accuracyRecords = append(accuracyRecords, peerUnderRecords...)

		// Correlate governance_health_index with subsequent governance_improving or buyback_authorization.
		govHealthRecords := entitygraph.CorrelateGovernanceHealthIndexStability(allForAccuracy)
		accuracyRecords = append(accuracyRecords, govHealthRecords...)

		// Correlate director_long_tenure with subsequent governance_entrenchment or compensation_concern.
		longTenureRecords := entitygraph.CorrelateDirectorLongTenureEntrenchment(allForAccuracy)
		accuracyRecords = append(accuracyRecords, longTenureRecords...)

		if len(accuracyRecords) > 0 {
			if err := entitygraph.WriteAccuracyRecords(cfg.graphDir, accuracyRecords); err != nil {
				logger.Printf("write accuracy records err=%v", err)
			}
			// Keep the deduplicated index current for the next batch's
			// prevAccuracyRecords load -- accuracy.ndjson above is still the
			// raw append-only history, this is the fast query-of-truth cache.
			if err := upsertAccuracyRecords(cfg.accuracyDB, accuracyRecords, logger); err != nil {
				logger.Printf("accuracy_index: upsert err=%v", err)
			}
			accuracyReports = entitygraph.BuildAccuracyReports(accuracyRecords)
			logger.Printf("accuracy records=%d reports=%d (decay=%d auditor=%d ins_buy=%d ins_sell=%d cfo=%d dir_fric=%d div_cut=%d late=%d lead=%d bb_susp=%d abst=%d board_decay=%d div_raise=%d gov_det=%d gov_imp=%d gov_entr=%d abst_out=%d post_fail=%d bb_auth=%d broker=%d spec_div=%d eps_rev=%d comp=%d nom_rej=%d hi_trust=%d fam_ctrl=%d dir_link=%d peer_under=%d gov_health=%d long_tenure=%d)",
				len(accuracyRecords), len(accuracyReports), len(decayRecords), len(auditorRiskRecords),
				len(insiderBuyRecords), len(insiderSellRecords), len(cfoDeparturRecords), len(dirFrictionRecords),
				len(divCutRecords), len(lateFilingRecords), len(leadershipDepRecords), len(buybackSuspRecords),
				len(abstentionSpikeRecords), len(boardDecayRecords), len(divRaiseRecords), len(govDetRecords),
				len(govImpRecords), len(govEntrRecords), len(abstOutlierRecords), len(postFailureRecords),
				len(bbAuthRecords), len(brokerNonVoteRecords), len(specDivRecords), len(epsRevRecords),
				len(compConcernRecords), len(nomRejRecords), len(highTrustRecords), len(familyCtrlRecords),
				len(dirLinkRecords), len(peerUnderRecords), len(govHealthRecords), len(longTenureRecords))
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
			// Mirror the write into the long-lived in-memory historicalSignals
			// (graph-lifetime hoist) -- a fresh LoadSignals+Dedupe next batch
			// would have picked up exactly these newly-written signals; this
			// keeps that behavior without re-reading the file. Re-dedupe
			// (cheap: in-memory, not a disk scan) since allSignals can
			// legitimately repeat a signal_id already in historicalSignals
			// (e.g. director_long_tenure re-fires identically every batch).
			historicalSignals = entitygraph.DeduplicateSignals(append(historicalSignals, allSignals...))
		}
		if cfg.mongoClient != nil {
			if err := mongowriter.WriteEntities(ctx, cfg.mongoClient, cfg.mongoDB, graph, allSignals, logger); err != nil {
				logger.Printf("mongowriter err=%v", err)
			}
		}
		logger.Printf("batch complete processed=%d directors=%d signals=%d parse_errors=%d", processed, len(graph.Nodes), len(allSignals), len(parseErrors))

		obs := entitygraph.BuildObservation(processed, allSignals, parseErrors, len(graph.Nodes), totalProposals, directorsThisBatch, accuracyReports)
		if err := entitygraph.PublishObservation(obs, cfg.obsDir); err != nil {
			logger.Printf("publish observation err=%v", err)
		} else {
			logger.Printf("observation published dir=%s status=%s", cfg.obsDir, obs.Status)
		}
	}

	saveCursor(cfg.cursorPath, newCursor, logger)
	return newCursor, historicalSignals
}

// buildFilingIndexes scans all filing_discovered events in the store and returns
// two maps keyed by filing identity (CIK:ACCESSION):
//   - dates: identity → SEC filing date (for recovering FilingDate on old docs)
//   - forms: identity → effective form type (for detecting 8-K on legacy docs
//     where form="" source_type="press_release" due to a historical processor bug)
func buildFilingIndexes(ctx context.Context, store eventstore.EventStore, logger *log.Logger) (dates map[string]string, forms map[string]string) {
	dates = make(map[string]string)
	forms = make(map[string]string)
	err := store.Scan(ctx, 1, func(r eventstore.Record) error {
		if r.Event.Type != "filing_discovered" {
			return nil
		}
		var ev secwatch.FilingDiscoveredEvent
		if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
			return nil
		}
		if ev.CIK == "" || ev.AccessionNumber == "" {
			return nil
		}
		id := secwatch.FilingIdentity(ev.CIK, ev.AccessionNumber)
		if ev.FilingDate != "" {
			if _, exists := dates[id]; !exists {
				dates[id] = ev.FilingDate
			}
		}
		if form := ev.EffectiveForm(); form != "" {
			if _, exists := forms[id]; !exists {
				forms[id] = form
			}
		}
		return nil
	})
	if err != nil && logger != nil {
		logger.Printf("filing_indexes scan error: %v", err)
	}
	logger.Printf("filing_indexes loaded entries=%d", len(dates))
	return dates, forms
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
