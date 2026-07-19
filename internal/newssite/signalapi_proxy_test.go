package newssite

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/prrject-fatbaby/eventstore"
)

// TestProxySignalAPI_StripsPrefixAndForwards is the regression test for the
// bug found 2026-07-19: the API playground used to embed an absolute
// http://localhost:9091 spec URL, which only worked when the visitor's
// browser happened to be on this same box. This confirms newssite's own
// /signalapi/* handler correctly proxies same-origin instead.
func TestProxySignalAPI_StripsPrefixAndForwards(t *testing.T) {
	var gotPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"openapi":"3.1.0"}`))
	}))
	defer fake.Close()

	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	h := NewHandler(store, log.New(io.Discard, "", 0))
	h.SetSignalapiURL(fake.URL)

	req := httptest.NewRequest(http.MethodGet, "/signalapi/v1/openapi.json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/v1/openapi.json" {
		t.Errorf("upstream saw path %q, want /v1/openapi.json (the /signalapi prefix should be stripped)", gotPath)
	}
	if !strings.Contains(w.Body.String(), `"openapi":"3.1.0"`) {
		t.Errorf("response body not forwarded correctly: %s", w.Body.String())
	}
}

func TestServeAPIPlayground_UsesRelativeURL(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	h := NewHandler(store, log.New(io.Discard, "", 0))
	req := httptest.NewRequest(http.MethodGet, "/api-playground", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "localhost:9091") {
		t.Error("playground page must not embed an absolute localhost URL -- that only works for visitors on this same box")
	}
	if !strings.Contains(body, `url: "/signalapi/v1/openapi.json"`) {
		t.Errorf("expected a relative same-origin spec URL, got: %s", body)
	}
}
