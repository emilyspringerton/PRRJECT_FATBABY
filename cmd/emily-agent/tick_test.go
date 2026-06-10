package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newServerForTest(t *testing.T, anthropicURL string) *Server {
	t.Helper()
	s := NewServer(Config{
		Model:        "test",
		APIKey:       "test",
		RateLimitRPM: 6000,
		MaxToolIters: 2,
		FatbabyRoot:  t.TempDir(),
	})
	s.anthropicURL = anthropicURL
	return s
}

func TestTickRequiresPOST(t *testing.T) {
	s := newServerForTest(t, "http://invalid.invalid")
	req := httptest.NewRequest(http.MethodGet, "/tick", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /tick = %d, want 405", w.Code)
	}
}

func TestTickHappyPathRepliesOK(t *testing.T) {
	var seenSystemText string
	var seenMessages []map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// system is now sent as an array of content blocks (required for prompt caching).
		var payload struct {
			System   []map[string]any `json:"system"`
			Messages []map[string]any `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		for _, block := range payload.System {
			if t, ok := block["text"].(string); ok {
				seenSystemText += t
			}
		}
		seenMessages = payload.Messages
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer fake.Close()

	s := newServerForTest(t, fake.URL)
	req := httptest.NewRequest(http.MethodPost, "/tick", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Reply     string              `json:"reply"`
		ToolCalls []map[string]string `json:"tool_calls"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v body=%s", err, w.Body.String())
	}
	if resp.Reply != "ok" {
		t.Errorf("reply = %q, want ok", resp.Reply)
	}
	if !strings.Contains(seenSystemText, "Emily") {
		t.Errorf("system prompt missing Emily identity: %s", seenSystemText)
	}
	if len(seenMessages) != 1 {
		t.Fatalf("expected 1 seeded message, got %d", len(seenMessages))
	}
	content, _ := seenMessages[0]["content"].(string)
	if !strings.Contains(content, "health sweep") {
		t.Errorf("tick prompt missing 'health sweep': %q", content)
	}
}

func TestTickPromptInstructsToReadFirst(t *testing.T) {
	// Lock down the contract: the tick prompt must tell Emily to consult any
	// open observation before writing a new one, or she'll trample her own
	// findings on every tick.
	for _, want := range []string{"fatbaby_read_observation", "fatbaby_write_observation", "health sweep", "Never write"} {
		if !strings.Contains(tickPrompt, want) {
			t.Errorf("tickPrompt missing %q", want)
		}
	}
}
