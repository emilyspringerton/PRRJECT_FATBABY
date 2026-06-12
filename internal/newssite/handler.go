package newssite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/earningscal"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/eps"
	"github.com/example/prrject-fatbaby/internal/newssite/catalog"
	"github.com/example/prrject-fatbaby/internal/newssite/chart"
	"github.com/example/prrject-fatbaby/internal/newssite/commentary"
	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/newssite/edition"
	"github.com/example/prrject-fatbaby/internal/newssite/epsread"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
	"github.com/example/prrject-fatbaby/internal/newssite/guidanceread"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

// summaryToEntry converts an in-memory DocSummary to the DocEntry shape used by renderers.
// FullText is intentionally left empty — the detail page fetches it directly from the store.
func summaryToEntry(ds *docindex.DocSummary) DocEntry {
	return DocEntry{
		Seq:         ds.Sequence,
		Identity:    ds.Identity,
		Ticker:      ds.Ticker,
		SourceType:  ds.SourceType,
		Form:        ds.Form,
		DocumentURL: ds.DocumentURL,
		BodyPreview: ds.BodyPreview,
		CharCount:   ds.CharCount,
		FilingDate:  ds.FilingDate,
		PersistedAt: ds.PersistedAt,
	}
}

// Handler is an http.Handler for the news site.
type Handler struct {
	store           eventstore.EventStore
	graph           *graphread.Store       // nil if graph dir not configured
	sigIdx          *signalindex.Index     // nil if not wired
	docIdx          *docindex.Index        // nil if not wired
	cat             *catalog.Catalog       // nil if not wired
	epsStore        *epsread.Store         // nil if eps-dir not configured
	commentaryStore *commentary.Store      // nil if commentary-dir not configured
	commentaryDir   string                 // path for POST /api/commentary writes
	guidanceStore   *guidanceread.Store    // nil if guidance-dir not configured
	earningsCalStore *earningscal.Store    // nil if earnings-cal-dir not configured
	emilyBaseURL    string                 // Emily Prime base URL for /api/ask; empty disables
	signalapiURL    string                 // signalapi base URL for ticker context injection; empty skips
	logger          *log.Logger
	defaultLimit    int
	rateLimiter     *ipRateLimiter // free-tier IP rate limiter; nil disables limiting
	askRateLimiter  *ipRateLimiter // Ask Emily rate limiter (5/day); nil disables
}

// NewHandler returns a new Handler.
func NewHandler(store eventstore.EventStore, logger *log.Logger) *Handler {
	return &Handler{
		store:          store,
		logger:         logger,
		defaultLimit:   50,
		rateLimiter:    newIPRateLimiter(),
		askRateLimiter: newAskRateLimiter(),
	}
}

// SetRateLimiter replaces the rate limiter. Pass nil to disable rate limiting (e.g. in tests).
func (h *Handler) SetRateLimiter(rl *ipRateLimiter) { h.rateLimiter = rl }

func (h *Handler) SetGraphStore(gs *graphread.Store)          { h.graph = gs }
func (h *Handler) SetSignalIndex(si *signalindex.Index)       { h.sigIdx = si }
func (h *Handler) SetDocIndex(di *docindex.Index)             { h.docIdx = di }
func (h *Handler) SetCatalog(c *catalog.Catalog)              { h.cat = c }
func (h *Handler) SetEpsStore(es *epsread.Store)              { h.epsStore = es }
func (h *Handler) SetCommentaryStore(cs *commentary.Store, dir string) {
	h.commentaryStore = cs
	h.commentaryDir = dir
}
func (h *Handler) SetGuidanceStore(gs *guidanceread.Store)        { h.guidanceStore = gs }
func (h *Handler) SetEarningsCalStore(s *earningscal.Store)       { h.earningsCalStore = s }

// ServeHTTP dispatches routes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	status := http.StatusOK
	defer func() {
		h.logger.Printf("newssite method=%s path=%s status=%d dur=%s", r.Method, r.URL.Path, status, time.Since(start))
	}()

	path := r.URL.Path

	// Free-tier IP rate limiting: 10 queries/day per IP for content pages.
	if h.rateLimiter != nil && r.Method == http.MethodGet && isRateLimitedPath(path) {
		ip := remoteIP(r)
		ok, _ := h.rateLimiter.check(ip)
		if !ok {
			status = http.StatusTooManyRequests
			h.servePaywall(w, r)
			return
		}
	}

	// POST /api/commentary — Emily publishes a governance article.
	if path == "/api/commentary" && r.Method == http.MethodPost {
		status = h.servePostCommentary(w, r)
		return
	}

	// POST /api/ask — Ask Emily chat (S21-01).
	if path == "/api/ask" && r.Method == http.MethodPost {
		status = h.serveAsk(w, r)
		return
	}

	if r.Method != http.MethodGet {
		status = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", status)
		return
	}

	switch {
	case path == "/":
		status = h.serveFrontPage(w, r)
	case path == "/healthz":
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	case path == "/wire":
		status = h.serveWire(w, r)
	case path == "/breaking":
		status = h.serveBreaking(w, r)
	case path == "/tickers":
		status = h.serveTickers(w, r)
	case path == "/search":
		status = h.serveSearch(w, r)
	case path == "/archive":
		status = h.serveArchive(w, r)
	case path == "/about":
		status = h.serveAbout(w, r)
	case path == "/live":
		status = h.serveLive(w, r)
	case path == "/live/events":
		status = h.serveLiveEvents(w, r)
	case path == "/feed.xml":
		status = h.serveRSS(w, r, "")
	case path == "/api/tickers":
		status = h.serveAPITickers(w, r)
	case strings.HasPrefix(path, "/doc/"):
		status = h.serveDoc(w, r)
	case strings.HasPrefix(path, "/api/chart/"):
		sym := catalog.Normalize(strings.TrimPrefix(path, "/api/chart/"))
		status = h.serveChart(w, r, sym)
	case strings.HasPrefix(path, "/ticker/") && strings.HasSuffix(path, "/feed.xml"):
		sym := strings.TrimSuffix(strings.TrimPrefix(path, "/ticker/"), "/feed.xml")
		status = h.serveTickerRSS(w, r, sym)
	case strings.HasPrefix(path, "/ticker/"):
		status = h.serveTicker(w, r)
	case strings.HasPrefix(path, "/person/"):
		status = h.servePersonPage(w, r)
	case strings.HasPrefix(path, "/company/"):
		// 301 redirect: /company/{sym} → /ticker/{sym} (north-star compatibility)
		sym := strings.TrimPrefix(path, "/company/")
		http.Redirect(w, r, "/ticker/"+sym, http.StatusMovedPermanently)
		status = http.StatusMovedPermanently
	case path == "/section/earnings":
		status = h.serveEarnings(w, r)
	case path == "/section/guidance":
		status = h.serveGuidance(w, r)
	case strings.HasPrefix(path, "/section/"):
		if strings.HasSuffix(path, "/feed.xml") {
			slug := strings.TrimSuffix(strings.TrimPrefix(path, "/section/"), "/feed.xml")
			status = h.serveRSS(w, r, slug)
		} else {
			status = h.serveSection(w, r)
		}
	default:
		status = http.StatusNotFound
		http.NotFound(w, r)
	}
}

// ── Route handlers ────────────────────────────────────────────────────────────

func (h *Handler) serveFrontPage(w http.ResponseWriter, r *http.Request) int {
	var entries []DocEntry
	if h.docIdx != nil {
		for _, ds := range h.docIdx.Recent(h.defaultLimit) {
			entries = append(entries, summaryToEntry(ds))
		}
	} else {
		// docIdx not ready yet. Bound the store scan so a large event store
		// doesn't cause client-side timeouts (which result in 500s). If the scan
		// can't complete in 2s, return a self-refreshing loading page.
		scanCtx, scanCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer scanCancel()
		var err error
		entries, err = ReadLatest(scanCtx, h.store, h.defaultLimit)
		if err != nil {
			if scanCtx.Err() != nil || r.Context().Err() != nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Refresh", "5")
				fmt.Fprintf(w, `<!doctype html><html><head><meta charset=utf-8>
<meta http-equiv="refresh" content="5"><title>Loading…</title></head>
<body style="font-family:sans-serif;padding:2em">
<h2>Newssite is indexing filings…</h2>
<p>The document index is building. This page will refresh in a few seconds.</p>
</body></html>`)
				return http.StatusOK
			}
			http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
			return http.StatusInternalServerError
		}
	}
	earnings := EarningsItemsFrom(h.recentEPS(4))
	var buf bytes.Buffer
	RenderListPage(&buf, entries, h.liveRanked(), earnings, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveDoc(w http.ResponseWriter, r *http.Request) int {
	id := strings.TrimPrefix(r.URL.Path, "/doc/")
	entry, found, err := ReadByIdentity(r.Context(), h.store, id)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	if !found {
		http.NotFound(w, r)
		return http.StatusNotFound
	}
	var buf bytes.Buffer
	RenderDetailPage(&buf, entry, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveWire(w http.ResponseWriter, r *http.Request) int {
	var wire []DocEntry
	if h.docIdx != nil {
		for _, ds := range h.docIdx.Recent(100) {
			if ds.SourceType == "press_release" {
				wire = append(wire, summaryToEntry(ds))
			}
		}
	} else {
		entries, err := ReadLatest(r.Context(), h.store, 100)
		if err != nil {
			http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
			return http.StatusInternalServerError
		}
		for _, e := range entries {
			if e.SourceType == "press_release" {
				wire = append(wire, e)
			}
		}
	}
	var buf bytes.Buffer
	RenderListPage(&buf, wire, nil, nil, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveBreaking(w http.ResponseWriter, r *http.Request) int {
	ranked := h.liveRanked()
	var breaking []edition.Ranked
	for _, r := range ranked {
		if r.Signal.Severity == "critical" || r.Signal.Severity == "high" {
			breaking = append(breaking, r)
		}
	}
	var buf bytes.Buffer
	RenderBreakingPage(&buf, breaking, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveSection(w http.ResponseWriter, r *http.Request) int {
	slug := strings.TrimPrefix(r.URL.Path, "/section/")
	ranked := h.liveRanked()
	var filtered []edition.Ranked
	for _, r := range ranked {
		if r.Section == slug {
			filtered = append(filtered, r)
		}
	}
	var buf bytes.Buffer
	RenderSectionPage(&buf, slug, filtered, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveTicker(w http.ResponseWriter, r *http.Request) int {
	rawSym := strings.TrimPrefix(r.URL.Path, "/ticker/")
	symbol := catalog.Normalize(rawSym)
	if symbol == "" {
		http.NotFound(w, r)
		return http.StatusNotFound
	}

	// Check catalog (if available) to decide known vs 404.
	var row *catalog.TickerRow
	if h.cat != nil {
		row = h.cat.Lookup(symbol)
		if row == nil {
			nearest := h.cat.NearestMatches(symbol, 5)
			var buf bytes.Buffer
			RenderTicker404Page(&buf, symbol, nearest, h.symbols())
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(buf.Bytes())
			return http.StatusNotFound
		}
	}

	// Governance signals for this ticker.
	today := time.Now().UTC().Format("2006-01-02")
	var ranked []edition.Ranked
	if h.graph != nil {
		ranked = edition.Rank(h.graph.LiveSignals(symbol, today), today)
	}

	// Directors and auditor.
	var directors []*entitygraph.PersonNode
	if h.graph != nil {
		directors = h.graph.DirectorsFor(symbol)
	}

	// Docs: prefer docindex (fast in-memory), fall back to event store scan.
	var secDocs, wireDocs []DocEntry
	if h.docIdx != nil {
		for _, ds := range h.docIdx.ForTicker(symbol) {
			de := summaryToEntry(ds)
			if ds.SourceType == "press_release" {
				wireDocs = append(wireDocs, de)
			} else {
				secDocs = append(secDocs, de)
			}
		}
	} else {
		all, _ := ReadLatest(r.Context(), h.store, 200)
		for _, e := range all {
			if normTicker(e.Ticker) != symbol {
				continue
			}
			if e.SourceType == "press_release" {
				wireDocs = append(wireDocs, e)
			} else {
				secDocs = append(secDocs, e)
			}
		}
	}

	// Commentary articles (Emily-authored governance analysis).
	if h.commentaryStore != nil {
		for _, a := range h.commentaryStore.ForTicker(symbol) {
			secDocs = append([]DocEntry{commentaryToEntry(a)}, secDocs...)
		}
	}

	var buf bytes.Buffer
	var tickerEPS []EarningsItemView
	if h.epsStore != nil {
		tickerEPS = EarningsItemsFrom(h.epsStore.ArticlesFor(symbol))
	}
	RenderTickerPage(&buf, symbol, row, ranked, directors, secDocs, wireDocs, tickerEPS, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveTickers(w http.ResponseWriter, r *http.Request) int {
	byAlpha := r.URL.Query().Get("sort") == "alpha"
	var rows []*catalog.TickerRow
	if h.cat != nil {
		rows = h.cat.Directory(byAlpha)
	}
	var buf bytes.Buffer
	RenderTickersPage(&buf, rows, byAlpha, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveSearch(w http.ResponseWriter, r *http.Request) int {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if h.cat != nil && q != "" {
		sym := catalog.Normalize(q)
		if h.cat.Known(sym) {
			// Exact match → redirect straight to ticker page (no interstitial).
			http.Redirect(w, r, "/ticker/"+sym, http.StatusFound)
			return http.StatusFound
		}
	}

	var results []catalog.SearchResult
	if h.cat != nil && q != "" {
		results = h.cat.Search(q)
	}
	var buf bytes.Buffer
	RenderSearchPage(&buf, q, results, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveAPITickers(w http.ResponseWriter, r *http.Request) int {
	q := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 8
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
		if n > 50 {
			n = 50
		}
		limit = n
	}

	var results []catalog.SearchResult
	if h.cat != nil {
		results = h.cat.Search(q)
	}
	if len(results) > limit {
		results = results[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	entries := make([]TickerAPIEntry, 0, len(results))
	for _, res := range results {
		entries = append(entries, TickerAPIEntry{
			Symbol:         res.Symbol,
			SignalCount:    res.SignalCount,
			MaxSeverity:    res.MaxSeverity,
			LatestActivity: res.LatestActivity,
		})
	}
	out := TickerAPIResult{
		Query:   catalog.Normalize(q),
		Count:   len(entries),
		Tickers: entries,
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		h.logger.Printf("api/tickers encode: %v", err)
	}
	return http.StatusOK
}

func (h *Handler) serveArchive(w http.ResponseWriter, r *http.Request) int {
	var entries []DocEntry
	if h.docIdx != nil {
		for _, ds := range h.docIdx.Recent(500) {
			entries = append(entries, summaryToEntry(ds))
		}
	} else {
		var err error
		entries, err = ReadLatest(r.Context(), h.store, 500)
		if err != nil {
			http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
			return http.StatusInternalServerError
		}
	}
	var buf bytes.Buffer
	RenderArchivePage(&buf, entries, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveAbout(w http.ResponseWriter, r *http.Request) int {
	var rulesAt time.Time
	if h.graph != nil {
		rulesAt = h.graph.RulesUpdatedAt()
	}
	var buf bytes.Buffer
	RenderAboutPage(&buf, h.symbols(), rulesAt)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveLive(w http.ResponseWriter, r *http.Request) int {
	ranked := h.liveRanked()
	var breaking []edition.Ranked
	for _, item := range ranked {
		if item.Signal.Severity == "critical" || item.Signal.Severity == "high" {
			breaking = append(breaking, item)
		}
	}
	var buf bytes.Buffer
	RenderLivePage(&buf, breaking, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

// serveLiveEvents is an SSE endpoint. Each time the graph store refreshes it
// pushes a "refresh" event; the client re-fetches the /breaking list and
// prepends any new cards. Falls back gracefully when the graph is not wired.
func (h *Handler) serveLiveEvents(w http.ResponseWriter, r *http.Request) int {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send an initial heartbeat so the client knows it's connected.
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		var updates <-chan struct{}
		if h.graph != nil {
			updates = h.graph.Updates()
		}

		select {
		case <-ctx.Done():
			return http.StatusOK
		case <-time.After(30 * time.Second):
			// Heartbeat to keep the connection alive through proxies.
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case _, _ = <-updates:
			fmt.Fprintf(w, "event: refresh\ndata: {}\n\n")
			flusher.Flush()
			// Arm next wait: get fresh channel after the store replaced it.
			if h.graph != nil {
				updates = h.graph.Updates()
			}
		}
	}
}

func (h *Handler) servePersonPage(w http.ResponseWriter, r *http.Request) int {
	canonicalID := strings.TrimPrefix(r.URL.Path, "/person/")
	if canonicalID == "" || h.graph == nil {
		http.NotFound(w, r)
		return http.StatusNotFound
	}

	node, ok := h.graph.Node(canonicalID)
	if !ok {
		http.NotFound(w, r)
		return http.StatusNotFound
	}

	edges := h.graph.EdgesFor(canonicalID)
	personSigs := h.graph.SignalsForPerson(canonicalID)
	today := time.Now().UTC().Format("2006-01-02")

	var buf bytes.Buffer
	view := buildPersonPage(node, edges, personSigs, today, h.graph, h.symbols())
	RenderPersonPage(&buf, view)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveTickerRSS(w http.ResponseWriter, r *http.Request, sym string) int {
	sym = strings.ToUpper(strings.TrimSpace(sym))
	if sym == "" {
		http.NotFound(w, r)
		return http.StatusNotFound
	}
	today := time.Now().UTC().Format("2006-01-02")
	var ranked []edition.Ranked
	if h.graph != nil {
		sigs := h.graph.LiveSignals(sym, today)
		ranked = edition.Rank(sigs, today)
	}
	if len(ranked) > 50 {
		ranked = ranked[:50]
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	var buf bytes.Buffer
	if err := RenderTickerRSS(&buf, sym, ranked, baseURL); err != nil {
		http.Error(w, fmt.Sprintf("rss error: %v", err), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveRSS(w http.ResponseWriter, r *http.Request, section string) int {
	ranked := h.liveRanked()
	if section != "" {
		var filtered []edition.Ranked
		for _, item := range ranked {
			if item.Section == section {
				filtered = append(filtered, item)
			}
		}
		ranked = filtered
	}
	// Cap feed at 50 items.
	if len(ranked) > 50 {
		ranked = ranked[:50]
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	var buf bytes.Buffer
	if err := RenderRSS(&buf, ranked, section, baseURL); err != nil {
		http.Error(w, fmt.Sprintf("rss error: %v", err), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveEarnings(w http.ResponseWriter, r *http.Request) int {
	items := EarningsItemsFrom(h.recentEPS(100))

	var upcoming []UpcomingEarningsView
	if h.earningsCalStore != nil {
		today := time.Now().UTC().Format("2006-01-02")
		// Show up to 30 days of upcoming earnings.
		horizon := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
		records := h.earningsCalStore.Query(nil, today, horizon, nil)
		for _, d := range records {
			uv := UpcomingEarningsView{
				Ticker:     strings.ToUpper(d.Ticker),
				ReportDate: formatUpcomingDate(d.ReportDate),
				PeriodStr:  formatPeriodStr(d.FiscalQuarter, d.FiscalYear),
				StatusLabel: earningsStatusLabel(d.Status),
			}
			if d.BeforeMarket != nil {
				if *d.BeforeMarket {
					uv.Timing = "BMO"
				} else {
					uv.Timing = "AMC"
				}
			}
			upcoming = append(upcoming, uv)
		}
	}

	var buf bytes.Buffer
	RenderEarningsPage(&buf, items, upcoming, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func formatUpcomingDate(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.Format("Jan 2, 2006")
}

func formatPeriodStr(quarter string, year int) string {
	if quarter == "" {
		return ""
	}
	if year > 0 {
		return fmt.Sprintf("%s %d", quarter, year)
	}
	return quarter
}

func earningsStatusLabel(s earningscal.Status) string {
	switch s {
	case earningscal.StatusConfirmed:
		return "Confirmed"
	case earningscal.StatusAnnounced:
		return "Announced"
	default:
		return "Expected"
	}
}

func (h *Handler) serveGuidance(w http.ResponseWriter, r *http.Request) int {
	var items []GuidanceItemView
	if h.guidanceStore != nil {
		items = GuidanceItemsFrom(h.guidanceStore.Recent(100))
	}
	var buf bytes.Buffer
	RenderGuidancePage(&buf, items, h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (h *Handler) recentEPS(n int) []*eps.Article {
	if h.epsStore == nil {
		return nil
	}
	return h.epsStore.Recent(n)
}

func (h *Handler) liveRanked() []edition.Ranked {
	if h.graph == nil {
		return nil
	}
	today := time.Now().UTC().Format("2006-01-02")
	return edition.Rank(h.graph.LiveSignals("", today), today)
}

// commentaryToEntry converts an Emily-authored Article to the DocEntry shape
// used by all newssite renderers. SourceType "emily_commentary" gets its own
// kicker style in templates.go so it's visually distinct from SEC filings.
func commentaryToEntry(a *commentary.Article) DocEntry {
	preview := a.Preview
	if preview == "" {
		runes := []rune(a.Body)
		if len(runes) > 300 {
			runes = runes[:300]
		}
		preview = strings.TrimSpace(string(runes))
	}
	return DocEntry{
		Identity:    a.ID,
		Ticker:      strings.ToUpper(strings.TrimSpace(a.Ticker)),
		SourceType:  "emily_commentary",
		DocumentURL: "/commentary/" + a.ID,
		BodyPreview: preview,
		FullText:    a.Body,
		CharCount:   len(a.Body),
		FilingDate:  a.FilingDate,
		PersistedAt: a.PublishedAt,
	}
}

func (h *Handler) symbols() []string {
	if h.cat == nil {
		return nil
	}
	return h.cat.AllSymbols()
}

// servePostCommentary handles POST /api/commentary — Emily publishes a governance article.
// Accepts JSON body matching commentary.Article. Requires commentaryStore to be wired.
// Returns 201 Created on success with the article ID.
func (h *Handler) servePostCommentary(w http.ResponseWriter, r *http.Request) int {
	if h.commentaryStore == nil || h.commentaryDir == "" {
		http.Error(w, `{"error":"commentary not configured"}`, http.StatusServiceUnavailable)
		return http.StatusServiceUnavailable
	}
	var art commentary.Article
	if err := json.NewDecoder(r.Body).Decode(&art); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return http.StatusBadRequest
	}
	if strings.TrimSpace(art.Headline) == "" || strings.TrimSpace(art.Body) == "" {
		http.Error(w, `{"error":"headline and body required"}`, http.StatusBadRequest)
		return http.StatusBadRequest
	}
	if art.ID == "" {
		art.ID = "commentary-" + strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339), ":", "")
	}
	if art.PublishedAt.IsZero() {
		art.PublishedAt = time.Now().UTC()
	}
	if art.Preview == "" && len(art.Body) > 0 {
		runes := []rune(art.Body)
		if len(runes) > 300 {
			runes = runes[:300]
		}
		art.Preview = strings.TrimSpace(string(runes))
	}
	if err := commentary.Append(h.commentaryDir, art); err != nil {
		h.logger.Printf("commentary append err: %v", err)
		http.Error(w, `{"error":"write failed"}`, http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	if err := h.commentaryStore.Refresh(); err != nil {
		h.logger.Printf("commentary refresh err: %v (non-fatal)", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "created", "id": art.ID})
	return http.StatusCreated
}

// serveChart returns a 3-month SVG price sparkline for the given ticker.
// Responds with SVG content-type so it can be embedded as <img src="/api/chart/AAPL">.
// servePaywall renders the free-tier query limit page with Emily+ upsell.
func (h *Handler) servePaywall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Retry-After", "86400")
	var buf bytes.Buffer
	RenderPaywallPage(&buf)
	_, _ = w.Write(buf.Bytes())
}

func (h *Handler) serveChart(w http.ResponseWriter, r *http.Request, sym string) int {
	if sym == "" {
		http.NotFound(w, r)
		return http.StatusNotFound
	}
	svg := chart.SVGCached(sym)
	if svg == "" {
		// No data — return a transparent 1×1 SVG placeholder.
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	fmt.Fprint(w, svg)
	return http.StatusOK
}
