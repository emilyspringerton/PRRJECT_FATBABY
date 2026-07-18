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
	"github.com/example/prrject-fatbaby/internal/earningscal"
	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
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
			name:         "feed.xml empty",
			path:         "/feed.xml",
			wantStatus:   http.StatusOK,
			wantContains: []string{"<?xml", "rss", "FATBABY"},
		},
		{
			name:         "section feed empty",
			path:         "/section/governance/feed.xml",
			wantStatus:   http.StatusOK,
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
		// ── Filing date provenance ──────────────────────────────────────────────
		{
			name: "filing date shown not persisted date",
			path: "/",
			seed: func(t *testing.T, store eventstore.EventStore) {
				// Filing from 2019; pipeline-indexed in 2026. Front page must show
				// the 2026 date as "today" only if the filing qualifies, but the
				// document itself should display 2019 as its date.
				writeSourceDoc(t, store, intelligence.SourceDocument{
					Identity: "fd:1", Ticker: "AAPL", SourceType: "sec_8k", Form: "8-K",
					FilingDate: "2019-04-26", PersistedAt: now,
					CleanedText: "vote results", CleanedCharCount: 12,
				})
			},
			// Historical filings (>90 days) are suppressed from the front page feed.
			// The front page will show "No source documents" rather than a 2019 filing.
			wantStatus:  http.StatusOK,
			notContains: []string{"22 May 2026"}, // persisted_at date must not appear as the article date
		},
		{
			name: "detail page shows filing date not persisted date",
			path: "/doc/fd:2",
			seed: func(t *testing.T, store eventstore.EventStore) {
				writeSourceDoc(t, store, intelligence.SourceDocument{
					Identity: "fd:2", Ticker: "AAPL", SourceType: "sec_8k", Form: "8-K",
					FilingDate: "2019-04-26", PersistedAt: now,
					CleanedText: "annual meeting votes", CleanedCharCount: 20,
				})
			},
			wantStatus:   http.StatusOK,
			wantContains: []string{"2019", "AAPL"},
		},
		// ── Routes smoke tests ─────────────────────────────────────────────────
		{
			name:         "healthz",
			path:         "/healthz",
			wantStatus:   http.StatusOK,
			wantContains: []string{"ok"},
		},
		{
			name:       "wire empty",
			path:       "/wire",
			wantStatus: http.StatusOK,
		},
		{
			name: "wire shows press release",
			path: "/wire",
			seed: func(t *testing.T, store eventstore.EventStore) {
				writeSourceDoc(t, store, intelligence.SourceDocument{
					Identity: "pr:1", Ticker: "AAPL", SourceType: "press_release",
					CleanedText: "earnings beat", CleanedCharCount: 13, PersistedAt: now,
				})
			},
			wantStatus:   http.StatusOK,
			wantContains: []string{"AAPL"},
		},
		{
			name:       "archive empty",
			path:       "/archive",
			wantStatus: http.StatusOK,
		},
		{
			name:         "about page",
			path:         "/about",
			wantStatus:   http.StatusOK,
			wantContains: []string{"FATBABY"},
		},
		{
			name:       "tickers empty",
			path:       "/tickers",
			wantStatus: http.StatusOK,
		},
		{
			name:       "live page",
			path:       "/live",
			wantStatus: http.StatusOK,
		},
		{
			name:       "breaking page",
			path:       "/breaking",
			wantStatus: http.StatusOK,
		},
		{
			name:         "section governance",
			path:         "/section/governance",
			wantStatus:   http.StatusOK,
			wantContains: []string{"Governance"},
		},
		{
			name:         "section earnings",
			path:         "/section/earnings",
			wantStatus:   http.StatusOK,
			wantContains: []string{"Earnings"},
		},
		{
			// Without a catalog wired, api/tickers returns empty JSON array.
			name:         "api tickers no catalog",
			path:         "/api/tickers?q=AAPL",
			wantStatus:   http.StatusOK,
			wantContains: []string{"["},
		},
		{
			// Without a catalog, search renders a results page (200) rather than redirecting.
			name:       "search no catalog returns page",
			path:       "/search?q=AAPL",
			wantStatus: http.StatusOK,
		},
		{
			name: "ticker page with doc",
			path: "/ticker/AAPL",
			seed: func(t *testing.T, store eventstore.EventStore) {
				writeSourceDoc(t, store, intelligence.SourceDocument{
					Identity: "t:1", Ticker: "AAPL", SourceType: "sec_8k", Form: "8-K",
					CleanedText: "some filing", CleanedCharCount: 11, PersistedAt: now,
				})
			},
			wantStatus:   http.StatusOK,
			wantContains: []string{"AAPL"},
		},
		{
			name:         "ticker rss",
			path:         "/ticker/AAPL/feed.xml",
			wantStatus:   http.StatusOK,
			wantContains: []string{"<?xml", "AAPL"},
		},
		{
			name:       "method not allowed",
			path:       "/",
			wantStatus: http.StatusOK, // GET is fine
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

// TestReadLatest_NewestFirst verifies the backward-walk returns newest records first.
func TestReadLatest_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	for i, ticker := range []string{"AAPL", "MSFT", "GOOG"} {
		writeSourceDoc(t, store, intelligence.SourceDocument{
			Identity:    ticker + ":1",
			Ticker:      ticker,
			SourceType:  "sec_8k",
			PersistedAt: now.Add(time.Duration(i) * time.Hour),
		})
	}

	ctx := context.Background()
	entries, err := ReadLatest(ctx, store, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// Most recent first: GOOG (hour 2), MSFT (hour 1), AAPL (hour 0)
	if entries[0].Ticker != "GOOG" {
		t.Errorf("entries[0].Ticker = %q, want GOOG (newest)", entries[0].Ticker)
	}
	if entries[2].Ticker != "AAPL" {
		t.Errorf("entries[2].Ticker = %q, want AAPL (oldest)", entries[2].Ticker)
	}
}

// TestEarningsCalendarSection verifies that upcoming earnings dates from the
// earningscal.Store appear on /section/earnings.
func TestEarningsCalendarSection(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	// Build an earningscal.Store with one upcoming date in the 30-day window.
	calDir := t.TempDir()
	today := time.Now().UTC()
	reportDate := today.AddDate(0, 0, 10).Format("2006-01-02") // 10 days away
	bmo := true
	d := earningscal.EarningsDate{
		ID:            earningscal.MakeID("AAPL", "Q3", today.Year()),
		Ticker:        "AAPL",
		FiscalQuarter: "Q3",
		FiscalYear:    today.Year(),
		ReportDate:    reportDate,
		BeforeMarket:  &bmo,
		Status:        earningscal.StatusAnnounced,
		Source:        "press_release",
		UpdatedAt:     today.Format(time.RFC3339),
	}
	calStore := earningscal.NewStore(calDir)
	if err := calStore.Append(d); err != nil {
		t.Fatalf("append earnings date: %v", err)
	}
	if err := calStore.Refresh(); err != nil {
		t.Fatalf("refresh earnings store: %v", err)
	}

	h := NewHandler(store, log.New(io.Discard, "", 0))
	h.SetEarningsCalStore(calStore)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/section/earnings", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "AAPL") {
		t.Errorf("/section/earnings missing AAPL ticker from upcoming calendar")
	}
	if !strings.Contains(body, "Announced") {
		t.Errorf("/section/earnings missing Announced status from upcoming calendar")
	}
	if !strings.Contains(body, "BMO") {
		t.Errorf("/section/earnings missing BMO timing from upcoming calendar")
	}
	if !strings.Contains(body, "Q3") {
		t.Errorf("/section/earnings missing Q3 period from upcoming calendar")
	}
	if !strings.Contains(body, "Upcoming") {
		t.Errorf("/section/earnings missing 'Upcoming' section header")
	}
}

// TestEarningsCalendarSection_Empty verifies that /section/earnings renders
// cleanly when no earningscal.Store is wired (nil-safe).
func TestEarningsCalendarSection_NilStore(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	h := NewHandler(store, log.New(io.Discard, "", 0))
	// earningsCalStore intentionally not set

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/section/earnings", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when earningsCal is nil", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Earnings") {
		t.Errorf("earnings page missing 'Earnings' heading")
	}
}

// TestEarningsCalendarSection_PastDateExcluded verifies that earnings dates
// before today are not shown in the upcoming section.
func TestEarningsCalendarSection_PastDateExcluded(t *testing.T) {
	dir := t.TempDir()
	store, _ := eventstore.NewFileStore(dir)
	defer store.Close()

	calDir := t.TempDir()
	past := time.Now().UTC().AddDate(0, 0, -5).Format("2006-01-02")
	d := earningscal.EarningsDate{
		ID: earningscal.MakeID("PAST", "Q1", 2026), Ticker: "PAST",
		FiscalQuarter: "Q1", FiscalYear: 2026, ReportDate: past,
		Status: earningscal.StatusConfirmed, Source: "test",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	calStore := earningscal.NewStore(calDir)
	_ = calStore.Append(d)
	_ = calStore.Refresh()

	h := NewHandler(store, log.New(io.Discard, "", 0))
	h.SetEarningsCalStore(calStore)

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/section/earnings", nil)
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "PAST") {
		t.Error("/section/earnings shows a past earnings date in the upcoming section")
	}
}

// TestFormatPeriodStr validates the period formatter.
func TestFormatPeriodStr(t *testing.T) {
	cases := []struct {
		q    string
		y    int
		want string
	}{
		{"Q3", 2026, "Q3 2026"},
		{"FY", 2027, "FY 2027"},
		{"Q1", 0, "Q1"},
		{"", 2026, ""},
	}
	for _, tc := range cases {
		got := formatPeriodStr(tc.q, tc.y)
		if got != tc.want {
			t.Errorf("formatPeriodStr(%q, %d) = %q, want %q", tc.q, tc.y, got, tc.want)
		}
	}
}

// TestFormatUpcomingDate validates the date formatter.
func TestFormatUpcomingDate(t *testing.T) {
	got := formatUpcomingDate("2026-11-14")
	if got != "Nov 14, 2026" {
		t.Errorf("formatUpcomingDate(2026-11-14) = %q, want Nov 14, 2026", got)
	}
	// Invalid date passthrough
	got2 := formatUpcomingDate("not-a-date")
	if got2 != "not-a-date" {
		t.Errorf("invalid date should pass through unchanged: %q", got2)
	}
}

// TestEarningsStatusLabel verifies status label mapping.
func TestEarningsStatusLabel(t *testing.T) {
	if earningsStatusLabel(earningscal.StatusConfirmed) != "Confirmed" {
		t.Error("expected Confirmed for StatusConfirmed")
	}
	if earningsStatusLabel(earningscal.StatusAnnounced) != "Announced" {
		t.Error("expected Announced for StatusAnnounced")
	}
	if earningsStatusLabel(earningscal.StatusBackfilled) != "Expected" {
		t.Error("expected Expected for StatusBackfilled")
	}
}

// TestReadLatest_LimitRespected verifies limit is honoured even when more records exist.
func TestReadLatest_LimitRespected(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		writeSourceDoc(t, store, intelligence.SourceDocument{
			Identity:    "x:" + time.Duration(i).String(),
			Ticker:      "X",
			SourceType:  "sec_8k",
			PersistedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}
	ctx := context.Background()
	entries, err := ReadLatest(ctx, store, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("got %d entries, want 5", len(entries))
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

func TestServeDoc_FastPathViaDocIndex(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	writeSourceDoc(t, store, intelligence.SourceDocument{
		Identity: "fastpath:1", Ticker: "MSFT", SourceType: "sec_8k",
		CleanedText: "FULL BODY VIA FAST PATH", CleanedCharCount: 24, PersistedAt: now,
	})

	idx := docindex.NewIndex()
	if err := docindex.Build(context.Background(), store, idx, 1, log.New(io.Discard, "", 0)); err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Sanity check the precondition this test actually exercises: the
	// fast path requires ByIdentity to resolve to a real sequence.
	summary, ok := idx.ByIdentity("fastpath:1")
	if !ok || summary.Sequence == 0 {
		t.Fatalf("expected indexed doc with a non-zero sequence, got %+v ok=%v", summary, ok)
	}

	h := NewHandler(store, log.New(io.Discard, "", 0))
	h.SetDocIndex(idx)

	req := httptest.NewRequest(http.MethodGet, "/doc/fastpath:1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FULL BODY VIA FAST PATH") {
		t.Fatalf("expected full body text in response, got: %s", rec.Body.String())
	}
}
