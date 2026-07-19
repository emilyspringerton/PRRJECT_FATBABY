package newssite

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mkHandlerForIngest builds a Handler with an empty, writable commentary
// store and the given API keys configured -- regression coverage for the
// 2026-07-19 fix: POST /api/commentary had no authentication at all before
// this (EMILY/BACKLOG.md SECTION 167, S167-01).
func mkHandlerForIngest(t *testing.T, keys []string) *Handler {
	t.Helper()
	h := mkHandlerWithCommentary(t) // empty commentary store, writable dir
	h.SetCommentaryAPIKeys(keys)
	return h
}

func postCommentaryReq(t *testing.T, h *Handler, authHeader string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/commentary", bytes.NewReader(payload))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func validArticleBody() map[string]any {
	return map[string]any{
		"id": "test-1", "headline": "Test Headline", "body": "Test body.",
		"kind": "market_movers",
	}
}

func TestServePostCommentary_NoKeysConfigured_FailsClosed(t *testing.T) {
	h := mkHandlerForIngest(t, nil)
	w := postCommentaryReq(t, h, "Bearer anything", validArticleBody())
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (fail closed when no keys configured)", w.Code)
	}
}

func TestServePostCommentary_NoAuthHeader_Rejected(t *testing.T) {
	h := mkHandlerForIngest(t, []string{"real-key"})
	w := postCommentaryReq(t, h, "", validArticleBody())
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with no Authorization header", w.Code)
	}
}

func TestServePostCommentary_WrongKey_Rejected(t *testing.T) {
	h := mkHandlerForIngest(t, []string{"real-key"})
	w := postCommentaryReq(t, h, "Bearer wrong-key", validArticleBody())
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with a wrong key", w.Code)
	}
}

func TestServePostCommentary_CorrectKey_Accepted(t *testing.T) {
	h := mkHandlerForIngest(t, []string{"real-key"})
	w := postCommentaryReq(t, h, "Bearer real-key", validArticleBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", w.Code, w.Body.String())
	}
}

func TestServePostCommentary_MultipleKeys_EitherWorks(t *testing.T) {
	h := mkHandlerForIngest(t, []string{"key-a", "key-b"})
	w := postCommentaryReq(t, h, "Bearer key-b", validArticleBody())
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201 for the second configured key", w.Code)
	}
}

func TestServeCommentary_BodyHTML_RendersAsTrustedHTML(t *testing.T) {
	h := mkHandlerForIngest(t, []string{"real-key"})
	body := map[string]any{
		"id": "movers-html-test", "headline": "Stocks on the Move", "kind": "market_movers",
		"body":      "Apple Inc. (NASDAQ:AAPL)",
		"body_html": `<ul><li>Apple Inc. (NASDAQ:<a href="https://news.okemily.com/ticker/AAPL">AAPL</a>)</li></ul>`,
	}
	w := postCommentaryReq(t, h, "Bearer real-key", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("publish failed: status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/commentary/movers-html-test", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req)
	if w2.Code != http.StatusOK {
		t.Fatalf("get commentary: status=%d", w2.Code)
	}
	got := w2.Body.String()
	if !strings.Contains(got, `<a href="https://news.okemily.com/ticker/AAPL">AAPL</a>`) {
		t.Errorf("expected the real <a> link to render unescaped, got:\n%s", got)
	}
}

func TestServeCommentary_NoBodyHTML_FallsBackToEscapedPlainText(t *testing.T) {
	h := mkHandlerForIngest(t, []string{"real-key"})
	body := map[string]any{
		"id": "plain-test", "headline": "Plain Article", "kind": "governance_alert",
		"body": "Apple Inc. (NASDAQ:AAPL) — no HTML here, and if there were <b>it</b> should be escaped.",
	}
	w := postCommentaryReq(t, h, "Bearer real-key", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("publish failed: status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/commentary/plain-test", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req)
	got := w2.Body.String()
	if strings.Contains(got, "<b>it</b>") {
		t.Error("plain-text body content must stay escaped when body_html isn't set")
	}
	if !strings.Contains(got, "&lt;b&gt;it&lt;/b&gt;") {
		t.Errorf("expected the literal text HTML-escaped, got:\n%s", got)
	}
}
