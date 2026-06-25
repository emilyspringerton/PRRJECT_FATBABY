package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ── NullMailer ────────────────────────────────────────────────────────────────

func TestNullMailerSend(t *testing.T) {
	m := NullMailer{}
	if err := m.Send(context.Background(), "a@b.com", "hi", "<p>hi</p>"); err != nil {
		t.Fatalf("NullMailer.Send: %v", err)
	}
}

// ── MailgunMailer ─────────────────────────────────────────────────────────────

func TestMailgunMailerSend(t *testing.T) {
	var got url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header (Basic api:<key>).
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("missing Basic auth, got %q", auth)
		}
		decoded, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if !strings.HasPrefix(string(decoded), "api:") {
			t.Errorf("auth prefix not api:, got %q", decoded)
		}
		body, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(body))
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "testid", "message": "queued"})
	}))
	defer ts.Close()

	m := NewMailgunMailer(MailgunConfig{
		APIKey:  "key-test",
		Domain:  "mg.example.com",
		From:    "Emily <emily@mg.example.com>",
		BaseURL: ts.URL,
	})

	if err := m.Send(context.Background(), "user@example.com", "Q2 Earnings Alert", "<p>Apple reports Thursday</p>"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.Get("to") != "user@example.com" {
		t.Errorf("to=%q want user@example.com", got.Get("to"))
	}
	if got.Get("subject") != "Q2 Earnings Alert" {
		t.Errorf("subject=%q", got.Get("subject"))
	}
	if !strings.Contains(got.Get("html"), "Apple reports Thursday") {
		t.Errorf("html body missing expected content: %q", got.Get("html"))
	}
}

func TestMailgunMailerErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"Forbidden"}`)
	}))
	defer ts.Close()

	m := NewMailgunMailer(MailgunConfig{
		APIKey:  "bad-key",
		Domain:  "mg.example.com",
		From:    "x@mg.example.com",
		BaseURL: ts.URL,
	})
	err := m.Send(context.Background(), "a@b.com", "sub", "<p>body</p>")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401, got: %v", err)
	}
}

func TestMailgunEuropeanEndpoint(t *testing.T) {
	m := NewMailgunMailer(MailgunConfig{
		APIKey:   "key",
		Domain:   "mg.example.com",
		From:     "x@mg.example.com",
		European: true,
	})
	if !strings.Contains(m.cfg.BaseURL, "api.eu.mailgun.net") {
		t.Errorf("EU endpoint not set: %q", m.cfg.BaseURL)
	}
}

// ── NewFromEnv ────────────────────────────────────────────────────────────────

func TestNewFromEnvNullFallback(t *testing.T) {
	// With no env vars set, must return NullMailer.
	t.Setenv("MAILGUN_API_KEY", "")
	t.Setenv("MAILGUN_DOMAIN", "")
	t.Setenv("SMTP_HOST", "")
	m := NewFromEnv()
	if _, ok := m.(NullMailer); !ok {
		t.Errorf("expected NullMailer, got %T", m)
	}
}

func TestNewFromEnvMailgun(t *testing.T) {
	t.Setenv("MAILGUN_API_KEY", "key-123")
	t.Setenv("MAILGUN_DOMAIN", "mg.example.com")
	t.Setenv("SMTP_HOST", "")
	m := NewFromEnv()
	if _, ok := m.(*MailgunMailer); !ok {
		t.Errorf("expected MailgunMailer, got %T", m)
	}
}

func TestNewFromEnvSMTP(t *testing.T) {
	t.Setenv("MAILGUN_API_KEY", "")
	t.Setenv("MAILGUN_DOMAIN", "")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	m := NewFromEnv()
	if _, ok := m.(*SMTPMailer); !ok {
		t.Errorf("expected SMTPMailer, got %T", m)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func TestSplitAddresses(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a@b.com", []string{"a@b.com"}},
		{"a@b.com, c@d.com", []string{"a@b.com", "c@d.com"}},
		{"  x@y.com  ,  z@w.com  ", []string{"x@y.com", "z@w.com"}},
	}
	for _, tc := range cases {
		got := splitAddresses(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitAddresses(%q): got %v want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitAddresses(%q)[%d]: got %q want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
