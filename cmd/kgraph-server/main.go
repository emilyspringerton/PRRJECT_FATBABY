// cmd/kgraph-server — the real origin server IDUNA's KGraphHandler has been
// proxying to since S138-06, via KGRAPH_URL, without an actual service on
// the other end.
//
// 2026-08-20, founder real-time: "developing any data science infrastructure
// you need as iduna apis" / "we can back it with python or whatever best
// suits our needs." Investigated rather than guessed at what this concretely
// meant: IDUNA already has a real KGraphHandler
// (IDUNA/internal/http/handlers/kgraph.go) proxying GET /api/v1/kgraph/query
// to KGRAPH_URL, and internal/kgraph already has a real, working
// Haiku-driven entity-extraction + MongoDB-backed Store.Query -- but no Go
// code anywhere calls it as a running service (only test files import the
// package). That's the actual gap: not missing analysis capability, a
// missing few dozen lines of HTTP wiring to expose it. Go, not Python --
// the founder left the language open ("whatever best suits our needs") and
// the real capability is already implemented in Go; a Python rewrite would
// mean re-deriving the same Mongo queries for no benefit.
//
// Route matches IDUNA's proxy contract exactly:
//   GET /query?entity=<name>&predicate=<rel>&limit=<n>
//
// Env:
//   MONGO_URI  — required; same connection string internal/mongowriter uses
//   MONGO_DB   — optional, defaults to "fatbaby" (matches mongowriter's own default)
//   PORT       — optional, defaults to 9092
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/example/prrject-fatbaby/internal/kgraph"
	"github.com/example/prrject-fatbaby/internal/mongowriter"
)

func main() {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("kgraph-server: MONGO_URI is required (see this file's own header comment) -- " +
			"no real MongoDB connection has been provisioned for this box yet as of 2026-08-20, " +
			"this is real infrastructure work still queued, not a code gap")
	}
	dbName := os.Getenv("MONGO_DB")
	if dbName == "" {
		dbName = "fatbaby"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "9092"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongowriter.Connect(ctx, mongoURI)
	if err != nil {
		log.Fatalf("kgraph-server: mongo connect: %v", err)
	}
	store := kgraph.New(client.Database(dbName))

	mux := http.NewServeMux()
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		entity := r.URL.Query().Get("entity")
		if entity == "" {
			writeJSONError(w, http.StatusBadRequest, "entity query param is required")
			return
		}
		predicate := r.URL.Query().Get("predicate")
		limit := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				limit = n
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		results, err := store.Query(ctx, entity, predicate, limit)
		if err != nil {
			log.Printf("kgraph-server: query entity=%q predicate=%q err=%v", entity, predicate, err)
			writeJSONError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if results == nil {
			results = []kgraph.GraphResult{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"entity": entity, "results": results})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	log.Printf("kgraph-server: listening on :%s (mongo db=%s)", port, dbName)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func writeJSONError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"code": http.StatusText(status), "detail": detail})
}
