package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

const anthropicMessagesURL = "https://api.anthropic.com/v1/messages"
const haikuModel = "claude-haiku-4-5-20251001"

// HaikuProvider calls claude-haiku via the Anthropic Messages API and parses
// the JSON signal out of the assistant response.
type HaikuProvider struct {
	APIKey     string
	HTTPClient *http.Client
}

func NewHaikuProvider(apiKey string) *HaikuProvider {
	return &HaikuProvider{
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (h *HaikuProvider) AnalyzeText(ctx context.Context, text string) (*intelligence.Signal, error) {
	prompt := fmt.Sprintf(PromptTemplate, "the filing/press release below") + "\n\nDo not include any text outside the JSON object.\n\n" + text
	reqBody, err := json.Marshal(anthropicRequest{
		Model:     haikuModel,
		MaxTokens: 512,
		System:    "You are a senior hedge fund analyst. Always respond with a single valid JSON object and nothing else.",
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("haiku provider marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("haiku provider request: %w", err)
	}
	req.Header.Set("x-api-key", h.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("haiku provider http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("haiku provider read: %w", err)
	}

	var ar anthropicResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("haiku provider decode: %w", err)
	}
	if ar.Error != nil {
		return nil, fmt.Errorf("haiku provider api error: %s", ar.Error.Message)
	}
	if len(ar.Content) == 0 || ar.Content[0].Type != "text" {
		return nil, fmt.Errorf("haiku provider: empty or non-text response")
	}

	raw := strings.TrimSpace(ar.Content[0].Text)
	// Strip markdown code fence if present.
	if strings.HasPrefix(raw, "```") {
		raw = raw[strings.Index(raw, "\n")+1:]
		if idx := strings.LastIndex(raw, "```"); idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}

	var sig intelligence.Signal
	if err := json.Unmarshal([]byte(raw), &sig); err != nil {
		return nil, fmt.Errorf("haiku provider parse signal JSON: %w — raw=%q", err, truncate(raw, 200))
	}
	return &sig, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
