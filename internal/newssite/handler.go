package newssite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
	"github.com/example/prrject-fatbaby/internal/newssite/catalog"
	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/newssite/edition"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

// Handler is an http.Handler for the news site.
type Handler struct {
	store        eventstore.EventStore
	graph        *graphread.Store    // nil if graph dir not configured
	sigIdx       *signalindex.Index  // nil if not wired
	docIdx       *docindex.Index     // nil if not wired
	cat          *catalog.Catalog    // nil if not wired
	logger       *log.Logger
	defaultLimit int
}

// NewHandler returns a new Handler.
func NewHandler(store eventstore.EventStore, logger *log.Logger) *Handler {
	return &Handler{store: store, logger: logger, defaultLimit: 50}
}

func (h *Handler) SetGraphStore(gs *graphread.Store)    { h.graph = gs }
func (h *Handler) SetSignalIndex(si *signalindex.Index) { h.sigIdx = si }
func (h *Handler) SetDocIndex(di *docindex.Index)       { h.docIdx = di }
func (h *Handler) SetCatalog(c *catalog.Catalog)        { h.cat = c }

// ServeHTTP dispatches routes.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	status := http.StatusOK
	defer func() {
		h.logger.Printf("newssite method=%s path=%s status=%d dur=%s", r.Method, r.URL.Path, status, time.Since(start))
	}()

	if r.Method != http.MethodGet {
		status = http.StatusMethodNotAllowed
		http.Error(w, "method not allowed", status)
		return
	}

	path := r.URL.Path
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
	entries, err := ReadLatest(r.Context(), h.store, h.defaultLimit)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	var buf bytes.Buffer
	RenderListPage(&buf, entries, h.liveRanked(), h.symbols())
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
	entries, err := ReadLatest(r.Context(), h.store, 100)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	var wire []DocEntry
	for _, e := range entries {
		if e.SourceType == "press_release" {
			wire = append(wire, e)
		}
	}
	var buf bytes.Buffer
	RenderListPage(&buf, wire, nil, h.symbols())
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

	// Docs: prefer docindex (fast), fall back to event store scan.
	var secDocs, wireDocs []DocEntry
	if h.docIdx != nil {
		for _, ds := range h.docIdx.ForTicker(symbol) {
			de := DocEntry{
				Identity:    ds.Identity,
				Ticker:      ds.Ticker,
				SourceType:  ds.SourceType,
				Form:        ds.Form,
				PersistedAt: ds.PersistedAt,
			}
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

	var buf bytes.Buffer
	RenderTickerPage(&buf, symbol, row, ranked, directors, secDocs, wireDocs, h.symbols())
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
	entries, err := ReadLatest(r.Context(), h.store, 500)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return http.StatusInternalServerError
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

// ── Helpers ───────────────────────────────────────────────────────────────────

func (h *Handler) liveRanked() []edition.Ranked {
	if h.graph == nil {
		return nil
	}
	today := time.Now().UTC().Format("2006-01-02")
	return edition.Rank(h.graph.LiveSignals("", today), today)
}

func (h *Handler) symbols() []string {
	if h.cat == nil {
		return nil
	}
	return h.cat.AllSymbols()
}
