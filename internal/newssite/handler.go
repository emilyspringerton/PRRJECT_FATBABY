package newssite

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/newssite/edition"
	"github.com/example/prrject-fatbaby/internal/newssite/graphread"
)

// Handler is an http.Handler for the news site.
type Handler struct {
	store        eventstore.EventStore
	graph        *graphread.Store // may be nil when graph dir not configured
	logger       *log.Logger
	defaultLimit int
}

// NewHandler returns a new Handler. Call SetGraphStore to enable signal-based features.
func NewHandler(store eventstore.EventStore, logger *log.Logger) *Handler {
	return &Handler{store: store, logger: logger, defaultLimit: 50}
}

// SetGraphStore attaches a pre-loaded entity-graph store to the handler.
// Must be called before the server starts accepting requests.
func (h *Handler) SetGraphStore(gs *graphread.Store) { h.graph = gs }

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

	switch {
	case r.URL.Path == "/":
		status = h.serveFrontPage(w, r)
	case r.URL.Path == "/healthz":
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	case r.URL.Path == "/wire":
		status = h.serveWire(w, r)
	case r.URL.Path == "/breaking":
		status = h.serveBreaking(w, r)
	case r.URL.Path == "/archive":
		status = h.serveArchive(w, r)
	case r.URL.Path == "/about":
		status = h.serveAbout(w, r)
	case strings.HasPrefix(r.URL.Path, "/doc/"):
		status = h.serveDoc(w, r)
	case strings.HasPrefix(r.URL.Path, "/section/"):
		status = h.serveSection(w, r)
	case strings.HasPrefix(r.URL.Path, "/company/"):
		status = h.serveCompany(w, r)
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
	ranked := h.liveRanked()
	var buf bytes.Buffer
	RenderListPage(&buf, entries, ranked)
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
	RenderDetailPage(&buf, entry)
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
	RenderListPage(&buf, wire, nil)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveBreaking(w http.ResponseWriter, r *http.Request) int {
	ranked := h.liveRanked()
	// Filter to critical/high only.
	var breaking []edition.Ranked
	for _, r := range ranked {
		if r.Signal.Severity == "critical" || r.Signal.Severity == "high" {
			breaking = append(breaking, r)
		}
	}
	var buf bytes.Buffer
	RenderBreakingPage(&buf, breaking)
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
	RenderSectionPage(&buf, slug, filtered)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveCompany(w http.ResponseWriter, r *http.Request) int {
	ticker := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/company/"))
	ranked := h.liveRanked()
	var sigs []edition.Ranked
	for _, r := range ranked {
		if strings.ToUpper(r.Signal.Ticker) == ticker {
			sigs = append(sigs, r)
		}
	}
	entries, _ := ReadLatest(r.Context(), h.store, 200)
	var docs []DocEntry
	for _, e := range entries {
		if strings.ToUpper(strings.TrimSpace(e.Ticker)) == ticker {
			docs = append(docs, e)
		}
	}
	var buf bytes.Buffer
	RenderCompanyPage(&buf, ticker, sigs, docs)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveArchive(w http.ResponseWriter, r *http.Request) int {
	entries, err := ReadLatest(r.Context(), h.store, 500)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	var buf bytes.Buffer
	RenderArchivePage(&buf, entries)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

func (h *Handler) serveAbout(w http.ResponseWriter, r *http.Request) int {
	var buf bytes.Buffer
	RenderAboutPage(&buf)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

// liveRanked returns ranked live signals, or nil if graph is not configured.
func (h *Handler) liveRanked() []edition.Ranked {
	if h.graph == nil {
		return nil
	}
	today := time.Now().UTC().Format("2006-01-02")
	sigs := h.graph.LiveSignals("", today)
	return edition.Rank(sigs, today)
}
