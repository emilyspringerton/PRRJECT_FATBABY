// Package mongowriter writes entity-graph data to MongoDB as flattened entity documents.
// One document per tracked company (keyed by ticker). Documents are rebuilt from the
// in-memory graph + signal list after each entity-graph batch and upserted via ReplaceOne.
//
// MongoDB is NOT the source of truth — the eventstore is. MongoDB is a read cache for the
// Ask Emily query layer. The whole document can be rebuilt from scratch by replaying the
// eventstore through entity-graph.
//
// Activated only when MONGODB_URL is non-empty. entity-graph calls WriteEntities after
// each successful batch; no-op if client is nil.
package mongowriter

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

// EntityDocument is the MongoDB document stored in the `entities` collection.
// One document per tracked company, keyed by `ticker`.
type EntityDocument struct {
	Ticker          string            `bson:"ticker"`
	Name            string            `bson:"name"`
	EntityType      string            `bson:"entity_type"`
	UpdatedAt       time.Time         `bson:"updated_at"`
	SignalScore     float64           `bson:"signal_score"`
	Directors       []DirectorEntry   `bson:"directors"`
	Auditor         AuditorEntry      `bson:"auditor"`
	GovernanceEvents []GovernanceEvent `bson:"governance_events"`
}

// DirectorEntry is one director/executive row in the entity document.
type DirectorEntry struct {
	Name          string   `bson:"name"`
	Role          string   `bson:"role"`
	FirstFiling   string   `bson:"first_filing"`
	LastFiling    string   `bson:"last_filing"`
	FilingCount   int      `bson:"filing_count"`
	Centrality    int      `bson:"centrality"`
	Flags         []string `bson:"flags,omitempty"`
}

// AuditorEntry holds the last known auditor for the company.
type AuditorEntry struct {
	Name      string `bson:"name"`
	Since     string `bson:"since"`
	ChangedAt string `bson:"changed_at,omitempty"`
}

// GovernanceEvent is one governance event row (sourced from a signal).
type GovernanceEvent struct {
	SignalType  string  `bson:"signal_type"`
	Date        string  `bson:"date"`
	Headline    string  `bson:"headline"`
	FilingID    string  `bson:"filing_id"`
	SignalScore float64 `bson:"signal_score"`
}

// Connect opens a MongoDB client. Returns nil if mongoURL is empty (graceful degradation).
func Connect(ctx context.Context, mongoURL string) (*mongo.Client, error) {
	if mongoURL == "" {
		return nil, nil
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURL))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	return client, nil
}

// WriteEntities builds entity documents from graph + signals and upserts them to MongoDB.
// dbName defaults to "fatbaby" if empty.
func WriteEntities(ctx context.Context, client *mongo.Client, dbName string, graph *entitygraph.Graph, signals []entitygraph.Signal, logger *log.Logger) error {
	if client == nil {
		return nil
	}
	if dbName == "" {
		dbName = "fatbaby"
	}
	coll := client.Database(dbName).Collection("entities")

	docs := buildDocuments(graph, signals)
	if len(docs) == 0 {
		return nil
	}

	upserted := 0
	for _, doc := range docs {
		filter := bson.M{"ticker": doc.Ticker}
		opts := options.Replace().SetUpsert(true)
		if _, err := coll.ReplaceOne(ctx, filter, doc, opts); err != nil {
			logger.Printf("mongowriter upsert %s: %v", doc.Ticker, err)
			continue
		}
		upserted++
	}
	logger.Printf("mongowriter upserted=%d tickers=%d", upserted, len(docs))
	return nil
}

// buildDocuments converts the in-memory graph + signals into one EntityDocument per ticker.
func buildDocuments(graph *entitygraph.Graph, signals []entitygraph.Signal) []*EntityDocument {
	// Index signals by ticker.
	sigsByTicker := make(map[string][]entitygraph.Signal)
	for _, s := range signals {
		if s.Ticker != "" {
			sigsByTicker[s.Ticker] = append(sigsByTicker[s.Ticker], s)
		}
	}

	// Build the set of tickers: union of auditor keys + signal tickers + node filing tickers.
	tickers := make(map[string]struct{})
	for t := range graph.Auditors {
		tickers[t] = struct{}{}
	}
	for t := range sigsByTicker {
		tickers[t] = struct{}{}
	}
	for _, node := range graph.Nodes {
		for _, f := range node.Filings {
			tickers[f.Ticker] = struct{}{}
		}
	}

	now := time.Now().UTC()
	var docs []*EntityDocument

	for ticker := range tickers {
		doc := &EntityDocument{
			Ticker:     ticker,
			EntityType: "company",
			UpdatedAt:  now,
		}

		// Directors: collect nodes that appear at this ticker.
		directorFlags := buildDirectorFlags(sigsByTicker[ticker])
		for _, node := range graph.Nodes {
			var latestFiling string
			var firstFiling string
			tickerCount := 0
			for _, f := range node.Filings {
				if f.Ticker != ticker {
					continue
				}
				tickerCount++
				if firstFiling == "" || f.FilingDate < firstFiling {
					firstFiling = f.FilingDate
				}
				if f.FilingDate > latestFiling {
					latestFiling = f.FilingDate
				}
			}
			if tickerCount == 0 {
				continue
			}
			flags := directorFlags[node.Name]
			doc.Directors = append(doc.Directors, DirectorEntry{
				Name:        node.Name,
				Role:        string(node.Type),
				FirstFiling: firstFiling,
				LastFiling:  latestFiling,
				FilingCount: tickerCount,
				Centrality:  node.Centrality,
				Flags:       flags,
			})
		}

		// Sort directors by last filing descending.
		sort.Slice(doc.Directors, func(i, j int) bool {
			return doc.Directors[i].LastFiling > doc.Directors[j].LastFiling
		})

		// Auditor.
		if aud, ok := graph.Auditors[ticker]; ok {
			doc.Auditor = AuditorEntry{
				Name:  aud.Auditor,
				Since: aud.FilingDate,
			}
		}

		// Governance events from signals (keep last 50 per ticker, sorted by filing date desc).
		tickerSigs := sigsByTicker[ticker]
		sort.Slice(tickerSigs, func(i, j int) bool {
			return tickerSigs[i].FilingDate > tickerSigs[j].FilingDate
		})
		if len(tickerSigs) > 50 {
			tickerSigs = tickerSigs[:50]
		}
		maxScore := 0.0
		for _, s := range tickerSigs {
			if s.Score > maxScore {
				maxScore = s.Score
			}
			filingID := ""
			if s.Metadata != nil {
				filingID = s.Metadata["accession_number"]
			}
			doc.GovernanceEvents = append(doc.GovernanceEvents, GovernanceEvent{
				SignalType:  string(s.Type),
				Date:        s.FilingDate,
				Headline:    s.Interpretation,
				FilingID:    filingID,
				SignalScore: s.Score,
			})
		}
		doc.SignalScore = maxScore

		docs = append(docs, doc)
	}

	return docs
}

// buildDirectorFlags returns signal-derived flags per director name for a given ticker's signals.
func buildDirectorFlags(signals []entitygraph.Signal) map[string][]string {
	flags := make(map[string][]string)
	for _, s := range signals {
		name := s.Entity
		if name == "" && s.Metadata != nil {
			name = s.Metadata["director_name"]
		}
		if name == "" {
			continue
		}
		flagType := signalToFlag(string(s.Type))
		if flagType == "" {
			continue
		}
		for _, existing := range flags[name] {
			if existing == flagType {
				goto next
			}
		}
		flags[name] = append(flags[name], flagType)
	next:
	}
	return flags
}

func signalToFlag(signalType string) string {
	switch signalType {
	case "director_friction":
		return "friction"
	case "compensation_concern":
		return "compensation_concern"
	case "director_decay":
		return "decay"
	case "director_long_tenure":
		return "long_tenure"
	case "nomination_rejection":
		return "nomination_rejected"
	case "insider_buy":
		return "insider_buy"
	case "insider_sell_cluster":
		return "insider_sell_cluster"
	case "leadership_departure":
		return "departed"
	case "cfo_departure":
		return "departed"
	}
	return ""
}
