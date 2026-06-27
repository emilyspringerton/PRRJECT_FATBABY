// extractor.go — S138-03 entity extraction pipeline
//
// Takes crawled document text, calls claude-haiku to extract named entities
// and relationships, then upserts the results into the knowledge graph.
//
// Output JSON schema (haiku is instructed to produce this):
//
//	{
//	  "nodes": [{"entity_type":"company","canonical_name":"StickerMule","aliases":["Sticker Mule"],"properties":{}}],
//	  "edges": [{"subject":"StickerMule","predicate":"sells","object":"die-cut vinyl sticker","confidence":0.9}]
//	}

package kgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const extractorModel = "claude-haiku-4-5-20251001"
const anthropicURL = "https://api.anthropic.com/v1/messages"

const extractionSystemPrompt = `You are an entity extraction engine. Given a text excerpt, extract all named entities and relationships.

Output a single valid JSON object with two arrays:
- "nodes": each with fields: entity_type (company|person|product|location|price_point|event|technology), canonical_name, aliases (array of strings), properties (object)
- "edges": each with fields: subject (canonical_name), predicate (sells|supplies|employs|located_in|competes_with|priced_at|acquired|cited_in), object (canonical_name), confidence (0.0-1.0)

Rules:
- canonical_name must be a stable identifier (e.g. company legal name, product model, person full name)
- Only extract high-confidence entities and relationships
- Do not invent relationships not present in the text
- Output ONLY the JSON object, no other text`

// ExtractionResult is the structured output from haiku entity extraction.
type ExtractionResult struct {
	Nodes []struct {
		EntityType    string         `json:"entity_type"`
		CanonicalName string         `json:"canonical_name"`
		Aliases       []string       `json:"aliases"`
		Properties    map[string]any `json:"properties"`
	} `json:"nodes"`
	Edges []struct {
		Subject    string  `json:"subject"`
		Predicate  string  `json:"predicate"`
		Object     string  `json:"object"`
		Confidence float64 `json:"confidence"`
	} `json:"edges"`
}

// Extractor extracts entities from text using claude-haiku and upserts into the KG.
type Extractor struct {
	Store      *Store
	APIKey     string
	HTTPClient *http.Client
}

// NewExtractor returns an Extractor.
func NewExtractor(store *Store, apiKey string) *Extractor {
	return &Extractor{
		Store:      store,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 45 * time.Second},
	}
}

// ExtractAndUpsert extracts entities from text and upserts them into the graph.
// sourceURL is used as provenance for all generated nodes and edges.
// Returns the number of nodes and edges upserted.
func (e *Extractor) ExtractAndUpsert(ctx context.Context, text, sourceURL string) (int, int, error) {
	if len(text) > 8000 {
		text = text[:8000]
	}

	extracted, err := e.callHaiku(ctx, text)
	if err != nil {
		return 0, 0, fmt.Errorf("haiku extraction: %w", err)
	}

	// Build canonical name → node ID map for edge resolution.
	nameToID := make(map[string]string)

	// Upsert nodes.
	var nodeCount int
	for _, n := range extracted.Nodes {
		if n.CanonicalName == "" || n.EntityType == "" {
			continue
		}
		node := Node{
			EntityType:    EntityType(n.EntityType),
			CanonicalName: n.CanonicalName,
			Aliases:       n.Aliases,
			Properties:    n.Properties,
			SourceURLs:    []string{sourceURL},
		}
		node.ID = NodeID(node.EntityType, node.CanonicalName)
		nameToID[strings.ToLower(n.CanonicalName)] = node.ID
		if err := e.Store.UpsertNode(ctx, node); err != nil {
			continue
		}
		nodeCount++
	}

	// Upsert edges.
	var edgeCount int
	for _, edge := range extracted.Edges {
		subjectID, ok := nameToID[strings.ToLower(edge.Subject)]
		if !ok {
			continue
		}
		objectID, ok2 := nameToID[strings.ToLower(edge.Object)]
		if !ok2 {
			continue
		}
		e2 := Edge{
			SubjectID:  subjectID,
			Predicate:  edge.Predicate,
			ObjectID:   objectID,
			Confidence: edge.Confidence,
			SourceURL:  sourceURL,
		}
		e2.ID = EdgeID(subjectID, edge.Predicate, objectID)
		if err := e.Store.UpsertEdge(ctx, e2); err != nil {
			continue
		}
		edgeCount++
	}

	return nodeCount, edgeCount, nil
}

func (e *Extractor) callHaiku(ctx context.Context, text string) (*ExtractionResult, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	reqBody, err := json.Marshal(map[string]any{
		"model":      extractorModel,
		"max_tokens": 1024,
		"system":     extractionSystemPrompt,
		"messages":   []message{{Role: "user", Content: text}},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", e.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var ar struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("anthropic response unmarshal: %w", err)
	}
	if ar.Error != nil {
		return nil, fmt.Errorf("anthropic error: %s", ar.Error.Message)
	}
	if len(ar.Content) == 0 {
		return nil, fmt.Errorf("anthropic empty response")
	}

	text2 := ar.Content[0].Text
	// Strip markdown code fences if present.
	text2 = strings.TrimPrefix(strings.TrimSpace(text2), "```json")
	text2 = strings.TrimPrefix(text2, "```")
	text2 = strings.TrimSuffix(text2, "```")
	text2 = strings.TrimSpace(text2)

	var result ExtractionResult
	if err := json.Unmarshal([]byte(text2), &result); err != nil {
		return nil, fmt.Errorf("extraction JSON parse: %w — raw: %s", err, text2[:min(200, len(text2))])
	}
	return &result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
