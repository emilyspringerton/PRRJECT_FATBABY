package newssite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SetEmilyBaseURL configures the Emily Prime base URL for /api/ask proxying.
// Example: "http://localhost:8086". Empty string disables the endpoint.
func (h *Handler) SetEmilyBaseURL(url string) {
	h.emilyBaseURL = strings.TrimRight(url, "/")
}

// serveAsk handles POST /api/ask.
//
//	Request:  { "question": string, "ticker"?: string, "session_id"?: string }
//	Response: { "answer": string, "ticker"?: string }
//
// Proxies to Emily Prime /chat. Returns 503 if Emily is not configured.
func (h *Handler) serveAsk(w http.ResponseWriter, r *http.Request) int {
	if h.emilyBaseURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Ask Emily not configured"})
		return http.StatusServiceUnavailable
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return http.StatusMethodNotAllowed
	}

	var req struct {
		Question  string `json:"question"`
		Ticker    string `json:"ticker"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return http.StatusBadRequest
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "question required"})
		return http.StatusBadRequest
	}

	// Build the message. Prepend ticker context if provided.
	message := req.Question
	if req.Ticker != "" {
		message = fmt.Sprintf("[Ticker: %s] %s", strings.ToUpper(req.Ticker), req.Question)
	}

	// Generate a stable session_id if not provided.
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = "askeily-" + remoteIP(r)
	}

	// Forward to Emily Prime /chat.
	chatPayload, _ := json.Marshal(map[string]string{
		"message":    message,
		"session_id": sessionID,
	})

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := http.NewRequestWithContext(ctx, http.MethodPost, h.emilyBaseURL+"/chat", bytes.NewReader(chatPayload))
	if err != nil {
		h.logger.Printf("askeily: build request: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "request build failed"})
		return http.StatusInternalServerError
	}
	resp.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	emilyResp, err := httpClient.Do(resp)
	if err != nil {
		h.logger.Printf("askeily: emily call: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Emily Prime unavailable"})
		return http.StatusBadGateway
	}
	defer emilyResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(emilyResp.Body, 64*1024))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "read response failed"})
		return http.StatusBadGateway
	}

	if emilyResp.StatusCode != http.StatusOK {
		h.logger.Printf("askeily: emily returned %d: %s", emilyResp.StatusCode, string(body))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Emily Prime returned error"})
		return http.StatusBadGateway
	}

	// Emily /chat returns { reply: string, ... }
	var emilyBody struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(body, &emilyBody); err != nil {
		// If we can't parse it, return raw.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body) //nolint:errcheck
		return http.StatusOK
	}

	result := map[string]string{"answer": emilyBody.Reply}
	if req.Ticker != "" {
		result["ticker"] = strings.ToUpper(req.Ticker)
	}
	writeJSON(w, http.StatusOK, result)
	return http.StatusOK
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
