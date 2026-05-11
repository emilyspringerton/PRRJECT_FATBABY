package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	store         eventstore.EventStore
	pollInterval  time.Duration
	initialEvents int
}

func New(store eventstore.EventStore) *Server {
	return &Server{store: store, pollInterval: 1 * time.Second, initialEvents: 50}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("sub static fs: %v", err))
	}
	mux.Handle("/", http.FileServer(http.FS(staticRoot)))
	mux.HandleFunc("/events", s.handleEvents)
	return mux
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	lastSeq, err := s.sendInitial(ctx, w)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("send initial events: %v", err)
		}
		return
	}

	_, _ = w.Write([]byte("retry: 2000\n\n"))
	flusher.Flush()

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			recs, err := s.store.ReadFrom(ctx, lastSeq+1, 200)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				log.Printf("poll events: %v", err)
				return
			}
			for _, rec := range recs {
				if err := writeSSE(w, rec); err != nil {
					return
				}
				lastSeq = rec.Sequence
			}
			flusher.Flush()
		}
	}
}

func (s *Server) sendInitial(ctx context.Context, w http.ResponseWriter) (uint64, error) {
	latest, err := s.store.LatestSequence(ctx)
	if err != nil {
		return 0, err
	}
	if latest == 0 {
		return 0, nil
	}

	var start uint64 = 1
	if latest > uint64(s.initialEvents) {
		start = latest - uint64(s.initialEvents) + 1
	}

	recs, err := s.store.ReadFrom(ctx, start, s.initialEvents)
	if err != nil {
		return 0, err
	}

	var lastSeq uint64
	for _, rec := range recs {
		if err := writeSSE(w, rec); err != nil {
			return lastSeq, err
		}
		lastSeq = rec.Sequence
	}
	return lastSeq, nil
}

func writeSSE(w http.ResponseWriter, rec eventstore.Record) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\n", rec.Sequence); err != nil {
		return err
	}
	if _, err := w.Write([]byte("event: record\n")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	return nil
}
