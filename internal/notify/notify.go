// Package notify provides a Mailer abstraction for sending email notifications.
//
// Implementations:
//   - MailgunMailer — Mailgun HTTP API (MAILGUN_API_KEY + MAILGUN_DOMAIN)
//   - SMTPMailer    — standard SMTP (SMTP_HOST / SMTP_USER / SMTP_PASS)
//   - NullMailer    — discards all messages (for tests / dry-run mode)
//
// Pick a backend via NewFromEnv:
//
//	mailer := notify.NewFromEnv()
//	err := mailer.Send(ctx, "emily@example.com", "Subject", "<p>body</p>")
package notify

import (
	"context"
	"fmt"
	"os"
)

// Mailer is the interface implemented by all email backends.
type Mailer interface {
	// Send sends an HTML email to the given address.
	// to may be a single address or comma-separated list.
	Send(ctx context.Context, to, subject, htmlBody string) error
}

// NewFromEnv picks a Mailer backend based on environment variables.
//
// Priority:
//  1. MAILGUN_API_KEY + MAILGUN_DOMAIN → MailgunMailer
//  2. SMTP_HOST → SMTPMailer
//  3. Fallback → NullMailer (logs to stderr)
func NewFromEnv() Mailer {
	if os.Getenv("MAILGUN_API_KEY") != "" && os.Getenv("MAILGUN_DOMAIN") != "" {
		return NewMailgunMailer(MailgunConfig{
			APIKey:   os.Getenv("MAILGUN_API_KEY"),
			Domain:   os.Getenv("MAILGUN_DOMAIN"),
			From:     envOr("MAILGUN_FROM", "Emily Prime <emily@"+os.Getenv("MAILGUN_DOMAIN")+">"),
			BaseURL:  envOr("MAILGUN_BASE_URL", "https://api.mailgun.net/v3"),
			European: os.Getenv("MAILGUN_EU") == "1",
		})
	}
	if os.Getenv("SMTP_HOST") != "" {
		return NewSMTPMailer(SMTPConfig{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     envOr("SMTP_PORT", "587"),
			Username: os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASS"),
			From:     envOr("SMTP_FROM", os.Getenv("SMTP_USER")),
		})
	}
	return NullMailer{}
}

// NullMailer discards every message and logs to stderr.
type NullMailer struct{}

func (NullMailer) Send(_ context.Context, to, subject, _ string) error {
	fmt.Fprintf(os.Stderr, "[notify/null] would send to=%s subject=%q\n", to, subject)
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
