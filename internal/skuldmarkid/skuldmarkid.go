// Package skuldmarkid adapts PRRJECT_FATBABY's own identity.SecurityRef
// into a real SKULDMARK-25 instrument identifier (see the SKULDMARK repo,
// public domain, github.com/emilyspringerton/SKULDMARK). SKULDMARK.Encode
// itself deliberately has no dependency on this pipeline's types -- this is
// the caller-side adapter its own README says is the natural next step.
//
// Minted as early in the intake pipeline as possible (founder, real-time,
// 2026-08-13/14): secwatch and prwatch discovery are the earliest points a
// SecurityRef exists, so that's where this gets called, not further
// downstream in processor/signalapi.
package skuldmarkid

import (
	"fmt"
	"strconv"
	"strings"

	"skuldmark"

	"github.com/example/prrject-fatbaby/internal/identity"
)

// exchangeToMIC maps exchange-name strings to their real ISO 10383 Market
// Identifier Codes. Keys are upper-cased for case-insensitive lookup (SEC's
// company_tickers_exchange.json uses Title Case, "Nasdaq"; internal/prwatch's
// own regex-extracted exchange names are upper-case, "NASDAQ" -- both sources
// feed this same table). Deliberately small and explicit -- an unrecognized
// exchange name returns ok=false rather than guessing at a MIC. "CBOE" is
// left out on purpose: the bare regex match doesn't disambiguate which real
// Cboe market (options vs. BZX) it refers to, and guessing would produce a
// wrong-but-plausible-looking ID, which is worse than no ID.
var exchangeToMIC = map[string]string{
	"NASDAQ":        "XNAS",
	"NYSE":          "XNYS",
	"NYSE AMERICAN": "XASE",
	"AMEX":          "XASE", // AMEX merged into NYSE American; same MIC today
	"OTC":           "OTCM",
	"OTCQX":         "OTCM", // SEC's own exchange field doesn't split OTC tiers either
	"OTCQB":         "OTCM",
	"BATS":          "BATS", // Cboe BZX Exchange
	"TSX":           "XTSE",
	"LSE":           "XLON",
	"HKEX":          "XHKG",
}

// ExchangeToMIC resolves an exchange name (as used in config/watchlist.json's
// Exchange field, or internal/prwatch's regex-extracted exchange strings) to
// its ISO 10383 MIC. ok is false for an empty or unrecognized exchange name
// -- callers must not guess.
func ExchangeToMIC(exchange string) (mic string, ok bool) {
	mic, ok = exchangeToMIC[strings.ToUpper(strings.TrimSpace(exchange))]
	return mic, ok
}

// FromSecurityRef mints a SKULDMARK-25 ID from a SecurityRef plus the
// exchange name SecurityRef itself doesn't carry (identity.SecurityRef.Exchange
// exists but is populated by almost nothing in this codebase today -- callers
// pass the watchlist's own Exchange field instead, the one real source of
// this data verified live against SEC's company_tickers_exchange.json,
// 2026-08-14).
//
// Returns an error, not a guess, when CIK/Symbol/exchange can't all be
// resolved -- an unminted record is honest; a wrong ID is not.
func FromSecurityRef(ref identity.SecurityRef, exchange string) (string, error) {
	mic, ok := ExchangeToMIC(exchange)
	if !ok {
		return "", fmt.Errorf("skuldmarkid: no known MIC for exchange %q (symbol=%q cik=%q)", exchange, ref.Symbol, ref.CIK)
	}
	if ref.CIK == "" {
		return "", fmt.Errorf("skuldmarkid: empty CIK for symbol %q", ref.Symbol)
	}
	cik, err := strconv.ParseUint(strings.TrimSpace(ref.CIK), 10, 64)
	if err != nil {
		return "", fmt.Errorf("skuldmarkid: CIK %q is not numeric: %w", ref.CIK, err)
	}
	return skuldmark.Encode(skuldmark.Instrument{
		MIC:    mic,
		Symbol: ref.Symbol,
		CIK:    cik,
	})
}
