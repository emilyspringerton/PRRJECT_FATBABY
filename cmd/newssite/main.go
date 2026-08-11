package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/earningscal"
	"github.com/example/prrject-fatbaby/internal/iamguard"
	"github.com/example/prrject-fatbaby/internal/indexcheckpoint"
	"github.com/example/prrject-fatbaby/internal/newssite"
	"github.com/example/prrject-fatbaby/internal/newssite/catalog"
	"github.com/example/prrject-fatbaby/internal/newssite/commentary"
	"github.com/example/prrject-fatbaby/internal/newssite/companybios"
	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/newssite/epsread"
	"github.com/example/prrject-fatbaby/internal/newssite/marketdata"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
	"github.com/example/prrject-fatbaby/internal/newssite/guidanceread"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

// headResponseWriter wraps a ResponseWriter and discards Write calls,
// letting headers (including status) pass through unchanged.
type headResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (hw *headResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (hw *headResponseWriter) WriteHeader(code int) {
	hw.wroteHeader = true
	hw.ResponseWriter.WriteHeader(code)
}

func main() {
	storeRoot        := flag.String("store", "var/secwatch", "path to eventstore root")
	graphDir         := flag.String("graph-dir", "var/entity-graph", "path to entity-graph directory (empty to disable)")
	epsDir           := flag.String("eps-dir", "var/eps", "path to eps output directory (empty to disable)")
	commentaryDir    := flag.String("commentary-dir", "var/commentary", "path to Emily commentary directory (empty to disable)")
	commentaryAPIKeys := flag.String("commentary-api-keys", os.Getenv("COMMENTARY_API_KEY"), "comma-separated bearer tokens allowed to POST /api/commentary (default: $COMMENTARY_API_KEY); empty disables the endpoint (fails closed)")
	guidanceDir      := flag.String("guidance-dir", "var/guidance", "path to guidance articles directory (empty to disable)")
	earningsCalDir   := flag.String("earnings-cal-dir", "var/earnings-calendar", "path to earnings calendar directory (empty to disable)")
	marketDataDir    := flag.String("market-data-dir", "var/market-data", "path to market-data eventstore root (empty to disable; charts fall back to a transparent placeholder)")
	companyBiosPath  := flag.String("company-bios-path", "config/company_bios.json", "path to draft company bios JSON (empty to disable)")
	emilyURL         := flag.String("emily-url", os.Getenv("EMILY_BASE_URL"), "Emily Prime base URL for /api/ask (default: $EMILY_BASE_URL)")
	signalapiURL     := flag.String("signalapi-url", os.Getenv("SIGNALAPI_URL"), "signalapi base URL for ticker context injection (default: $SIGNALAPI_URL)")
	googleClientID   := flag.String("google-client-id", os.Getenv("GOOGLE_CLIENT_ID"), "Google OAuth client ID for Sign in with Google (default: $GOOGLE_CLIENT_ID)")
	idunaBaseURL     := flag.String("iduna-url", os.Getenv("IDUNA_BASE_URL"), "IDUNA base URL for Google→JWT exchange + JWT validation (default: $IDUNA_BASE_URL)")
	addr             := flag.String("addr", ":8082", "listen address")
	readTO    := flag.Duration("read-timeout", 10*time.Second, "")
	writeTO   := flag.Duration("write-timeout", 15*time.Second, "")
	replayFromSeq := flag.Uint64("replay-from-seq", 1, "emergency degraded-mode lever: start signal/doc index rebuild at this sequence instead of full history (see PRRJECT_FATBABY/docs/northstar/replay-fragility.md §5)")
	indexDBPath := flag.String("index-db", "", "checkpoint SQLite file (default: <store's parent dir>/newssite-index.db) -- see docs/northstar/replay-fragility.md §4b Phase 1b")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	if *replayFromSeq != 1 {
		logger.Printf("WARNING: -replay-from-seq=%d — starting index below full history, degraded mode", *replayFromSeq)
	}

	store, err := eventstore.NewFileStore(*storeRoot)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h := newssite.NewHandler(store, logger)
	if *emilyURL != "" {
		h.SetEmilyBaseURL(*emilyURL)
		logger.Printf("Ask Emily enabled: emily_url=%s → POST /api/ask", *emilyURL)
	}
	if *signalapiURL != "" {
		h.SetSignalapiURL(*signalapiURL)
		logger.Printf("Ask Emily signal context enabled: signalapi_url=%s", *signalapiURL)
	}
	if *googleClientID != "" {
		h.SetGoogleClientID(*googleClientID)
		logger.Printf("Ask Emily auth enabled: google_client_id configured")
	}
	if *idunaBaseURL != "" {
		h.SetIdunaBaseURL(*idunaBaseURL)
		if jwksURL := *idunaBaseURL + "/.well-known/jwks.json"; *googleClientID != "" {
			if g, err := iamguard.New(jwksURL); err == nil {
				h.SetAskJWTVerifier(g)
				logger.Printf("Ask Emily JWT verification enabled: jwks_url=%s", jwksURL)
			} else {
				logger.Printf("iamguard: ask-emily JWKS init failed (%v) — JWT verification disabled", err)
			}
		}
	}

	// ── Entity-graph store (fast: just reads NDJSON files) ────────────────────
	var gs *graphread.Store
	if *graphDir != "" {
		gs = graphread.NewStore(*graphDir)
		gs.SetRulesFile("config/entity-graph-rules.json")
		if err := gs.Refresh(); err != nil {
			logger.Printf("newssite graph load: %v", err)
		} else {
			logger.Printf("newssite graph loaded from %s", *graphDir)
		}
		h.SetGraphStore(gs)
		done := make(chan struct{})
		gs.StartRefresh(30*time.Second, func(f string, a ...any) { logger.Printf(f, a...) }, done)
		defer close(done)
	}

	// ── EPS article store ─────────────────────────────────────────────────────
	if *epsDir != "" {
		es := epsread.NewStore(*epsDir)
		if err := es.Refresh(); err != nil {
			logger.Printf("newssite eps load: %v", err)
		} else {
			logger.Printf("newssite eps loaded count=%d from %s", es.Count(), *epsDir)
		}
		h.SetEpsStore(es)
		done := make(chan struct{})
		es.StartRefresh(60*time.Second, func(f string, a ...any) { logger.Printf(f, a...) }, done)
		defer close(done)
	}

	// ── Commentary store (Emily-authored governance articles) ─────────────────
	if *commentaryDir != "" {
		cs := commentary.NewStore(*commentaryDir)
		if err := cs.Refresh(); err != nil {
			logger.Printf("newssite commentary load: %v", err)
		} else {
			logger.Printf("newssite commentary loaded from %s", *commentaryDir)
		}
		h.SetCommentaryStore(cs, *commentaryDir)
		if *commentaryAPIKeys != "" {
			h.SetCommentaryAPIKeys(splitCSV(*commentaryAPIKeys))
		} else {
			logger.Printf("WARNING: no commentary API keys configured -- POST /api/commentary will fail closed (503)")
		}
	}

	// ── Guidance store ───────────────────────────────────────────────────────────
	if *guidanceDir != "" {
		gs := guidanceread.NewStore(*guidanceDir)
		if err := gs.Refresh(); err != nil {
			logger.Printf("newssite guidance load: %v", err)
		} else {
			logger.Printf("newssite guidance loaded count=%d from %s", gs.Count(), *guidanceDir)
		}
		h.SetGuidanceStore(gs)
		done := make(chan struct{})
		gs.StartRefresh(60*time.Second, func(f string, a ...any) { logger.Printf(f, a...) }, done)
		defer close(done)
	}

	// ── Company bios (draft, one-shot LLM pass; EMILY/BACKLOG.md S166-03/S170-22) ─
	if *companyBiosPath != "" {
		cb := companybios.NewStore(*companyBiosPath)
		if err := cb.Refresh(); err != nil {
			logger.Printf("newssite company-bios load: %v", err)
		} else {
			logger.Printf("newssite company-bios loaded count=%d from %s", cb.Count(), *companyBiosPath)
		}
		h.SetCompanyBiosStore(cb)
	}

	// ── Earnings calendar store ──────────────────────────────────────────────────
	if *earningsCalDir != "" {
		ecs := earningscal.NewStore(*earningsCalDir)
		if err := ecs.Refresh(); err != nil {
			logger.Printf("newssite earnings-cal load: %v", err)
		} else {
			logger.Printf("newssite earnings-cal loaded count=%d from %s", ecs.Count(), *earningsCalDir)
		}
		h.SetEarningsCalStore(ecs)
	}

	// ── Market data store (own-ingested OHLCV -> the chart sparkline's only
	// data source; never a live third-party fetch at render time) ─────────────
	// Also the source for catalog.TickerRow.MarketCap (see cat.Build calls
	// below) -- mdIdx is hoisted to this outer scope so both the chart
	// handler and the catalog builder can share the same live store. It's
	// safe to hand a *marketdata.Store to catalog.Build before its own
	// Build goroutine finishes: LatestMarketCap just reports ok=false for
	// anything not ingested yet, same eventual-consistency tolerance this
	// file already has for sigIdx/docIdx below.
	var mdIdx *marketdata.Store
	if *marketDataDir != "" {
		if mdStoreRaw, err := eventstore.NewFileStore(*marketDataDir); err != nil {
			logger.Printf("newssite market-data store open: %v (charts will use placeholder)", err)
		} else {
			mdIdx = marketdata.NewStore()
			go func() {
				if err := marketdata.Build(ctx, mdStoreRaw, mdIdx, logger); err != nil {
					logger.Printf("newssite marketdata build: %v", err)
				}
				go marketdata.Tail(ctx, mdStoreRaw, mdIdx, 5*time.Minute, logger)
			}()
			h.SetMarketData(mdIdx)
		}
	}

	// ── Checkpoint — see docs/northstar/replay-fragility.md §4b Phase 1b ──────
	// Loaded synchronously, before the Build goroutines below launch: the
	// hydration itself is fast (subsecond for thousands of rows), and doing
	// it first means the HTTP server -- which SetSignalIndex/SetDocIndex
	// makes live immediately, before Build finishes -- serves warm data from
	// its very first request instead of the empty-index window that caused
	// the "we don't cover AMZN" symptom during the original incident.
	ckptPath := *indexDBPath
	if ckptPath == "" {
		ckptPath = filepath.Join(filepath.Dir(*storeRoot), "newssite-index.db")
	}
	ckpt, err := indexcheckpoint.Open(ckptPath, logger)
	if err != nil {
		logger.Fatalf("open index checkpoint: %v", err)
	}
	defer ckpt.Close()

	// ── Signal index + doc index — built in parallel ───────────────────────────
	sigIdx := signalindex.NewIndex()
	docIdx := docindex.NewIndex()
	signalsFrom, docsFrom := *replayFromSeq, *replayFromSeq

	if *replayFromSeq == 1 {
		if cached, cerr := ckpt.LoadSignals(); cerr != nil {
			logger.Printf("WARNING: load signals checkpoint failed (%v); full rebuild", cerr)
		} else if len(cached) > 0 {
			sigIdx.LoadEntries(cached)
			signalsFrom = ckpt.SignalsLatestSeq() + 1
			logger.Printf("newssite signalindex warm start from checkpoint: %d entries, resuming at seq %d", len(cached), signalsFrom)
		}
		if cachedDocs, cerr := ckpt.LoadDocs(); cerr != nil {
			logger.Printf("WARNING: load docs checkpoint failed (%v); full rebuild", cerr)
		} else if len(cachedDocs) > 0 {
			docIdx.LoadSummaries(cachedDocs)
			docsFrom = ckpt.DocsLatestSeq() + 1
			logger.Printf("newssite docindex warm start from checkpoint: %d docs, resuming at seq %d", len(cachedDocs), docsFrom)
		}
	}

	var buildWG sync.WaitGroup
	buildWG.Add(2)
	go func() {
		defer buildWG.Done()
		if err := signalindex.Build(ctx, store, sigIdx, signalsFrom, logger); err != nil {
			logger.Printf("newssite signalindex build: %v", err)
		} else {
			logger.Printf("newssite signalindex built depth=%d", sigIdx.Depth())
		}
	}()
	go func() {
		defer buildWG.Done()
		if err := docindex.Build(ctx, store, docIdx, docsFrom, logger); err != nil {
			logger.Printf("newssite docindex build: %v", err)
		} else {
			logger.Printf("newssite docindex built tickers=%d", len(docIdx.KnownTickers()))
		}
	}()

	h.SetSignalIndex(sigIdx)
	h.SetDocIndex(docIdx)

	// ── Catalog — built once indexes are ready, refreshed on a timer ──────────
	cat := catalog.New()
	h.SetCatalog(cat)

	go func() {
		buildWG.Wait() // wait for both index builds to finish
		today := time.Now().UTC().Format("2006-01-02")
		cat.Build(sigIdx, gs, docIdx, mdIdx, today)
		logger.Printf("newssite catalog built tickers=%d", len(cat.AllSymbols()))

		// Checkpoint watermark = the store's true current end sequence, not
		// sigIdx/docIdx.LatestSeq() (highest sequence among only *matching*
		// records) -- see cmd/signalapi/main.go's saveNewssiteCheckpoint-
		// equivalent comment; same bug, same fix, found once and applied to
		// both binaries rather than rediscovered here independently.
		saveNewssiteCheckpoint(ctx, store, ckpt, sigIdx, docIdx, logger)

		// Tail new records into the indexes after initial build.
		go signalindex.Tail(ctx, store, sigIdx, 30*time.Second, logger)
		go docindex.Tail(ctx, store, docIdx, 30*time.Second, logger)

		// Periodic checkpoint sync, same shape as cmd/signalapi.
		go func() {
			ckptTicker := time.NewTicker(30 * time.Second)
			defer ckptTicker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ckptTicker.C:
					saveNewssiteCheckpoint(ctx, store, ckpt, sigIdx, docIdx, logger)
				}
			}
		}()

		// Rebuild catalog every 30s.
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cat.Build(sigIdx, gs, docIdx, mdIdx, time.Now().UTC().Format("2006-01-02"))
			}
		}
	}()

	// ── IDUNA IAM guard for machine-consumed endpoints ───────────────────────
	// Human-facing HTML pages are public; machine-consumed streams and JSON APIs
	// require fatbaby.read when IDUNA is configured. Falls back to no-op guard.
	guard, err := iamguard.NewFromEnv()
	if err != nil {
		logger.Printf("iamguard: JWKS init failed (%v) — /live/events and /api/tickers will be unprotected", err)
		guard = &iamguard.Guard{}
	}
	if guard.IsActive() {
		logger.Printf("iamguard: newssite /live/events and /api/tickers protected by IDUNA JWT (fatbaby.read)")
	}
	// headToGet converts HEAD requests into GET requests with body suppressed.
	// Go's ServeMux does not auto-respond to HEAD on GET handlers, so without
	// this wrapper all routes return 405 on HEAD (breaks SEO crawlers).
	headToGet := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				r2 := r.Clone(r.Context())
				r2.Method = http.MethodGet
				nw := &headResponseWriter{ResponseWriter: w}
				next.ServeHTTP(nw, r2)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// Wrap only the machine-consumed endpoints; all other paths use h directly.
	mux := http.NewServeMux()
	mux.Handle("/live/events", guard.RequirePermission("fatbaby.read")(h))
	mux.Handle("/api/tickers", guard.RequirePermission("fatbaby.read")(h))
	mux.Handle("/", headToGet(h))

	// ── HTTP server starts immediately; indexes fill in behind it ─────────────
	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  *readTO,
		WriteTimeout: *writeTO,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	logger.Printf("newssite listening addr=%s store=%s graph-dir=%s", *addr, *storeRoot, *graphDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("listen: %v", err)
	}
}

// saveNewssiteCheckpoint upserts both indexes' current full state under a
// single shared watermark: the store's true end sequence, not
// sigIdx/docIdx.LatestSeq() (see cmd/signalapi/main.go's saveCheckpoint for
// the full reasoning -- same fix applied here, not rediscovered).
func saveNewssiteCheckpoint(ctx context.Context, evStore eventstore.EventStore, ckpt *indexcheckpoint.DB, sigIdx *signalindex.Index, docIdx *docindex.Index, logger *log.Logger) {
	endSeq, err := evStore.LatestSequence(ctx)
	if err != nil {
		logger.Printf("WARNING: get store latest sequence failed (%v); checkpointing at index watermark instead", err)
		endSeq = max(sigIdx.LatestSeq(), docIdx.LatestSeq())
	}
	if err := ckpt.SaveSignals(sigIdx.AllEntries(), endSeq); err != nil {
		logger.Printf("WARNING: checkpoint signals save failed: %v", err)
	}
	if err := ckpt.SaveDocs(docIdx.AllSummaries(), endSeq); err != nil {
		logger.Printf("WARNING: checkpoint docs save failed: %v", err)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
