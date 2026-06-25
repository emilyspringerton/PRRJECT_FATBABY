package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// SMTPConfig holds connection details for a standard SMTP relay.
type SMTPConfig struct {
	Host     string // e.g. smtp.mailgun.org
	Port     string // default 587 (STARTTLS)
	Username string
	Password string
	From     string // envelope + header From
}

// SMTPMailer sends mail via SMTP with STARTTLS.
// Compatible with Mailgun's SMTP relay, SendGrid, Postmark, or any SMTP provider.
type SMTPMailer struct {
	cfg SMTPConfig
}

// NewSMTPMailer returns an SMTPMailer.
func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer {
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	return &SMTPMailer{cfg: cfg}
}

// Send connects, authenticates, and delivers an HTML message via STARTTLS.
func (s *SMTPMailer) Send(ctx context.Context, to, subject, htmlBody string) error {
	addr := net.JoinHostPort(s.cfg.Host, s.cfg.Port)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: connect: %w", err)
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp: new client: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: s.cfg.Host}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}

	if err := c.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	for _, addr := range splitAddresses(to) {
		if err := c.Rcpt(strings.TrimSpace(addr)); err != nil {
			return fmt.Errorf("smtp: RCPT TO %s: %w", addr, err)
		}
	}

	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	msg := buildMIME(s.cfg.From, to, subject, htmlBody)
	if _, err := fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("smtp: write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp: close data: %w", err)
	}
	return c.Quit()
}

// buildMIME assembles a minimal MIME message with HTML body.
func buildMIME(from, to, subject, htmlBody string) string {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(htmlBody)
	return sb.String()
}

func splitAddresses(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
