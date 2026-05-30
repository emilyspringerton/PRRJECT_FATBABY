package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/newssite"
	"github.com/example/prrject-fatbaby/internal/newssite/catalog"
	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

func main() {
	storeRoot := flag.String("store", "var/secwatch", "path to eventstore root")
	graphDir  := flag.String("graph-dir", "var/entity-graph", "path to entity-graph directory (empty to disable)")
	addr      := flag.String("addr", ":8082", "listen address")
	readTO    := flag.Duration("read-timeout", 10*time.Second, "")
	writeTO   := flag.Duration("write-timeout", 15*time.Second, "")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h := newssite.NewHandler(store, logger)

	// ── Entity-graph store (fast: just reads NDJSON files) ────────────────────
	var gs *graphread.Store
	if *graphDir != "" {
		gs = graphread.NewStore(*graphDir)
		if err := gs.Refresh(); err != nil {
			logger.Printf("newssite graph load: %v", err)
		} else {
			logger.Printf("newssite graph loaded from %s", *graphDir)
		}
		h.SetGraphStore(gs)
		done := make(chan struct{})
		gs.StartRefresh(30*time.Second, func(f string, a ...any) { logger.Printf(f, a...) }, done)
		defer close(done)
	}

	// ── Signal index + doc index — built in parallel ───────────────────────────
	sigIdx := signalindex.NewIndex()
	docIdx := docindex.NewIndex()

	var buildWG sync.WaitGroup
	buildWG.Add(2)
	go func() {
		defer buildWG.Done()
		if err := signalindex.Build(ctx, store, sigIdx, logger); err != nil {
			logger.Printf("newssite signalindex build: %v", err)
		} else {
			logger.Printf("newssite signalindex built depth=%d", sigIdx.Depth())
		}
	}()
	go func() {
		defer buildWG.Done()
		if err := docindex.Build(ctx, store, docIdx, logger); err != nil {
			logger.Printf("newssite docindex build: %v", err)
		} else {
			logger.Printf("newssite docindex built tickers=%d", len(docIdx.KnownTickers()))
		}
	}()

	h.SetSignalIndex(sigIdx)
	h.SetDocIndex(docIdx)

	// ── Catalog — built once indexes are ready, refreshed on a timer ──────────
	cat := catalog.New()
	h.SetCatalog(cat)

	go func() {
		buildWG.Wait() // wait for both index builds to finish
		today := time.Now().UTC().Format("2006-01-02")
		cat.Build(sigIdx, gs, docIdx, today)
		logger.Printf("newssite catalog built tickers=%d", len(cat.AllSymbols()))

		// Tail new records into the indexes after initial build.
		go signalindex.Tail(ctx, store, sigIdx, 30*time.Second, logger)
		go docindex.Tail(ctx, store, docIdx, 30*time.Second, logger)

		// Rebuild catalog every 30s.
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cat.Build(sigIdx, gs, docIdx, time.Now().UTC().Format("2006-01-02"))
			}
		}
	}()

	// ── HTTP server starts immediately; indexes fill in behind it ─────────────
	srv := &http.Server{
		Addr:         *addr,
		Handler:      h,
		ReadTimeout:  *readTO,
		WriteTimeout: *writeTO,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	logger.Printf("newssite listening addr=%s store=%s graph-dir=%s", *addr, *storeRoot, *graphDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("listen: %v", err)
	}
}
