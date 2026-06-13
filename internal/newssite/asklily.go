package newssite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SetEmilyBaseURL configures the Emily Prime base URL for /api/ask proxying.
// Example: "http://localhost:8086". Empty string disables the endpoint.
func (h *Handler) SetEmilyBaseURL(url string) {
	h.emilyBaseURL = strings.TrimRight(url, "/")
}

// SetSignalapiURL configures the signalapi base URL for ticker context injection (S21-03).
// Example: "http://localhost:9091". Empty string disables context enrichment.
func (h *Handler) SetSignalapiURL(url string) {
	h.signalapiURL = strings.TrimRight(url, "/")
}

// SetGoogleClientID sets the Google OAuth client ID for Sign in with Google (S21-05).
func (h *Handler) SetGoogleClientID(id string) {
	h.googleClientID = id
}

// SetIdunaBaseURL sets the IDUNA base URL used to proxy Google ID token → IDUNA JWT (S21-05).
func (h *Handler) SetIdunaBaseURL(url string) {
	h.idunaBaseURL = strings.TrimRight(url, "/")
}

// SetAskJWTVerifier configures the JWT verifier used to identify authenticated Ask Emily users.
func (h *Handler) SetAskJWTVerifier(v askVerifier) {
	h.askJWTVerifier = v
}

// serveAuthGoogle handles POST /api/auth/google.
// Proxies the Google ID token to IDUNA /api/v1/auth/google and returns the IDUNA JWT.
// This avoids CORS issues: the browser sends the token to the same origin.
func (h *Handler) serveAuthGoogle(w http.ResponseWriter, r *http.Request) int {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return http.StatusMethodNotAllowed
	}
	if h.idunaBaseURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth not configured"})
		return http.StatusServiceUnavailable
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
		return http.StatusBadRequest
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.idunaBaseURL+"/api/v1/auth/google", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "proxy request failed"})
		return http.StatusInternalServerError
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "IDUNA unavailable"})
		return http.StatusBadGateway
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
	return resp.StatusCode
}

// serveAsk handles POST /api/ask.
//
//	Request:  { "question": string, "ticker"?: string, "session_id"?: string }
//	Response: { "answer": string, "ticker"?: string }
//
// Proxies to Emily Prime /chat. Returns 503 if Emily is not configured.
// Authenticated users (Bearer JWT) get 20 questions/day; anonymous gets 5/day.
func (h *Handler) serveAsk(w http.ResponseWriter, r *http.Request) int {
	if h.emilyBaseURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Ask Emily not configured"})
		return http.StatusServiceUnavailable
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return http.StatusMethodNotAllowed
	}

	// Rate limit: authenticated users get 20/day by user_id; anonymous get 5/day by IP.
	userSub := h.extractUserSub(r)
	if userSub != "" {
		if h.authedAskRateLimiter != nil {
			if ok, _ := h.authedAskRateLimiter.check(userSub); !ok {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{
					"error": "Daily limit reached (20 questions/day). Upgrade to Emily+ for unlimited access.",
				})
				return http.StatusTooManyRequests
			}
		}
	} else if h.askRateLimiter != nil {
		ip := remoteIP(r)
		if ok, _ := h.askRateLimiter.check(ip); !ok {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "Daily limit reached (5 questions/day). Sign in for 20 questions/day.",
			})
			return http.StatusTooManyRequests
		}
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

	// Build the message. Inject FatBaby signal context if ticker + signalapi configured.
	message := req.Question
	if req.Ticker != "" {
		tickerCtx := h.fetchTickerContext(r.Context(), strings.ToUpper(req.Ticker))
		if tickerCtx != "" {
			message = fmt.Sprintf("[FatBaby context for %s]\n%s\n\n[Question] %s",
				strings.ToUpper(req.Ticker), tickerCtx, req.Question)
		} else {
			message = fmt.Sprintf("[Ticker: %s] %s", strings.ToUpper(req.Ticker), req.Question)
		}
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

// fetchTickerContext fetches governance signals + entity document from signalapi
// and formats them as a compact text block for Emily Prime's context.
// Returns empty string if signalapiURL is not configured or calls fail.
func (h *Handler) fetchTickerContext(ctx context.Context, ticker string) string {
	if h.signalapiURL == "" {
		return ""
	}
	client := &http.Client{Timeout: 5 * time.Second}
	var sb strings.Builder

	// Fetch last 5 governance signals for the ticker.
	sigURL := fmt.Sprintf("%s/v1/governance-signals?ticker=%s&limit=5", h.signalapiURL, ticker)
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, sigURL, nil); err == nil {
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			var signals []struct {
				EventType  string  `json:"event_type"`
				Headline   string  `json:"headline"`
				FilingDate string  `json:"filing_date"`
				SignalScore float64 `json:"signal_score"`
			}
			if json.NewDecoder(resp.Body).Decode(&signals) == nil && len(signals) > 0 {
				sb.WriteString("Recent governance signals:\n")
				for _, s := range signals {
					sb.WriteString(fmt.Sprintf("- %s [%s] score=%.2f: %s\n",
						s.EventType, s.FilingDate, s.SignalScore, truncate512(s.Headline)))
				}
			}
		}
	}

	// Fetch entity document from MongoDB for director/auditor info.
	entURL := fmt.Sprintf("%s/v1/entities/%s", h.signalapiURL, ticker)
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, entURL, nil); err == nil {
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var entity struct {
					Auditor struct {
						Name string `json:"name"`
					} `json:"auditor"`
					Directors []struct {
						Name       string   `json:"name"`
						Role       string   `json:"role"`
						LastFiling string   `json:"last_filing"`
						Flags      []string `json:"flags"`
					} `json:"directors"`
					SignalScore float64 `json:"signal_score"`
				}
				if json.NewDecoder(resp.Body).Decode(&entity) == nil {
					if entity.Auditor.Name != "" {
						sb.WriteString(fmt.Sprintf("Auditor: %s\n", entity.Auditor.Name))
					}
					if len(entity.Directors) > 0 {
						sb.WriteString("Current directors:\n")
						for i, d := range entity.Directors {
							if i >= 5 {
								break
							}
							flags := ""
							if len(d.Flags) > 0 {
								flags = " [" + strings.Join(d.Flags, ",") + "]"
							}
							sb.WriteString(fmt.Sprintf("- %s (%s)%s\n", d.Name, d.Role, flags))
						}
					}
					if entity.SignalScore > 0 {
						sb.WriteString(fmt.Sprintf("Overall signal score: %.2f\n", entity.SignalScore))
					}
				}
			}
		}
	}

	return sb.String()
}

func truncate512(s string) string {
	if len(s) <= 512 {
		return s
	}
	return s[:512]
}

// extractUserSub returns the IDUNA JWT sub claim from the Bearer token in the
// Authorization header, or empty string if missing/invalid.
func (h *Handler) extractUserSub(r *http.Request) string {
	if h.askJWTVerifier == nil {
		return ""
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	sub, err := h.askJWTVerifier.VerifyTokenSub(token)
	if err != nil {
		return ""
	}
	return sub
}

// serveAskLanding renders GET /ask — the Ask Emily product landing page.
func (h *Handler) serveAskLanding(w http.ResponseWriter, r *http.Request) int {
	var buf bytes.Buffer
	if err := RenderAskLanding(&buf, AskLandingData{GoogleClientID: h.googleClientID}); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
	return http.StatusOK
}

// serveWaitlist handles POST /api/waitlist — captures email into var/waitlist.txt.
func (h *Handler) serveWaitlist(w http.ResponseWriter, r *http.Request) int {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return http.StatusBadRequest
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid email required"})
		return http.StatusBadRequest
	}

	// Best-effort append to var/waitlist.txt; log on failure but don't 500.
	if err := appendWaitlist(email); err != nil {
		h.logger.Printf("waitlist append err: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "You're on the list!"})
	return http.StatusOK
}

func appendWaitlist(email string) error {
	if err := os.MkdirAll("var", 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join("var", "waitlist.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\t%s\n", time.Now().UTC().Format(time.RFC3339), email)
	return err
}
