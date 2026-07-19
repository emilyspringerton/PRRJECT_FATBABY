package newssite

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/newssite/commentary"
)

// mkHandlerWithCommentary builds a Handler with a real commentary.Store
// seeded with the given articles -- the regression coverage for the
// 2026-07-19 fix: commentaryToEntry-generated /commentary/{id} links used to
// 404 (no route ever served them), and every commentary article's headline
// was discarded in favor of a synthesized "$TICKER — $Type" string.
func mkHandlerWithCommentary(t *testing.T, articles ...commentary.Article) *Handler {
	t.Helper()
	storeDir := t.TempDir()
	store, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	commentaryDir := t.TempDir()
	for _, a := range articles {
		if err := commentary.Append(commentaryDir, a); err != nil {
			t.Fatalf("append commentary: %v", err)
		}
	}
	cs := commentary.NewStore(commentaryDir)
	if err := cs.Refresh(); err != nil {
		t.Fatalf("refresh commentary store: %v", err)
	}

	h := NewHandler(store, log.New(io.Discard, "", 0))
	h.SetCommentaryStore(cs, commentaryDir)
	return h
}

func TestServeCommentary_FoundReturnsArticle(t *testing.T) {
	now := time.Date(2026, 7, 20, 9, 45, 0, 0, time.UTC)
	h := mkHandlerWithCommentary(t, commentary.Article{
		ID: "movers-2026-07-20", Kind: "market_movers",
		Headline: "Stocks on the Move — July 20, 2026",
		Body:     "TOP GAINERS\n\nLucid Group, Inc. (LCID) — +13.93%",
		Byline:   "The Markets Desk", PublishedAt: now,
	})

	req := httptest.NewRequest(http.MethodGet, "/commentary/movers-2026-07-20", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Stocks on the Move — July 20, 2026") {
		t.Errorf("expected the article's own authored headline, not a synthesized one; body:\n%s", body)
	}
	if !strings.Contains(body, "Lucid Group") {
		t.Error("expected article body content to render")
	}
	if !strings.Contains(body, "Stocks on the Move") {
		t.Error("expected the market_movers kicker to render (not a generic SEC-filing kicker)")
	}
}

func TestServeCommentary_NotFound(t *testing.T) {
	h := mkHandlerWithCommentary(t)
	req := httptest.NewRequest(http.MethodGet, "/commentary/does-not-exist", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestServeCommentary_NoStoreConfigured(t *testing.T) {
	storeDir := t.TempDir()
	store, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	h := NewHandler(store, log.New(io.Discard, "", 0)) // no SetCommentaryStore call

	req := httptest.NewRequest(http.MethodGet, "/commentary/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when commentary isn't configured", w.Code)
	}
}

func TestServeMoversSection_OnlyMarketMoversKindNewestFirst(t *testing.T) {
	now := time.Date(2026, 7, 20, 9, 45, 0, 0, time.UTC)
	h := mkHandlerWithCommentary(t,
		commentary.Article{ID: "movers-old", Kind: "market_movers", Headline: "Old Movers", PublishedAt: now.Add(-24 * time.Hour)},
		commentary.Article{ID: "movers-new", Kind: "market_movers", Headline: "New Movers", PublishedAt: now},
		commentary.Article{ID: "gov-1", Kind: "governance_alert", Headline: "Unrelated Governance Piece", Ticker: "AAPL", PublishedAt: now},
	)

	req := httptest.NewRequest(http.MethodGet, "/section/movers", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "New Movers") || !strings.Contains(body, "Old Movers") {
		t.Errorf("expected both movers articles listed, body:\n%s", body)
	}
	if strings.Contains(body, "Unrelated Governance Piece") {
		t.Error("governance_alert article should not appear on the movers section page")
	}
	if strings.Index(body, "New Movers") > strings.Index(body, "Old Movers") {
		t.Error("expected newest-first ordering")
	}
}

func TestServeMoversSection_EmptyWhenNoCommentaryStore(t *testing.T) {
	storeDir := t.TempDir()
	store, err := eventstore.NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	h := NewHandler(store, log.New(io.Discard, "", 0))

	req := httptest.NewRequest(http.MethodGet, "/section/movers", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (empty list page, not an error)", w.Code)
	}
}
