package newssite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	"github.com/example/prrject-fatbaby/internal/newssite/marketdata"
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
	store                eventstore.EventStore
	graph                *graphread.Store    // nil if graph dir not configured
	sigIdx               *signalindex.Index  // nil if not wired
	docIdx               *docindex.Index     // nil if not wired
	cat                  *catalog.Catalog    // nil if not wired
	epsStore             *epsread.Store      // nil if eps-dir not configured
	commentaryStore      *commentary.Store   // nil if commentary-dir not configured
	commentaryDir        string              // path for POST /api/commentary writes
	guidanceStore        *guidanceread.Store // nil if guidance-dir not configured
	earningsCalStore     *earningscal.Store  // nil if earnings-cal-dir not configured
	marketData           *marketdata.Store   // nil if market-data-dir not configured
	emilyBaseURL         string              // Emily Prime base URL for /api/ask; empty disables
	signalapiURL         string              // signalapi base URL for ticker context injection; empty skips
	googleClientID       string              // Google OAuth client ID for Sign in with Google; empty disables auth flow
	idunaBaseURL         string              // IDUNA base URL for Google→IDUNA JWT exchange; empty disables
	askJWTVerifier       askVerifier         // JWT verifier for authenticated /api/ask; nil → no auth
	logger               *log.Logger
	defaultLimit         int
	rateLimiter          *ipRateLimiter // free-tier IP rate limiter; nil disables limiting
	askRateLimiter       *ipRateLimiter // Ask Emily rate limiter (5/day); nil disables
	authedAskRateLimiter *ipRateLimiter // authenticated Ask Emily rate limiter (20/day); nil disables
}

// askVerifier is a minimal interface so we can swap in iamguard.Guard without importing it directly.
type askVerifier interface {
	VerifyTokenSub(token string) (sub string, err error)
	VerifyTokenPermissions(token string) (sub string, permissions []string, err error)
}

// NewHandler returns a new Handler.
func NewHandler(store eventstore.EventStore, logger *log.Logger) *Handler {
	return &Handler{
		store:                store,
		logger:               logger,
		defaultLimit:         50,
		rateLimiter:          newIPRateLimiter(),
		askRateLimiter:       newAskRateLimiter(),
		authedAskRateLimiter: newAuthedAskRateLimiter(),
	}
}

// SetRateLimiter replaces the rate limiter. Pass nil to disable rate limiting (e.g. in tests).
func (h *Handler) SetRateLimiter(rl *ipRateLimiter) { h.rateLimiter = rl }

func (h *Handler) SetGraphStore(gs *graphread.Store)    { h.graph = gs }
func (h *Handler) SetSignalIndex(si *signalindex.Index) { h.sigIdx = si }
func (h *Handler) SetDocIndex(di *docindex.Index)       { h.docIdx = di }
func (h *Handler) SetCatalog(c *catalog.Catalog)        { h.cat = c }
func (h *Handler) SetEpsStore(es *epsread.Store)        { h.epsStore = es }
func (h *Handler) SetMarketData(ms *marketdata.Store)   { h.marketData = ms }
func (h *Handler) SetCommentaryStore(cs *commentary.Store, dir string) {
	h.commentaryStore = cs
	h.commentaryDir = dir
}
func (h *Handler) SetGuidanceStore(gs *guidanceread.Store)  { h.guidanceStore = gs }
func (h *Handler) SetEarningsCalStore(s *earningscal.Store) { h.earningsCalStore = s }

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

	// POST /api/auth/google — proxy Google ID token → IDUNA JWT (S21-05).
	if path == "/api/auth/google" && r.Method == http.MethodPost {
		status = h.serveAuthGoogle(w, r)
		return
	}

	// POST /api/waitlist — email capture for Emily+ waitlist (S21-06).
	if path == "/api/waitlist" && r.Method == http.MethodPost {
		status = h.serveWaitlist(w, r)
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
	case path == "/ask":
		status = h.serveAskLanding(w, r)
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
	case path == "/api-playground":
		status = h.serveAPIPlayground(w, r)
	case strings.HasPrefix(path, "/signalapi/"):
		status = h.proxySignalAPI(w, r)
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
	case path == "/section/movers":
		status = h.serveMoversSection(w, r)
	case strings.HasPrefix(path, "/commentary/"):
		status = h.serveCommentary(w, r)
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

	// Fast path: O(1) sequence lookup via the in-memory index, then a
	// targeted single-record fetch — touches at most one day-partition
	// file, not the entire event-store history. Falls back to the slow
	// full-scan only if the index doesn't have this identity yet (e.g.
	// mid-rebuild after a restart), matching prior behavior in that case.
	var entry DocEntry
	var found bool
	var err error
	if h.docIdx != nil {
		if summary, ok := h.docIdx.ByIdentity(id); ok {
			entry, found, err = ReadAtSequence(r.Context(), h.store, summary.Sequence)
		}
	}
	if !found && err == nil {
		entry, found, err = ReadByIdentity(r.Context(), h.store, id)
	}
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

// serveCommentary serves the detail page for one Emily-authored commentary
// article at /commentary/{id} -- commentaryToEntry has generated links here
// since this package was written, but nothing ever served the route itself
// until this (found + fixed 2026-07-19 while wiring up the first real
// commentary content, "Stocks on the Move").
func (h *Handler) serveCommentary(w http.ResponseWriter, r *http.Request) int {
	id := strings.TrimPrefix(r.URL.Path, "/commentary/")
	if h.commentaryStore == nil {
		http.NotFound(w, r)
		return http.StatusNotFound
	}
	art, ok := h.commentaryStore.ByID(id)
	if !ok {
		http.NotFound(w, r)
		return http.StatusNotFound
	}
	var buf bytes.Buffer
	RenderDetailPage(&buf, commentaryToEntry(art), h.symbols())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

// serveMoversSection serves /section/movers -- the "Stocks on the Move"
// article list, newest first.
func (h *Handler) serveMoversSection(w http.ResponseWriter, r *http.Request) int {
	var entries []DocEntry
	if h.commentaryStore != nil {
		for _, a := range h.commentaryStore.ByKind("market_movers", 30) {
			entries = append(entries, commentaryToEntry(a))
		}
	}
	var buf bytes.Buffer
	RenderListPage(&buf, entries, nil, nil, h.symbols())
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
	var nextEarnings *UpcomingEarningsView
	var pastEarnings []UpcomingEarningsView
	if h.earningsCalStore != nil {
		// Query sorts ascending by (ISO) ReportDate — split on that before
		// formatting, since formatted strings ("Jan 2, 2006") don't compare
		// chronologically.
		const maxPast = 4
		for _, d := range h.earningsCalStore.Query([]string{symbol}, "", "", nil) {
			uv := UpcomingEarningsView{
				Ticker:      strings.ToUpper(d.Ticker),
				ReportDate:  formatUpcomingDate(d.ReportDate),
				PeriodStr:   formatPeriodStr(d.FiscalQuarter, d.FiscalYear),
				StatusLabel: earningsStatusLabel(d.Status),
			}
			if d.BeforeMarket != nil {
				if *d.BeforeMarket {
					uv.Timing = "BMO"
				} else {
					uv.Timing = "AMC"
				}
			}
			if d.ReportDate >= today {
				if nextEarnings == nil {
					nextEarnings = &uv
				}
				continue
			}
			pastEarnings = append(pastEarnings, uv)
		}
		// pastEarnings arrived oldest-first (ascending); keep the most
		// recent maxPast entries, newest first.
		if len(pastEarnings) > maxPast {
			pastEarnings = pastEarnings[len(pastEarnings)-maxPast:]
		}
		for i, j := 0, len(pastEarnings)-1; i < j; i, j = i+1, j-1 {
			pastEarnings[i], pastEarnings[j] = pastEarnings[j], pastEarnings[i]
		}
	}
	RenderTickerPage(&buf, symbol, row, ranked, directors, secDocs, wireDocs, tickerEPS, nextEarnings, pastEarnings, h.symbols())
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

// signalAPIProxyTarget is where /signalapi/* requests actually get forwarded.
// Bug found 2026-07-19: this used to be hardcoded as an absolute
// http://localhost:9091 URL directly in the Swagger UI page, which only
// ever worked when the *browser* happened to be running on this same box --
// for any real visitor, "localhost" resolved to their own machine, so the
// spec silently failed to load. Fixed by having newssite itself
// reverse-proxy /signalapi/ same-origin (below), so the playground can use
// a relative URL that works from any domain newssite is actually served on.
var signalAPIProxyTarget = &url.URL{Scheme: "http", Host: "127.0.0.1:9091"}

// proxySignalAPI reverse-proxies GET /signalapi/* to the real signalapi
// process, stripping the /signalapi prefix. Same-origin so the API
// playground (and anything else) can reach it with a relative URL, no CORS,
// no hardcoded hostname.
func (h *Handler) proxySignalAPI(w http.ResponseWriter, r *http.Request) int {
	target := signalAPIProxyTarget
	if h.signalapiURL != "" {
		if u, err := url.Parse(h.signalapiURL); err == nil {
			target = u
		}
	}
	upstreamStatus := http.StatusOK
	proxy := httputil.NewSingleHostReverseProxy(target)
	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		orig(req)
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/signalapi")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		upstreamStatus = resp.StatusCode
		return nil
	}
	proxy.ServeHTTP(w, r)
	return upstreamStatus
}

// serveAPIPlayground serves a Swagger UI page against the FatBaby Signal
// API's OpenAPI spec, fetched same-origin via proxySignalAPI above (not a
// hardcoded absolute URL -- see the note on signalAPIProxyTarget for why
// that broke for every real visitor). Same CDN-loaded-Swagger-UI pattern as
// OKEMILY's api-playground.html, rendered server-side here since newssite
// doesn't serve static files from a directory the way OKEMILY does.
func (h *Handler) serveAPIPlayground(w http.ResponseWriter, r *http.Request) int {
	const specURL = "/signalapi/v1/openapi.json"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>API Playground &mdash; FatBaby Signal API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>
  body { margin: 0; background: #fafafa; }
  .topbar { display: none !important; }
  .our-header {
    padding: 1rem 2rem; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
    border-bottom: 1px solid #e4e4e8; background: #fff;
  }
  .our-header a { color: #4451c7; text-decoration: none; font-weight: 600; }
  .our-header p { color: #4a4f5c; font-size: 0.9rem; margin: 0.4rem 0 0; }
</style>
</head>
<body>
<div class="our-header">
  <a href="/">&larr; FATBABY</a>
  <p>Live API playground for the FatBaby Signal API. Most endpoints require a Bearer token
  (a signalapi API key, or an IDUNA JWT with fatbaby.read) &mdash; the spec itself is public.</p>
</div>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
<script>
  window.onload = function() {
    SwaggerUIBundle({
      url: %q,
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis],
      layout: 'BaseLayout',
    });
  };
</script>
</body>
</html>`, specURL)
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
				Ticker:      strings.ToUpper(d.Ticker),
				ReportDate:  formatUpcomingDate(d.ReportDate),
				PeriodStr:   formatPeriodStr(d.FiscalQuarter, d.FiscalYear),
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
// used by all newssite renderers. SourceType is derived from a.Kind for the
// kinds that get their own kicker/byline treatment in render.go
// (currently "market_movers"); every other Kind (including the empty string
// -- most existing commentary predates the Kind field) falls back to the
// generic "emily_commentary" style, unchanged from prior behavior.
func commentaryToEntry(a *commentary.Article) DocEntry {
	preview := a.Preview
	if preview == "" {
		runes := []rune(a.Body)
		if len(runes) > 300 {
			runes = runes[:300]
		}
		preview = strings.TrimSpace(string(runes))
	}
	sourceType := "emily_commentary"
	if a.Kind == "market_movers" {
		sourceType = "market_movers"
	}
	return DocEntry{
		Identity:    a.ID,
		Headline:    a.Headline,
		Ticker:      strings.ToUpper(strings.TrimSpace(a.Ticker)),
		SourceType:  sourceType,
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
	var svg string
	if h.marketData != nil {
		bars := h.marketData.SeriesFor(sym, 90) // ~3mo of trading days
		pts := make([]chart.PricePoint, 0, len(bars))
		for _, b := range bars {
			pts = append(pts, chart.PricePoint{Time: b.Timestamp, Close: b.Close})
		}
		svg = chart.SVGCachedFromPoints(sym, pts)
	}
	if svg == "" {
		// No data — return a transparent 1×1 SVG placeholder.
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	fmt.Fprint(w, svg)
	return http.StatusOK
}
