package skuldmarkid

import (
	"testing"

	"skuldmark"

	"github.com/example/prrject-fatbaby/internal/identity"
)

func TestFromSecurityRef_RealAAPL(t *testing.T) {
	ref := identity.SecurityRef{Symbol: "AAPL", CIK: "320193"}
	got, err := FromSecurityRef(ref, "Nasdaq")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SYMBOL padded to 7, CIK padded to 10 (v1 layout, skuldmark.go's own doc comment --
	// README.md still showed the superseded v0 example, fixed alongside this).
	want := "EINXNASAAPLXXX0000320193Y"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if err := skuldmark.Validate(got); err != nil {
		t.Errorf("Validate(%q) failed: %v", got, err)
	}
}

func TestFromSecurityRef_UnknownExchange(t *testing.T) {
	ref := identity.SecurityRef{Symbol: "XOM", CIK: "34088"}
	_, err := FromSecurityRef(ref, "")
	if err == nil {
		t.Fatal("expected an error for unresolved exchange, got nil")
	}
}

func TestFromSecurityRef_EmptyCIK(t *testing.T) {
	ref := identity.SecurityRef{Symbol: "AAPL", CIK: ""}
	_, err := FromSecurityRef(ref, "Nasdaq")
	if err == nil {
		t.Fatal("expected an error for empty CIK, got nil")
	}
}

func TestFromSecurityRef_NonNumericCIK(t *testing.T) {
	ref := identity.SecurityRef{Symbol: "AAPL", CIK: "not-a-number"}
	_, err := FromSecurityRef(ref, "Nasdaq")
	if err == nil {
		t.Fatal("expected an error for non-numeric CIK, got nil")
	}
}

func TestExchangeToMIC_KnownAndUnknown(t *testing.T) {
	cases := []struct {
		exchange string
		wantMIC  string
		wantOK   bool
	}{
		{"Nasdaq", "XNAS", true},   // SEC's own Title Case
		{"NASDAQ", "XNAS", true},  // prwatch's regex-extracted upper case
		{"NYSE", "XNYS", true},
		{"OTC", "OTCM", true},
		{"NYSE American", "XASE", true},
		{"", "", false},
		{"CBOE", "", false}, // deliberately ambiguous, not guessed
	}
	for _, c := range cases {
		mic, ok := ExchangeToMIC(c.exchange)
		if mic != c.wantMIC || ok != c.wantOK {
			t.Errorf("ExchangeToMIC(%q) = (%q, %v), want (%q, %v)", c.exchange, mic, ok, c.wantMIC, c.wantOK)
		}
	}
}
