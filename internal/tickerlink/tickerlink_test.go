package tickerlink

import (
	"strings"
	"testing"
)

func TestFormatRef_BasicFormat(t *testing.T) {
	got := string(FormatRef("https://news.okemily.com", "Ford Motor Company", "NYSE", "f"))
	want := `Ford Motor Company (NYSE:<a href="https://news.okemily.com/ticker/F">F</a>)`
	if got != want {
		t.Errorf("FormatRef =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatRef_AbsoluteURL_NotRelative(t *testing.T) {
	got := string(FormatRef("https://news.okemily.com", "Apple Inc.", "NASDAQ", "AAPL"))
	if !strings.Contains(got, `href="https://news.okemily.com/ticker/AAPL"`) {
		t.Errorf("expected an absolute href so distribution/syndication still resolves it, got: %s", got)
	}
	if strings.Contains(got, `href="/ticker/`) {
		t.Error("must never emit a bare relative href")
	}
}

func TestFormatRef_TrailingSlashOnBaseURLHandled(t *testing.T) {
	got := string(FormatRef("https://news.okemily.com/", "Apple Inc.", "NASDAQ", "AAPL"))
	if strings.Contains(got, "com//ticker") {
		t.Errorf("double slash from trailing baseURL slash: %s", got)
	}
}

func TestFormatRef_EmptyExchange_NoLeadingColon(t *testing.T) {
	got := string(FormatRef("https://news.okemily.com", "Lucid Group, Inc.", "", "LCID"))
	if !strings.Contains(got, "(<a") {
		t.Errorf("expected no exchange prefix when exchange is empty, got: %s", got)
	}
	if strings.Contains(got, "(:") {
		t.Error("must not emit a bare leading colon when exchange is empty")
	}
}

func TestFormatRef_EscapesHTMLInCompanyName(t *testing.T) {
	got := string(FormatRef("https://news.okemily.com", `<script>alert(1)</script> Corp`, "NYSE", "XSS"))
	if strings.Contains(got, "<script>") {
		t.Errorf("company name must be HTML-escaped, got: %s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag, got: %s", got)
	}
}

func TestFormatRef_URLEscapesTickerInHref(t *testing.T) {
	// Tickers with class shares sometimes carry a dot/dash (e.g. BRK.A); the
	// href must stay a well-formed URL even for unusual symbols.
	got := string(FormatRef("https://news.okemily.com", "Berkshire Hathaway", "NYSE", "BRK.A"))
	if !strings.Contains(got, `href="https://news.okemily.com/ticker/BRK.A"`) {
		t.Errorf("expected BRK.A preserved in href, got: %s", got)
	}
}

func TestFormatRef_UppercasesTicker(t *testing.T) {
	got := string(FormatRef("https://news.okemily.com", "Apple Inc.", "NASDAQ", "aapl"))
	if !strings.Contains(got, ">AAPL</a>") {
		t.Errorf("expected ticker uppercased in link text, got: %s", got)
	}
}

func TestPlainRef_NoMarkup(t *testing.T) {
	got := PlainRef("Ford Motor Company", "NYSE", "F")
	want := "Ford Motor Company (NYSE:F)"
	if got != want {
		t.Errorf("PlainRef = %q, want %q", got, want)
	}
	if strings.Contains(got, "<a") {
		t.Error("PlainRef must never emit HTML")
	}
}

func TestPlainRef_EmptyExchange(t *testing.T) {
	got := PlainRef("Lucid Group, Inc.", "", "LCID")
	if got != "Lucid Group, Inc. (LCID)" {
		t.Errorf("PlainRef with empty exchange = %q", got)
	}
}
