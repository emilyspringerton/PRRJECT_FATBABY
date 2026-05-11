package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/server"
)

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	dataDir := flag.String("data-dir", "./data", "path to eventstore root directory")
	flag.Parse()

	store, err := eventstore.NewFileStore(*dataDir)
	if err != nil {
		log.Fatalf("init file store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	srv := server.New(store)
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("dashboard listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("http server: %v", err)
	}
}
