// Package kgraph implements the EINHORN INDEX knowledge graph store.
//
// Nodes: entities (company, person, product, location, price_point, event, technology)
// Edges: typed relationships between nodes (sells, supplies, priced_at, etc.)
//
// Backend: MongoDB collections kg_nodes + kg_edges.
// IDs: SHA256 of "entity_type:canonical_name" (nodes) and "subject_id:predicate:object_id" (edges).
// All writes are upserts. All reads include source provenance.
//
// S138-04: knowledge graph store; S138-03: entity extraction entry point.
package kgraph

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EntityType classifies a knowledge graph node.
type EntityType string

const (
	EntityCompany    EntityType = "company"
	EntityPerson     EntityType = "person"
	EntityProduct    EntityType = "product"
	EntityLocation   EntityType = "location"
	EntityPricePoint EntityType = "price_point"
	EntityEvent      EntityType = "event"
	EntityTechnology EntityType = "technology"
)

// Node is one vertex in the knowledge graph.
type Node struct {
	ID            string            `bson:"_id"`
	EntityType    EntityType        `bson:"entity_type"`
	CanonicalName string            `bson:"canonical_name"`
	Aliases       []string          `bson:"aliases"`
	Properties    map[string]any    `bson:"properties"`
	FirstSeen     time.Time         `bson:"first_seen"`
	LastUpdated   time.Time         `bson:"last_updated"`
	SourceURLs    []string          `bson:"source_urls"`
}

// NodeID returns the deterministic node ID.
func NodeID(entityType EntityType, canonicalName string) string {
	h := sha256.Sum256([]byte(strings.ToLower(string(entityType)) + ":" + strings.ToLower(canonicalName)))
	return fmt.Sprintf("%x", h)[:32]
}

// Edge is one directed relationship in the knowledge graph.
type Edge struct {
	ID          string    `bson:"_id"`
	SubjectID   string    `bson:"subject_id"`
	Predicate   string    `bson:"predicate"`
	ObjectID    string    `bson:"object_id"`
	Confidence  float64   `bson:"confidence"`
	SourceURL   string    `bson:"source_url"`
	ExtractedAt time.Time `bson:"extracted_at"`
}

// EdgeID returns the deterministic edge ID.
func EdgeID(subjectID, predicate, objectID string) string {
	h := sha256.Sum256([]byte(subjectID + ":" + predicate + ":" + objectID))
	return fmt.Sprintf("%x", h)[:32]
}

// GraphResult is returned by KnowledgeQuery.
type GraphResult struct {
	SubjectName string    `json:"subject_name"`
	Predicate   string    `json:"predicate"`
	ObjectName  string    `json:"object_name"`
	Confidence  float64   `json:"confidence"`
	SourceURL   string    `json:"source_url"`
	ExtractedAt time.Time `json:"extracted_at"`
}

// Store is the knowledge graph MongoDB backend.
type Store struct {
	nodes *mongo.Collection
	edges *mongo.Collection
}

// New creates a Store using the given MongoDB database.
func New(db *mongo.Database) *Store {
	return &Store{
		nodes: db.Collection("kg_nodes"),
		edges: db.Collection("kg_edges"),
	}
}

// EnsureIndexes creates the required MongoDB indexes.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	_, err := s.nodes.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "entity_type", Value: 1}}},
		{Keys: bson.D{{Key: "canonical_name", Value: 1}}},
		{Keys: bson.D{{Key: "aliases", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("kgraph: node indexes: %w", err)
	}
	_, err = s.edges.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "subject_id", Value: 1}}},
		{Keys: bson.D{{Key: "object_id", Value: 1}}},
		{Keys: bson.D{{Key: "predicate", Value: 1}}},
		{Keys: bson.D{{Key: "subject_id", Value: 1}, {Key: "predicate", Value: 1}}},
	})
	if err != nil {
		return fmt.Errorf("kgraph: edge indexes: %w", err)
	}
	return nil
}

// UpsertNode upserts a node by its deterministic ID.
func (s *Store) UpsertNode(ctx context.Context, n Node) error {
	if n.ID == "" {
		n.ID = NodeID(n.EntityType, n.CanonicalName)
	}
	now := time.Now().UTC()
	n.LastUpdated = now
	filter := bson.M{"_id": n.ID}
	update := bson.M{
		"$set": bson.M{
			"entity_type":    n.EntityType,
			"canonical_name": n.CanonicalName,
			"properties":     n.Properties,
			"last_updated":   n.LastUpdated,
		},
		"$setOnInsert": bson.M{"first_seen": now},
		"$addToSet": bson.M{
			"aliases":     bson.M{"$each": n.Aliases},
			"source_urls": bson.M{"$each": n.SourceURLs},
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := s.nodes.UpdateOne(ctx, filter, update, opts)
	return err
}

// UpsertEdge upserts a directed edge.
func (s *Store) UpsertEdge(ctx context.Context, e Edge) error {
	if e.ID == "" {
		e.ID = EdgeID(e.SubjectID, e.Predicate, e.ObjectID)
	}
	e.ExtractedAt = time.Now().UTC()
	filter := bson.M{"_id": e.ID}
	update := bson.M{
		"$set": bson.M{
			"subject_id":   e.SubjectID,
			"predicate":    e.Predicate,
			"object_id":    e.ObjectID,
			"confidence":   e.Confidence,
			"source_url":   e.SourceURL,
			"extracted_at": e.ExtractedAt,
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err := s.edges.UpdateOne(ctx, filter, update, opts)
	return err
}

// Query returns edges where subject or object canonical_name matches entityName.
// If predicate is non-empty, filters by predicate.
func (s *Store) Query(ctx context.Context, entityName, predicate string, limit int) ([]GraphResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// Resolve entity name to node ID.
	var node Node
	filter := bson.M{
		"$or": []bson.M{
			{"canonical_name": bson.M{"$regex": entityName, "$options": "i"}},
			{"aliases": bson.M{"$regex": entityName, "$options": "i"}},
		},
	}
	if err := s.nodes.FindOne(ctx, filter).Decode(&node); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	// Find edges.
	edgeFilter := bson.M{
		"$or": []bson.M{
			{"subject_id": node.ID},
			{"object_id": node.ID},
		},
	}
	if predicate != "" {
		edgeFilter["predicate"] = predicate
	}
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "confidence", Value: -1}})
	cursor, err := s.edges.Find(ctx, edgeFilter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var edges []Edge
	if err := cursor.All(ctx, &edges); err != nil {
		return nil, err
	}

	// Hydrate node names for subject/object.
	var results []GraphResult
	for _, e := range edges {
		var subj, obj Node
		_ = s.nodes.FindOne(ctx, bson.M{"_id": e.SubjectID}).Decode(&subj)
		_ = s.nodes.FindOne(ctx, bson.M{"_id": e.ObjectID}).Decode(&obj)
		results = append(results, GraphResult{
			SubjectName: subj.CanonicalName,
			Predicate:   e.Predicate,
			ObjectName:  obj.CanonicalName,
			Confidence:  e.Confidence,
			SourceURL:   e.SourceURL,
			ExtractedAt: e.ExtractedAt,
		})
	}
	return results, nil
}
