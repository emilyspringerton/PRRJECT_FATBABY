package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/processor"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

type stubProvider struct{}

func (p *stubProvider) AnalyzeText(ctx context.Context, text string) (*intelligence.Signal, error) {
	_ = ctx
	return &intelligence.Signal{SignalType: "Other", Importance: 5, Sentiment: 0, Summary: "Stub analysis result.", ImpactAnalysis: fmt.Sprintf("Input length: %d chars", len(text)), RawMetadata: map[string]string{"provider": "stub"}}, nil
}

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "event store root")
	workers := flag.Int("workers", 4, "processor worker count")
	pollInterval := flag.Duration("poll-interval", 15*time.Second, "polling interval")
	ua := flag.String("user-agent", "prrject-fatbaby-secwatch/0.1 (contact: secops@example.com)", "SEC-compliant user-agent")
	maxDocBytes := flag.Int64("max-doc-bytes", 4<<20, "max filing document bytes to ingest")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open event store: %v", err)
	}
	defer store.Close()

	if b, _ := json.Marshal(map[string]any{"workers": *workers, "poll_interval": pollInterval.String()}); len(b) > 0 {
		logger.Printf("processor starting %s", b)
		logger.Printf("data directory %s", storeRoot)
	}
	if err := processor.Run(ctx, processor.WorkerConfig{Store: store, Provider: &stubProvider{}, Logger: logger, Workers: *workers, PollInterval: *pollInterval, UserAgent: *ua, MaxDocBytes: *maxDocBytes}); err != nil && err != context.Canceled {
		logger.Fatalf("processor run failed: %v", err)
	}
}
