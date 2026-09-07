package gauntlet

import (
	"strings"
	"testing"
)

func TestAppendDisclaimer_ContainsDisclosureText(t *testing.T) {
	out := AppendDisclaimer("Some article body.")
	if !strings.Contains(out, "Some article body.") {
		t.Fatalf("expected the original body to survive, got %q", out)
	}
	if !strings.Contains(out, "not investment advice") {
		t.Fatalf("expected the disclaimer to be appended, got %q", out)
	}
}

func TestAppendDisclaimerHTML_ContainsDisclosureText(t *testing.T) {
	out := AppendDisclaimerHTML("<p>Some article body.</p>")
	if !strings.Contains(out, "<p>Some article body.</p>") {
		t.Fatalf("expected the original body HTML to survive, got %q", out)
	}
	if !strings.Contains(out, "not investment advice") {
		t.Fatalf("expected the disclaimer to be appended, got %q", out)
	}
}

func TestLinkIssuer_LinksToTickerPage(t *testing.T) {
	got := string(LinkIssuer("https://news.example.com", "Example Corp", "EX"))
	if !strings.Contains(got, `href="https://news.example.com/ticker/EX"`) {
		t.Fatalf("expected a real ticker-page link, got %q", got)
	}
	if !strings.Contains(got, "Example Corp") {
		t.Fatalf("expected the company name to be present, got %q", got)
	}
}

func TestPlainIssuer_NoExchangePrefix(t *testing.T) {
	got := PlainIssuer("Example Corp", "EX")
	if got != "Example Corp (EX)" {
		t.Fatalf("expected \"Example Corp (EX)\" with no exchange prefix, got %q", got)
	}
}
