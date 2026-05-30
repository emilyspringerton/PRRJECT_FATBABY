package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/newssite"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
)

func main() {
	storeRoot  := flag.String("store", "var/secwatch", "path to eventstore root")
	graphDir   := flag.String("graph-dir", "var/entity-graph", "path to entity-graph directory (empty to disable)")
	addr       := flag.String("addr", ":8082", "listen address")
	readTO     := flag.Duration("read-timeout", 10*time.Second, "")
	writeTO    := flag.Duration("write-timeout", 15*time.Second, "")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	h := newssite.NewHandler(store, logger)

	if *graphDir != "" {
		gs := graphread.NewStore(*graphDir)
		if err := gs.Refresh(); err != nil {
			logger.Printf("newssite graph initial load: %v (signals won't appear until the entity-graph processor runs)", err)
		} else {
			logger.Printf("newssite graph loaded from %s", *graphDir)
		}
		h.SetGraphStore(gs)
		done := make(chan struct{})
		gs.StartRefresh(30*time.Second, func(f string, a ...any) { logger.Printf(f, a...) }, done)
		defer close(done)
	}

	srv := &http.Server{
		Addr:         *addr,
		Handler:      h,
		ReadTimeout:  *readTO,
		WriteTimeout: *writeTO,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
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
