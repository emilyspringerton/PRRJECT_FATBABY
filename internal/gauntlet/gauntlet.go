// Package gauntlet is the one, real, shared compliance pass every
// auto-generated FatBaby article goes through before publishing — founder
// real-time, 2026-09-07: "one use case of gauntlet is all auto generated
// articles need disclaimers; another is the tickerization via (NYSE:F) raw
// text or metadata whatever we can get at." It replaces the earlier
// NORTHSTAR-only "Gauntlet" concept (EMILY/docs/fable-prompts/
// gauntlet-press-release-publishing.md, a broader editorial/licensing
// pipeline that was never built) with a narrow, real v0: two concrete
// obligations, applied consistently.
//
// Ticker-linking itself is NOT reimplemented here — internal/tickerlink is
// already the one shared implementation (its own doc comment already names
// Gauntlet as the intended future caller, EMILY/BACKLOG.md SECTION 167).
// This package only adds the disclaimer half, plus a thin LinkIssuer
// wrapper for generators that only have a ticker/company name in hand, no
// exchange (eps/guidance today) — tickerlink.FormatRef already tolerates an
// empty exchange for exactly this case.
package gauntlet

import (
	"html/template"
	"strings"

	"github.com/example/prrject-fatbaby/internal/tickerlink"
)

// Disclaimer is the standard, plain-text disclosure appended to the body of
// every auto-generated article (movers, EPS, guidance today; any future
// Gauntlet-managed content type per S167-04). Deliberately factual, not
// legal boilerplate written by a lawyer -- matches this repo's own "named
// honestly, not oversold" convention (see TINA's own NORTHSTAR.md, which
// this disclaimer is intentionally consistent in tone with).
const Disclaimer = "This article was generated automatically from structured signal data as part of FatBaby's automated markets desk. It is a factual observation, not investment advice, and should not be relied upon as the sole basis for any investment decision. FatBaby holds no position in, and receives no compensation related to, the securities named above."

// DisclaimerHTML is Disclaimer wrapped for an HTML body -- a horizontal
// rule followed by an italicized paragraph, the same visual treatment
// TINA's own hand-drafted disclosure block already uses.
var DisclaimerHTML = template.HTML(`<hr><p class="gauntlet-disclaimer"><em>` + template.HTMLEscapeString(Disclaimer) + `</em></p>`)

// AppendDisclaimer appends Disclaimer to a plain-text article body, with a
// blank-line separator. Idempotent-ish in spirit (not literally checked for
// duplicates) -- callers should call this exactly once, at the end of body
// construction, same as tickerlink is meant to be called at ref-formatting
// time rather than post-hoc.
func AppendDisclaimer(body string) string {
	return strings.TrimRight(body, "\n") + "\n\n" + Disclaimer + "\n"
}

// AppendDisclaimerHTML appends DisclaimerHTML to an HTML article body.
func AppendDisclaimerHTML(bodyHTML string) string {
	return bodyHTML + string(DisclaimerHTML)
}

// LinkIssuer is tickerlink.FormatRef with no exchange -- for generators
// (eps, guidance today) that only carry a bare ticker/company name, not a
// structured exchange code. baseURL and ticker empty-checked by
// tickerlink.FormatRef itself; this is a thin, named wrapper so call sites
// read as "gauntlet's own ticker treatment" rather than a bare tickerlink
// call with an empty-string exchange argument that looks like an oversight.
func LinkIssuer(baseURL, companyName, ticker string) template.HTML {
	return tickerlink.FormatRef(baseURL, companyName, "", ticker)
}

// PlainIssuer is the plain-text counterpart to LinkIssuer, for bodies that
// aren't rendered as HTML yet (eps.Article.Body, guidance.Article.Body
// today -- neither has a BodyHTML field/reader page yet, a real, honest,
// named gap: see those packages' own doc comments).
func PlainIssuer(companyName, ticker string) string {
	return tickerlink.PlainRef(companyName, "", ticker)
}

// SkuldmarkTag and SkuldmarkTagHTML surface a real, already-minted
// SKULDMARK-25 instrument identifier (see the SKULDMARK repo -- public
// domain, github.com/emilyspringerton/SKULDMARK; the "updated version"
// referenced here is its v1 field layout, SYMBOL 7 / CIK 10) alongside a
// ticker reference -- founder real-time, 2026-09-07: "ensure SKULDMARK (the
// updated version) is included in GAUNTLET obviously."
//
// Gauntlet does NOT mint IDs itself -- internal/skuldmarkid (adapting
// identity.SecurityRef into skuldmark.Encode) is the one real minting path,
// same as prwatch's own live mintSkuldmarkIDs. These are thin, honest
// display wrappers: called only when a caller already has a real minted ID
// in hand (never guessed, never fabricated here) -- see each generator's
// own call site for where that ID actually comes from.
func SkuldmarkTag(skuldmarkID string) string {
	if skuldmarkID == "" {
		return ""
	}
	return "[SKULDMARK: " + skuldmarkID + "]"
}

func SkuldmarkTagHTML(skuldmarkID string) template.HTML {
	if skuldmarkID == "" {
		return ""
	}
	return template.HTML(`<code class="skuldmark-id" data-skuldmark="` + template.HTMLEscapeString(skuldmarkID) + `">` + template.HTMLEscapeString(skuldmarkID) + `</code>`)
}
