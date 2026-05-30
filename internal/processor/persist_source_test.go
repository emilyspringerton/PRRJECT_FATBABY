package processor

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
	"github.com/example/prrject-fatbaby/secwatch"
)

type staticProvider struct{}

func (staticProvider) AnalyzeText(context.Context, string) (*intelligence.Signal, error) {
	return &intelligence.Signal{ID: "sig-1", Ticker: "ABC", SignalType: "Other", Importance: 1}, nil
}

func TestPersistSourceDocument_AppendsEvent(t *testing.T) {
	store, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	filing := secwatch.FilingDiscoveredEvent{
		Ticker:          "ABC",
		CIK:             "123456",
		AccessionNumber: "000123456-26-000001",
		Form:            "8-K",
		PrimaryDocument: "https://example.com/doc",
	}
	identity := secwatch.FilingIdentity(filing.CIK, filing.AccessionNumber)
	clean := "hello world"
	if err := persistSourceDocument(context.Background(), store, filing, identity, "sec_8k", clean); err != nil {
		t.Fatal(err)
	}

	recs, err := store.ReadFrom(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record got %d", len(recs))
	}
	ev := recs[0].Event
	if ev.Type != "source_document_persisted" {
		t.Fatalf("unexpected type %s", ev.Type)
	}
	if ev.PartitionKey != identity {
		t.Fatalf("unexpected partition key %s", ev.PartitionKey)
	}

	var doc intelligence.SourceDocument
	if err := json.Unmarshal(ev.Data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Identity != identity || doc.Ticker != filing.Ticker || doc.DocumentURL != filing.PrimaryDocument {
		t.Fatalf("unexpected payload %#v", doc)
	}
	if doc.CleanedText != clean || doc.CleanedCharCount != len(clean) {
		t.Fatalf("unexpected cleaned fields %#v", doc)
	}
}

func TestSourceDocumentExists(t *testing.T) {
	store, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	identity := secwatch.FilingIdentity("123456", "000123456-26-000001")
	if sourceDocumentExists(context.Background(), store, identity) {
		t.Fatal("expected sourceDocumentExists false before persist")
	}

	filing := secwatch.FilingDiscoveredEvent{Ticker: "ABC", CIK: "123456", AccessionNumber: "000123456-26-000001", Form: "8-K", PrimaryDocument: "https://example.com/doc"}
	if err := persistSourceDocument(context.Background(), store, filing, identity, "sec_8k", "hello world"); err != nil {
		t.Fatal(err)
	}
	if !sourceDocumentExists(context.Background(), store, identity) {
		t.Fatal("expected sourceDocumentExists true after persist")
	}
}

func TestHandleOne_PersistsSourceBeforeSignalGenerated(t *testing.T) {
	store, err := eventstore.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body><h1>Hello filing</h1></body></html>"))
	}))
	defer srv.Close()

	cfg := WorkerConfig{Store: store, Provider: staticProvider{}, Logger: log.New(io.Discard, "", 0), UserAgent: "test-agent", MaxDocBytes: 1024 * 1024}
	filing := secwatch.FilingDiscoveredEvent{Ticker: "ABC", CIK: "123456", AccessionNumber: "000123456-26-000001", Form: "8-K", PrimaryDocument: srv.URL}

	if err := handleOne(context.Background(), cfg, filing, newSeenSet()); err != nil {
		t.Fatal(err)
	}

	recs, err := store.ReadFrom(context.Background(), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 events got %d", len(recs))
	}
	if recs[0].Event.Type != "source_document_persisted" {
		t.Fatalf("expected first event source_document_persisted got %s", recs[0].Event.Type)
	}
	if recs[1].Event.Type != "signal_generated" {
		t.Fatalf("expected second event signal_generated got %s", recs[1].Event.Type)
	}
}
