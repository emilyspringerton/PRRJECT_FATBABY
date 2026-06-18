package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/earningscal"
	"github.com/example/prrject-fatbaby/internal/iamguard"
	"github.com/example/prrject-fatbaby/internal/newssite"
	"github.com/example/prrject-fatbaby/internal/newssite/catalog"
	"github.com/example/prrject-fatbaby/internal/newssite/commentary"
	"github.com/example/prrject-fatbaby/internal/newssite/docindex"
	"github.com/example/prrject-fatbaby/internal/newssite/epsread"
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
	guidanceDir      := flag.String("guidance-dir", "var/guidance", "path to guidance articles directory (empty to disable)")
	earningsCalDir   := flag.String("earnings-cal-dir", "var/earnings-calendar", "path to earnings calendar directory (empty to disable)")
	emilyURL         := flag.String("emily-url", os.Getenv("EMILY_BASE_URL"), "Emily Prime base URL for /api/ask (default: $EMILY_BASE_URL)")
	signalapiURL     := flag.String("signalapi-url", os.Getenv("SIGNALAPI_URL"), "signalapi base URL for ticker context injection (default: $SIGNALAPI_URL)")
	googleClientID   := flag.String("google-client-id", os.Getenv("GOOGLE_CLIENT_ID"), "Google OAuth client ID for Sign in with Google (default: $GOOGLE_CLIENT_ID)")
	idunaBaseURL     := flag.String("iduna-url", os.Getenv("IDUNA_BASE_URL"), "IDUNA base URL for Google→JWT exchange + JWT validation (default: $IDUNA_BASE_URL)")
	addr             := flag.String("addr", ":8082", "listen address")
	readTO    := flag.Duration("read-timeout", 10*time.Second, "")
	writeTO   := flag.Duration("write-timeout", 15*time.Second, "")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

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

	// ── Signal index + doc index — built in parallel ───────────────────────────
	sigIdx := signalindex.NewIndex()
	docIdx := docindex.NewIndex()

	var buildWG sync.WaitGroup
	buildWG.Add(2)
	go func() {
		defer buildWG.Done()
		if err := signalindex.Build(ctx, store, sigIdx, logger); err != nil {
			logger.Printf("newssite signalindex build: %v", err)
		} else {
			logger.Printf("newssite signalindex built depth=%d", sigIdx.Depth())
		}
	}()
	go func() {
		defer buildWG.Done()
		if err := docindex.Build(ctx, store, docIdx, logger); err != nil {
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
		cat.Build(sigIdx, gs, docIdx, today)
		logger.Printf("newssite catalog built tickers=%d", len(cat.AllSymbols()))

		// Tail new records into the indexes after initial build.
		go signalindex.Tail(ctx, store, sigIdx, 30*time.Second, logger)
		go docindex.Tail(ctx, store, docIdx, 30*time.Second, logger)

		// Rebuild catalog every 30s.
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cat.Build(sigIdx, gs, docIdx, time.Now().UTC().Format("2006-01-02"))
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
