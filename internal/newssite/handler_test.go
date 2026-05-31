package newssite

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func TestHandler(t *testing.T) {
	mkHandler := func(t *testing.T, seed func(t *testing.T, store eventstore.EventStore)) http.Handler {
		t.Helper()
		dir := t.TempDir()
		store, err := eventstore.NewFileStore(dir)
		if err != nil {
			t.Fatalf("new store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if seed != nil {
			seed(t, store)
		}
		return NewHandler(store, log.New(io.Discard, "", 0))
	}

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		path         string
		seed         func(t *testing.T, store eventstore.EventStore)
		wantStatus   int
		wantContains []string
		notContains  []string
	}{
		{
			name:         "list page empty",
			path:         "/",
			wantStatus:   http.StatusOK,
			wantContains: []string{"No source documents have been persisted yet"},
		},
		{
			name: "list page one sec",
			path: "/",
			seed: func(t *testing.T, store eventstore.EventStore) {
				writeSourceDoc(t, store, intelligence.SourceDocument{Identity: "abc:1", Ticker: "MSFT", SourceType: "sec_8k", Form: "8-K", CleanedText: "hello world", CleanedCharCount: 11, PersistedAt: now})
			},
			wantStatus:   http.StatusOK,
			wantContains: []string{"MSFT", "SEC Filing", "Read full document"},
		},
		{
			name: "list page one press release",
			path: "/",
			seed: func(t *testing.T, store eventstore.EventStore) {
				writeSourceDoc(t, store, intelligence.SourceDocument{Identity: "abc:2", Ticker: "AAPL", SourceType: "press_release", CleanedText: "launch", CleanedCharCount: 6, PersistedAt: now})
			},
			wantStatus:   http.StatusOK,
			wantContains: []string{"Press Release"},
		},
		{
			name: "detail page found",
			path: "/doc/abc:3",
			seed: func(t *testing.T, store eventstore.EventStore) {
				writeSourceDoc(t, store, intelligence.SourceDocument{Identity: "abc:3", Ticker: "TSLA", SourceType: "sec_8k", CleanedText: "FULL BODY TEXT", CleanedCharCount: 14, PersistedAt: now})
			},
			wantStatus:   http.StatusOK,
			wantContains: []string{"FULL BODY TEXT"},
		},
		{
			name:       "detail page not found",
			path:       "/doc/missing",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown path",
			path:       "/wat",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "person page no graph",
			path:       "/person/frank-c-herringer",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "feed.xml empty",
			path:       "/feed.xml",
			wantStatus: http.StatusOK,
			wantContains: []string{"<?xml", "rss", "FATBABY"},
		},
		{
			name:       "section feed empty",
			path:       "/section/governance/feed.xml",
			wantStatus: http.StatusOK,
			wantContains: []string{"<?xml", "Governance"},
		},
		{
			name:       "company redirect",
			path:       "/company/SCHW",
			wantStatus: http.StatusMovedPermanently,
		},
		{
			name: "xss guard",
			path: "/doc/abc:4",
			seed: func(t *testing.T, store eventstore.EventStore) {
				writeSourceDoc(t, store, intelligence.SourceDocument{Identity: "abc:4", Ticker: "META", SourceType: "sec_8k", CleanedText: "<script>alert(1)</script>", CleanedCharCount: 25, PersistedAt: now})
			},
			wantStatus:   http.StatusOK,
			wantContains: []string{"&lt;script&gt;alert(1)&lt;/script&gt;"},
			notContains:  []string{"<script>alert(1)</script>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := mkHandler(t, tt.seed)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d", rr.Code, tt.wantStatus)
			}
			body := rr.Body.String()
			for _, s := range tt.wantContains {
				if !strings.Contains(body, s) {
					t.Fatalf("body missing %q", s)
				}
			}
			for _, s := range tt.notContains {
				if strings.Contains(body, s) {
					t.Fatalf("body unexpectedly contains %q", s)
				}
			}
		})
	}
}

func writeSourceDoc(t *testing.T, store eventstore.EventStore, doc intelligence.SourceDocument) {
	t.Helper()
	payload, _ := json.Marshal(doc)
	_, err := store.Append(context.Background(), eventstore.Event{
		ID:           "source_document_persisted:" + doc.Identity,
		Type:         "source_document_persisted",
		PartitionKey: doc.Identity,
		Source:       "test",
		Data:         payload,
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
}
