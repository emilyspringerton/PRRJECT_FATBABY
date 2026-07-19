package apiserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/example/prrject-fatbaby/internal/signalindex"
)

func TestOpenAPI_NoAuthRequired(t *testing.T) {
	// Server configured *with* API keys, to prove the spec is reachable
	// without one -- unlike every other /v1/ route.
	s := New(ServerConfig{Index: signalindex.NewIndex(), APIKeys: []string{"secret"}})

	w := req(s, "/v1/openapi.json", "") // no Authorization header
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (spec should be public even with API keys configured)", w.Code)
	}
}

func TestOpenAPI_ReturnsValidJSON(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex()})
	w := req(s, "/v1/openapi.json", "")

	var spec map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if spec["openapi"] != "3.1.0" {
		t.Errorf("openapi version = %v, want 3.1.0", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatal("expected non-empty paths object")
	}
	if _, ok := paths["/v1/governance-signals"]; !ok {
		t.Error("expected /v1/governance-signals in paths")
	}
}

func TestOpenAPI_CORSHeaderSet(t *testing.T) {
	s := New(ServerConfig{Index: signalindex.NewIndex()})
	w := req(s, "/v1/openapi.json", "")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want \"*\"", got)
	}
}
