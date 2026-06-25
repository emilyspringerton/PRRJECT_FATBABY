package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MailgunConfig holds credentials for the Mailgun API.
type MailgunConfig struct {
	APIKey   string // MAILGUN_API_KEY
	Domain   string // MAILGUN_DOMAIN — e.g. mg.fatbaby.io
	From     string // sender address — e.g. Emily Prime <emily@mg.fatbaby.io>
	BaseURL  string // default: https://api.mailgun.net/v3; EU: https://api.eu.mailgun.net/v3
	European bool   // when true overrides BaseURL to EU endpoint
}

// MailgunMailer sends mail via the Mailgun HTTP API using form-encoded POST.
// No third-party SDK dependency — uses only stdlib net/http.
type MailgunMailer struct {
	cfg    MailgunConfig
	client *http.Client
}

// NewMailgunMailer returns a MailgunMailer. The http.Client is created
// with a 15-second timeout; pass your own via the unexported field if needed.
func NewMailgunMailer(cfg MailgunConfig) *MailgunMailer {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.mailgun.net/v3"
	}
	if cfg.European {
		base = "https://api.eu.mailgun.net/v3"
	}
	cfg.BaseURL = base
	return &MailgunMailer{
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Send delivers an HTML email via Mailgun's /messages endpoint.
func (m *MailgunMailer) Send(ctx context.Context, to, subject, htmlBody string) error {
	endpoint := fmt.Sprintf("%s/%s/messages", m.cfg.BaseURL, m.cfg.Domain)

	form := url.Values{}
	form.Set("from", m.cfg.From)
	form.Set("to", to)
	form.Set("subject", subject)
	form.Set("html", htmlBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("mailgun: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("api", m.cfg.APIKey)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("mailgun: http: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mailgun: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
