# NORTHSTAR: TINA — Trading Idea, Not Advice Engine

**Status:** Draft v0.1
**Date:** 2026-07-18
**Fits into:** entity-graph signals + EPS/guidance/dividend/buyback/insider watchers → TINA
(structured idea layer, explicit compliance framing) → surfaced via `jon-agent`'s chat persona
and/or a structured feed on `newssite`

---

## 1. Premise, and the name

TINA — **Trading Idea, Not Advice** — is the productized layer that turns FatBaby's already-built
signal pipeline into ticker-level trading *ideas*: here is a real, sourced divergence or pattern;
here is the data behind it; here is how confident that data actually is. It stops there. No
position sizing, no entry/exit price, no "buy" or "sell," no urgency framing. The name is
deliberately load-bearing, not decorative — every output surface TINA produces should read as
"notice this," never "do this." (There's an obvious pun in the name too — There Is No
Alternative — worth keeping, not worth building a joke around.)

This is not a new signal-detection system. FatBaby already detects real, structured events:
`insider_buy`/`insider_sell_cluster` (`form4-watcher`), `dividend_cut`/`raise` (`dividend-watcher`),
`buyback_authorized`/`suspended` (`buyback-watcher`), guidance `raised`/`lowered`/`maintained`
(`guidance-watcher`), confirmed EPS surprises (`eps-reconciler`), governance/succession patterns
(`entity-graph`'s `director_decay`, already live on `newssite` as "Succession Watch" — see the
okemily.com blog post from 2026-07-18 about it). TINA's job is the presentation and compliance
layer on top of signals that already exist, not a new detection engine — matching this session's
own `docs/headlines/live-feed-northstar.md`, which is the same relationship (unify and present
existing signals, don't reinvent detection).

## 2. Relationship to Jon Stockwell (`cmd/jon-agent`)

`jon-agent` already exists: a full persona-driven options strategist (`docs/jon.md`), running on
`:8084`, doing "divergence analysis across pipeline signals, surfaces options setups." Jon is a
**voice** — a specific character with doctrine, tone, and an options-trading lens. TINA should not
duplicate that; it should be the **structured, disclaimer-forward substrate underneath it**:

- Jon's chat responses should be able to cite a TINA idea (a structured record: signal type,
  ticker, the divergence itself, source data, confidence/quality caveat) and narrate it in his
  voice — but the underlying idea record, its sourcing, and its "not advice" framing should be the
  same whether it's rendered through Jon's persona in chat or as a plain structured card on
  `newssite`.
- This separation matters for compliance: a persona (Jon) saying "the setup is obvious" is exactly
  the kind of framing that must never appear in TINA's own structured output, which should read
  more like "the underlying data" — plain, sourced, hedged — regardless of how any given front-end
  chooses to voice it on top.

## 3. What an idea actually contains

```json
{
  "idea_id": "string",
  "ticker": "string",
  "signal_type": "insider_buy | dividend_cut | buyback_authorized | guidance_raised | eps_confirmed | director_decay | ...",
  "observation": "string",       // what was actually detected, plainly stated
  "divergence": "string|null",   // what this contradicts or diverges from, if applicable (Jon's doctrine's own framing: "where alt data says one thing and price says another")
  "source_url": "string",        // the underlying filing/release — same publish-gate discipline as pr-feed.md and the headline-feed northstar: no source, no publish
  "confidence_note": "string",   // honest, e.g. "this signal type has historically run ~12% precision across resolved cases" — entity-graph's own disclosed 11.9% finding is the model for this: say the real number, don't round it up
  "generated_at": "timestamp",
  "disclaimer": "string"         // fixed, prominent, non-negotiable — see §4
}
```

**No fields for**: position size, entry/exit price, timeframe/urgency, or any directive verb
("buy," "sell," "should"). If a future revision wants to add price/timing context, that's a
different, much more carefully-considered product decision, not something to slide in as a field.

## 4. Compliance framing — the actual point of the name

`newssite`'s own `/about` page already states the company's position: "not a news organisation,
not a registered investment adviser, and not affiliated with any company it covers." TINA inherits
that stance directly and makes it more prominent, not less, precisely because it's closer to
"actionable-sounding" content than a plain filing summary is:

- Every TINA output carries a fixed, visible disclaimer — not buried in a footer.
- `confidence_note` is mandatory, not optional, and must state the real, disclosed precision
  history for that signal type where known (entity-graph's overall 11.9%, with `abstention_outlier`/
  `cfo_departure`/`family_control` at exactly 0%, is the standing example of the honesty bar to
  clear — if a signal type's real precision is bad, TINA says so, it doesn't quietly omit the
  signal type or round the number up).
- No urgency language, ever. Jon's own doctrine text ("audacity on entry," "zero hesitation") is
  voice and characterization for the chat persona — it must never leak into TINA's own structured
  `observation`/`divergence` fields, which stay plain and descriptive.

## 5. Build phases

- **Phase 0:** Structured idea generation for the signal types with the clearest disclosed
  precision story already available (start with what's honestly measurable, not what's flashiest)
  — likely `dividend_cut`/`raise` and `buyback_authorized` (deterministic, well-defined events)
  before `director_decay` (already disclosed as noisier) or anything requiring new judgment calls.
- **Phase 1:** Wire `jon-agent` to consume TINA records as its data layer for divergence framing,
  replacing whatever ad-hoc signal access it currently has (audit `cmd/jon-agent/tools.go` first —
  don't assume it needs new sourcing wired in from scratch without checking what's already there).
- **Phase 2:** A structured, disclaimer-forward TINA feed/section on `newssite` — plain cards, not
  routed through Jon's voice — for users who want the substrate without the persona.
- **Phase 3:** Cross-reference with the headline-feed northstar (`docs/headlines/
  live-feed-northstar.md`) — should a TINA idea's generation also emit a headline-feed entry?
  Probably yes, but as its own `event_type` (`tina_idea`), not folded into existing types, since
  it carries the compliance-framing requirements this document specifies and other headline types
  don't.

## 6. Open questions

- Should TINA generate ideas continuously (a background process, matching the pollers) or only
  on-demand (a query-time computation)? Continuous means another always-on process — this session
  has a live, disclosed lesson (BACKLOG S153-14) about adding processes with full-history-replay
  patterns casually. If continuous, design the persistence/rebuild story explicitly, don't inherit
  the same fragility by default.
- Does `confidence_note` need per-ticker or per-sector granularity, or is the signal-type-level
  precision number (entity-graph's existing disclosed breakdown) sufficient honesty for now?
- Is there a real legal/compliance review this needs before any public-facing output ships, given
  the explicit "not advice" framing is doing real work here, not just branding? Flag for founder
  decision — this document is the technical design, not a substitute for that review.
- Relationship to `docs/GTM_FUNNEL.md` (Ask Emily GTM funnel, free tier → subscription) — is TINA
  a free-tier surface, a paid one, or does that split by signal type/confidence tier? Not decided
  here.
