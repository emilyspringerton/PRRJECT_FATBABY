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
			graphDir:     *graphDir,
			schd13Dir:    *schd13Dir,
			obsDir:       *obsDir,
			rulesPath:    *rulesPath,
			watchlistPath: *watchlistPath,
			cursorPath:   *cursorPath,
			batchSize:    *batchSize,
			cursor:       cursor,
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
	graphDir     string
	schd13Dir    string
	obsDir       string
	rulesPath    string
	watchlistPath string
	cursorPath   string
	batchSize    int
	cursor       uint64
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

	// Build a filing-date index from all filing_discovered events so we can
	// recover the SEC filing date for source documents that pre-date the
	// filing_date field in source_document_persisted payloads.
	filingDates := buildFilingDateIndex(ctx, store, logger)

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

	// Load previous governance health snapshots for trend scoring (deteriorating/improving).
	prevHealthHistory, err := entitygraph.LoadHealthHistory(cfg.graphDir)
	if err != nil {
		logger.Printf("load health history err=%v (non-fatal)", err)
	}

	// RSI: load historical accuracy records to calibrate governance health penalty weights.
	// AccuracyAdjustedPenalties scales down weights for signal types whose empirical
	// precision is below the configured threshold, closing the RSI feedback loop.
	prevAccuracyRecords, _ := entitygraph.LoadAccuracyRecords(cfg.graphDir)
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

		if len(result.DirectorVotes) == 0 {
			logger.Printf("no_directors seq=%d identity=%s", r.Sequence, doc.Identity)
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

// buildFilingDateIndex scans all filing_discovered events in the store and
// returns a map from filing identity (CIK:ACCESSION) to SEC filing date.
// This is used to recover the correct filing date for source_document_persisted
// records that pre-date the FilingDate field.
func buildFilingDateIndex(ctx context.Context, store eventstore.EventStore, logger *log.Logger) map[string]string {
	index := make(map[string]string)
	from := uint64(1)
	for {
		recs, err := store.ReadFrom(ctx, from, 512)
		if err != nil || len(recs) == 0 {
			break
		}
		for _, r := range recs {
			if r.Event.Type != "filing_discovered" {
				continue
			}
			var ev secwatch.FilingDiscoveredEvent
			if err := json.Unmarshal(r.Event.Data, &ev); err != nil {
				continue
			}
			if ev.FilingDate == "" || ev.CIK == "" || ev.AccessionNumber == "" {
				continue
			}
			id := secwatch.FilingIdentity(ev.CIK, ev.AccessionNumber)
			if _, exists := index[id]; !exists {
				index[id] = ev.FilingDate
			}
		}
		from = recs[len(recs)-1].Sequence + 1
	}
	logger.Printf("filing_date_index loaded entries=%d", len(index))
	return index
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
