// earnings-alert sends a weekly HTML email listing upcoming earnings for
// watchlisted tickers in the next N days.
//
// Run once (e.g. Monday morning from cron/systemd timer):
//
//	earnings-alert -to emily@example.com -days 7
//
// Environment variables:
//
//	MAILGUN_API_KEY  + MAILGUN_DOMAIN  — use Mailgun backend
//	SMTP_HOST + SMTP_USER + SMTP_PASS  — use SMTP backend
//	ALERT_TO                           — default recipient (overridden by -to)
//	ALERT_DAYS                         — default look-ahead window in days
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/internal/earningscal"
	"github.com/example/prrject-fatbaby/internal/notify"
)

func main() {
	calDir := flag.String("cal-dir", filepath.Join("var", "earnings-calendar"), "earnings calendar directory")
	to     := flag.String("to", envOr("ALERT_TO", "emilyspringerton@gmail.com"), "recipient email")
	days   := flag.Int("days", intEnvOr("ALERT_DAYS", 7), "look-ahead window in days")
	dryRun := flag.Bool("dry-run", false, "print email body to stdout; do not send")
	flag.Parse()

	logger := log.New(os.Stdout, "earnings-alert ", log.LstdFlags|log.LUTC)

	store := earningscal.NewStore(*calDir)
	if err := store.Refresh(); err != nil {
		logger.Fatalf("load calendar: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	cutoff := time.Now().UTC().AddDate(0, 0, *days).Format("2006-01-02")
	upcoming := store.Query(nil, today, cutoff, nil)

	if len(upcoming) == 0 {
		logger.Printf("no upcoming earnings in next %d days — no alert sent", *days)
		return
	}

	subject := fmt.Sprintf("Earnings Calendar: %d reports in the next %d days (%s)", len(upcoming), *days, today)
	body := buildHTML(upcoming, today, *days)

	if *dryRun {
		fmt.Println("Subject:", subject)
		fmt.Println(body)
		return
	}

	mailer := notify.NewFromEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := mailer.Send(ctx, *to, subject, body); err != nil {
		logger.Fatalf("send: %v", err)
	}
	logger.Printf("sent to=%s reports=%d", *to, len(upcoming))
}

func buildHTML(items []*earningscal.EarningsDate, today string, days int) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><body style="font-family:monospace;background:#0a0a0f;color:#e0e0e0;padding:24px">`)
	sb.WriteString(fmt.Sprintf(`<h2 style="color:#f5c518">Earnings Calendar — Next %d Days</h2>`, days))
	sb.WriteString(fmt.Sprintf(`<p style="color:#888">Generated %s UTC</p>`, today))
	sb.WriteString(`<table style="border-collapse:collapse;width:100%">`)
	sb.WriteString(`<thead><tr style="background:#1a1a2e;color:#f5c518">`)
	sb.WriteString(`<th style="padding:8px;text-align:left">Date</th>`)
	sb.WriteString(`<th style="padding:8px;text-align:left">Ticker</th>`)
	sb.WriteString(`<th style="padding:8px;text-align:left">Period</th>`)
	sb.WriteString(`<th style="padding:8px;text-align:left">Time</th>`)
	sb.WriteString(`<th style="padding:8px;text-align:left">Status</th>`)
	sb.WriteString(`</tr></thead><tbody>`)

	for i, d := range items {
		bg := "#111118"
		if i%2 == 0 {
			bg = "#0d0d14"
		}
		timeStr := "unknown"
		if d.BeforeMarket != nil {
			if *d.BeforeMarket {
				timeStr = "BMO"
			} else {
				timeStr = "AMC"
			}
		}
		statusColor := "#888"
		switch d.Status {
		case earningscal.StatusConfirmed:
			statusColor = "#4caf50"
		case earningscal.StatusAnnounced:
			statusColor = "#f5c518"
		}
		sb.WriteString(fmt.Sprintf(`<tr style="background:%s">`, bg))
		sb.WriteString(fmt.Sprintf(`<td style="padding:8px">%s</td>`, d.ReportDate))
		sb.WriteString(fmt.Sprintf(`<td style="padding:8px;font-weight:bold;color:#f5c518">%s</td>`, escapeHTML(d.Ticker)))
		sb.WriteString(fmt.Sprintf(`<td style="padding:8px">%s %d</td>`, d.FiscalQuarter, d.FiscalYear))
		sb.WriteString(fmt.Sprintf(`<td style="padding:8px">%s</td>`, timeStr))
		sb.WriteString(fmt.Sprintf(`<td style="padding:8px;color:%s">%s</td>`, statusColor, d.Status))
		sb.WriteString(`</tr>`)
	}

	sb.WriteString(`</tbody></table>`)
	sb.WriteString(`<p style="color:#555;font-size:12px;margin-top:24px">`)
	sb.WriteString(`Status: <span style="color:#4caf50">confirmed</span> = actual 8-K filing date &nbsp;|&nbsp; `)
	sb.WriteString(`<span style="color:#f5c518">announced</span> = press release announcement`)
	sb.WriteString(`</p>`)
	sb.WriteString(`<p style="color:#333;font-size:11px">Emily Prime · FatBaby Signal Intelligence</p>`)
	sb.WriteString(`</body></html>`)
	return sb.String()
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnvOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
