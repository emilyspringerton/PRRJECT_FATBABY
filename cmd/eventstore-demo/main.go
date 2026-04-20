package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

func main() {
	ctx := context.Background()
	root := filepath.Join(os.TempDir(), "sec-eventstore-demo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatal(err)
	}

	store, err := eventstore.NewFileStore(root)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close error: %v", err)
		}
	}()

	events := []eventstore.Event{
		{
			ID:           "demo-evt-1",
			Type:         "filing_discovered",
			OccurredAt:   time.Now().UTC(),
			AggregateKey: "0000320193-25-000073",
			Source:       "sec-edgar",
			Data:         mustJSON(map[string]any{"form": "10-K", "cik": "320193"}),
		},
		{
			ID:           "demo-evt-2",
			Type:         "filing_fetched",
			OccurredAt:   time.Now().UTC(),
			AggregateKey: "0000320193-25-000073",
			Source:       "sec-fetcher",
			Data: mustJSON(map[string]any{
				"url":   "https://www.sec.gov/Archives/edgar/data/320193/000032019325000073/aapl-20240928x10k.htm",
				"bytes": 1834212,
			}),
		},
	}

	recs, err := store.Append(ctx, events...)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Appended records:")
	for _, r := range recs {
		fmt.Printf("  seq=%d type=%s id=%s\n", r.Sequence, r.Event.Type, r.Event.ID)
	}

	all, err := store.ReadFrom(ctx, 1, 100)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nRead back records:")
	for _, r := range all {
		fmt.Printf("  seq=%d type=%s data=%s\n", r.Sequence, r.Event.Type, string(r.Event.Data))
	}

	fmt.Printf("\nStore root: %s\n", root)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
