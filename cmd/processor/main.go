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
	"github.com/example/prrject-fatbaby/internal/processor"
)

func main() {
	storeRoot := flag.String("store", filepath.Join("var", "secwatch"), "event store root")
	workers := flag.Int("workers", 4, "processor worker count")
	pollInterval := flag.Duration("poll-interval", 15*time.Second, "polling interval")
	ua := flag.String("user-agent", "prrject-fatbaby-secwatch/0.1 (contact: secops@example.com)", "SEC-compliant user-agent")
	maxDocBytes := flag.Int64("max-doc-bytes", 16<<20, "max filing document bytes to ingest")
	archetypeURL := flag.String("archetype-engine", os.Getenv("ARCHETYPE_ENGINE_URL"), "THE_FIELD archetype engine URL (default: disabled)")
	// Real gap found live 2026-08-05: a single Anthropic billing lapse silently dropped every
	// filing seen while it lasted -- worker.go's handleOne persists source_document_persisted
	// BEFORE calling Provider.AnalyzeText, but returns early with no signal_generated at all if
	// AnalyzeText errors, and the poll loop's cursor has already moved past that filing by the
	// next attempt (no automatic retry). Founder: "we dont need the llm in the critical path of
	// the data... figure out a way around it for now" -> "we dont want llm generated data like
	// that in the critical path for now." LLM-backed analysis (HaikuProvider/ArchetypeProvider,
	// unchanged, still real code) now requires this explicit opt-in instead of activating itself
	// whenever ANTHROPIC_API_KEY happens to be set -- HeuristicProvider (never fails, no network
	// call) is the default.
	enableLLM := flag.Bool("enable-llm", false, "use Haiku/THE_FIELD for signal analysis instead of the default heuristic provider (opt-in: a billing lapse silently drops filings while active, see this flag's own doc comment)")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open event store: %v", err)
	}
	defer store.Close()

	var prov processor.Provider
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if *enableLLM && apiKey != "" && *archetypeURL != "" {
		haiku := processor.NewHaikuProvider(apiKey)
		prov = processor.NewArchetypeProvider(*archetypeURL, haiku, haiku)
		logger.Printf("processor provider=archetype engine=%s fallback=haiku", *archetypeURL)
	} else if *enableLLM && apiKey != "" {
		prov = processor.NewHaikuProvider(apiKey)
		logger.Printf("processor provider=haiku model=%s", "claude-haiku-4-5-20251001")
	} else {
		prov = processor.NewHeuristicProvider()
		if *enableLLM {
			logger.Printf("processor provider=heuristic (-enable-llm set but ANTHROPIC_API_KEY is empty)")
		} else {
			logger.Printf("processor provider=heuristic (default -- pass -enable-llm to use haiku/archetype instead)")
		}
	}

	if b, _ := json.Marshal(map[string]any{"workers": *workers, "poll_interval": pollInterval.String()}); len(b) > 0 {
		logger.Printf("processor starting %s", b)
		logger.Printf("data directory %s", *storeRoot)
	}
	if err := processor.Run(ctx, processor.WorkerConfig{Store: store, Provider: prov, Logger: logger, Workers: *workers, PollInterval: *pollInterval, UserAgent: *ua, MaxDocBytes: *maxDocBytes}); err != nil && err != context.Canceled {
		logger.Fatalf("processor run failed: %v", err)
	}
}
