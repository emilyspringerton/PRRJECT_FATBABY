package apiserver

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/example/prrject-fatbaby/internal/earningscal"
	"github.com/example/prrject-fatbaby/internal/idunaauth"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

// ServerConfig configures the signal API server.
type ServerConfig struct {
	Addr            string
	Index           *signalindex.Index
	EarningsCal     *earningscal.Store  // optional; enables /v1/earnings-calendar
	Logger          *log.Logger
	APIKeys         []string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxLimit        int
	// IDUNAVerifier is an optional IDUNA JWT verifier. When non-nil, the server
	// accepts Bearer JWTs issued by IDUNA in addition to (not instead of) static
	// API keys. Callers with an IDUNA JWT and no API key are admitted if the
	// token carries the "fatbaby.read" permission.
	IDUNAVerifier *idunaauth.Verifier
	// MySQL is an optional *sql.DB for the CQRS read model (governance_signals, eps_results).
	// When nil, /v1/governance-signals and /v1/eps/* return 503.
	MySQL *sql.DB
	// Mongo is an optional *mongo.Client for the entity graph read model.
	// When nil, /v1/entities/* returns 503.
	Mongo   *mongo.Client
	MongoDB string // database name; defaults to "fatbaby"
}

type server struct {
	cfg     ServerConfig
	started time.Time
}

// New builds a new HTTP server for signal API endpoints.
func New(cfg ServerConfig) *http.Server {
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 30 * time.Second
	}
	if cfg.MaxLimit <= 0 {
		cfg.MaxLimit = 100
	}
	if cfg.MongoDB == "" {
		cfg.MongoDB = "fatbaby"
	}
	s := &server{cfg: cfg, started: time.Now().UTC()}
	if cfg.Logger != nil && len(cfg.APIKeys) == 0 && cfg.IDUNAVerifier == nil {
		cfg.Logger.Printf("WARNING: signal API running without authentication")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.withMiddleware(s.handleHealth))
	mux.HandleFunc("/v1/signals", s.withMiddleware(s.handleSummary))
	mux.HandleFunc("/v1/signals/", s.withMiddleware(s.dispatchSignals))
	mux.HandleFunc("/v1/earnings-calendar", s.withMiddleware(s.handleEarningsCalendar))
	// CQRS read model endpoints (S20-05).
	mux.HandleFunc("/v1/governance-signals", s.withMiddleware(s.handleGovernanceSignals))
	mux.HandleFunc("/v1/eps/{ticker}", s.withMiddleware(s.handleEPS))
	mux.HandleFunc("/v1/entities/{ticker}", s.withMiddleware(s.handleEntity))
	return &http.Server{Addr: cfg.Addr, Handler: mux, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout}
}

func (s *server) withMiddleware(next func(http.ResponseWriter, *http.Request) (int, any)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		authHeader := r.Header.Get("Authorization")
		needsAuth := len(s.cfg.APIKeys) > 0 || s.cfg.IDUNAVerifier != nil
		if needsAuth && !s.checkAuth(authHeader) {
			s.writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			s.logRequest(r, http.StatusUnauthorized, start)
			return
		}
		status, payload := next(w, r)
		s.writeJSON(w, status, payload)
		s.logRequest(r, status, start)
	}
}

// checkAuth returns true if the Authorization header satisfies at least one
// configured auth method: static API key or IDUNA JWT with fatbaby.read.
func (s *server) checkAuth(header string) bool {
	if len(s.cfg.APIKeys) > 0 && s.isAuthorized(header) {
		return true
	}
	if s.cfg.IDUNAVerifier != nil && strings.HasPrefix(header, "Bearer ") {
		tok := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if claims, err := s.cfg.IDUNAVerifier.Verify(tok); err == nil && claims.HasPermission("fatbaby.read") {
			return true
		}
	}
	return false
}

func (s *server) dispatchSignals(w http.ResponseWriter, r *http.Request) (int, any) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/signals/")
	if path == "" {
		return http.StatusNotFound, map[string]string{"error": "not found"}
	}
	if strings.HasSuffix(path, "/latest") {
		return s.handleLatestSignal(strings.TrimSuffix(path, "/latest"))
	}
	return s.handleSignalsByTicker(path, r)
}
func (s *server) handleSignalsByTicker(ticker string, r *http.Request) (int, any) {
	items, ok := s.cfg.Index.ForTicker(ticker)
	if !ok {
		return http.StatusNotFound, map[string]string{"error": "no signals for ticker " + strings.ToUpper(strings.TrimSpace(ticker))}
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > s.cfg.MaxLimit {
		limit = s.cfg.MaxLimit
	}
	from := time.Time{}
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return http.StatusBadRequest, map[string]string{"error": "invalid from; must be RFC3339"}
		}
		from = t
	}
	sigType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("signal_type")))
	minImp := -1
	if v := r.URL.Query().Get("min_importance"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return http.StatusBadRequest, map[string]string{"error": "invalid min_importance"}
		}
		minImp = n
	}
	out := make([]*signalindex.SignalEntry, 0, min(limit, len(items)))
	for _, it := range items {
		if !from.IsZero() && it.Timestamp.Before(from) {
			continue
		}
		if sigType != "" && strings.ToLower(it.SignalType) != sigType {
			continue
		}
		if minImp >= 0 && it.Importance < minImp {
			continue
		}
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return http.StatusOK, map[string]any{"ticker": strings.ToUpper(strings.TrimSpace(ticker)), "count": len(out), "signals": out}
}
func (s *server) handleLatestSignal(ticker string) (int, any) {
	it, ok := s.cfg.Index.Latest(ticker)
	if !ok {
		return http.StatusNotFound, map[string]string{"error": "no signals for ticker " + strings.ToUpper(strings.TrimSpace(ticker))}
	}
	return http.StatusOK, it
}
func (s *server) handleSummary(http.ResponseWriter, *http.Request) (int, any) {
	summary := s.cfg.Index.Summary()
	tickers := len(summary)
	return http.StatusOK, map[string]any{"tickers": tickers, "total_signals": s.cfg.Index.Depth(), "index_depth": s.cfg.Index.Depth(), "latest_seq": s.cfg.Index.LatestSeq(), "summary": summary}
}
// handleEarningsCalendar serves GET /v1/earnings-calendar.
// Query params:
//   ticker   — comma-separated ticker filter (e.g. "AAPL,MSFT")
//   from     — YYYY-MM-DD lower bound on ReportDate (inclusive)
//   to       — YYYY-MM-DD upper bound on ReportDate (inclusive)
//   status   — comma-separated status filter (confirmed|announced|backfilled)
//   upcoming — "1" to return only future dates (>= today); overrides from
//   limit    — max results (default 50, capped at MaxLimit)
//
// Returns 503 when the EarningsCal store is not configured.
func (s *server) handleEarningsCalendar(_ http.ResponseWriter, r *http.Request) (int, any) {
	if s.cfg.EarningsCal == nil {
		return http.StatusServiceUnavailable, map[string]string{"error": "earnings calendar not configured"}
	}
	q := r.URL.Query()

	var tickers []string
	if t := strings.TrimSpace(q.Get("ticker")); t != "" {
		for _, p := range strings.Split(t, ",") {
			if p = strings.TrimSpace(strings.ToUpper(p)); p != "" {
				tickers = append(tickers, p)
			}
		}
	}
	var statuses []string
	if st := strings.TrimSpace(q.Get("status")); st != "" {
		for _, p := range strings.Split(st, ",") {
			if p = strings.TrimSpace(p); p != "" {
				statuses = append(statuses, p)
			}
		}
	}

	from := strings.TrimSpace(q.Get("from"))
	to := strings.TrimSpace(q.Get("to"))
	if q.Get("upcoming") == "1" {
		today := time.Now().UTC().Format("2006-01-02")
		if from == "" || from < today {
			from = today
		}
	}

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > s.cfg.MaxLimit {
		limit = s.cfg.MaxLimit
	}

	results := s.cfg.EarningsCal.Query(tickers, from, to, statuses)
	if len(results) > limit {
		results = results[:limit]
	}
	return http.StatusOK, map[string]any{
		"count":    len(results),
		"calendar": results,
	}
}

func (s *server) handleHealth(http.ResponseWriter, *http.Request) (int, any) {
	summary := s.cfg.Index.Summary()
	return http.StatusOK, map[string]any{"ok": true, "index_depth": s.cfg.Index.Depth(), "tickers": len(summary), "latest_seq": s.cfg.Index.LatestSeq(), "uptime_seconds": int(time.Since(s.started).Seconds())}
}
func (s *server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func (s *server) isAuthorized(h string) bool {
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	for _, key := range s.cfg.APIKeys {
		if subtle.ConstantTimeCompare([]byte(tok), []byte(key)) == 1 {
			return true
		}
	}
	return false
}
func (s *server) logRequest(r *http.Request, status int, start time.Time) {
	if s.cfg.Logger != nil {
		s.cfg.Logger.Printf("%s %s %d %d %d", r.Method, r.URL.Path, status, time.Since(start).Milliseconds(), s.cfg.Index.Depth())
	}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
