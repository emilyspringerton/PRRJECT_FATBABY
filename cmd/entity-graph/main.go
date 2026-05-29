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
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "secwatch event store root")
	graphDir := flag.String("graph-dir", filepath.Join("var", "entity-graph"), "output directory for graph NDJSON files")
	obsDir := flag.String("obs-dir", filepath.Join("var", "emily-observations"), "observation output directory")
	rulesPath := flag.String("rules", filepath.Join("config", "entity-graph-rules.json"), "signal scoring rules (hot-reloaded each batch)")
	pollInterval := flag.Duration("poll-interval", 30*time.Second, "how often to poll the event store")
	batchSize := flag.Int("batch-size", 256, "max events to read per poll")
	cursorPath := flag.String("cursor", filepath.Join("var", "entity-graph", ".cursor"), "file storing last-processed sequence number")
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

	logger.Printf("starting poll_interval=%s store=%s graph_dir=%s", *pollInterval, *storeRoot, *graphDir)

	cursor := loadCursor(*cursorPath, logger)

	for {
		cursor = runBatch(ctx, store, logger, runConfig{
			graphDir:   *graphDir,
			obsDir:     *obsDir,
			rulesPath:  *rulesPath,
			cursorPath: *cursorPath,
			batchSize:  *batchSize,
			cursor:     cursor,
		})

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

	graph := entitygraph.NewGraph()
	if err := graph.LoadNodesFromDir(cfg.graphDir); err != nil {
		logger.Printf("load existing nodes err=%v", err)
	}

	var (
		allSignals         []entitygraph.Signal
		parseErrors        []entitygraph.ParseError
		processed          int
		totalProposals     int
	)

	for _, r := range recs {
		if r.Event.Type != "source_document_persisted" {
			continue
		}
		var doc intelligence.SourceDocument
		if err := json.Unmarshal(r.Event.Data, &doc); err != nil {
			logger.Printf("unmarshal source_document seq=%d err=%v", r.Sequence, err)
			continue
		}
		if doc.Form != "8-K" {
			continue
		}

		logger.Printf("processing seq=%d ticker=%s identity=%s chars=%d", r.Sequence, doc.Ticker, doc.Identity, doc.CleanedCharCount)
		processed++

		result, err := entitygraph.ParseItem507(doc.CleanedText)
		if err != nil {
			logger.Printf("parse_item507 seq=%d identity=%s err=%v", r.Sequence, doc.Identity, err)
			parseErrors = append(parseErrors, entitygraph.ParseError{
				Ticker:   doc.Ticker,
				Identity: doc.Identity,
				Error:    err.Error(),
			})
			continue
		}

		if len(result.DirectorVotes) == 0 {
			logger.Printf("no_directors seq=%d identity=%s", r.Sequence, doc.Identity)
			continue
		}

		filingDate := time.Now().UTC().Format("2006-01-02")
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

		dirSigs := entitygraph.ScoreDirectorVotes(result.DirectorVotes, doc.Ticker, rules)
		propSigs := entitygraph.ScoreProposals(result.Proposals, doc.Ticker, rules)
		allSignals = append(allSignals, dirSigs...)
		allSignals = append(allSignals, propSigs...)
		totalProposals += len(result.Proposals)
		logger.Printf("signals ticker=%s dir_signals=%d prop_signals=%d proposals=%d", doc.Ticker, len(dirSigs), len(propSigs), len(result.Proposals))
	}

	newCursor := recs[len(recs)-1].Sequence + 1

	if processed > 0 {
		if err := graph.FlushNodes(cfg.graphDir); err != nil {
			logger.Printf("flush nodes err=%v", err)
		}
		if err := graph.FlushEdges(cfg.graphDir); err != nil {
			logger.Printf("flush edges err=%v", err)
		}
		if len(allSignals) > 0 {
			if err := entitygraph.WriteSignals(cfg.graphDir, allSignals); err != nil {
				logger.Printf("write signals err=%v", err)
			}
		}
		logger.Printf("batch complete processed=%d directors=%d signals=%d parse_errors=%d", processed, len(graph.Nodes), len(allSignals), len(parseErrors))

		obs := entitygraph.BuildObservation(processed, allSignals, parseErrors, len(graph.Nodes), totalProposals)
		if err := entitygraph.PublishObservation(obs, cfg.obsDir); err != nil {
			logger.Printf("publish observation err=%v", err)
		} else {
			logger.Printf("observation published dir=%s status=%s", cfg.obsDir, obs.Status)
		}
	}

	saveCursor(cfg.cursorPath, newCursor, logger)
	return newCursor
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
