# NORTHSTAR: Combined Live Headline Feed ("MTWire-style")

**Status:** Draft v0.1
**Date:** 2026-07-18
**Fits into:** every signal-producing poller → unified headline envelope → `feedserver` (TCP,
downstream/partner consumers) + a new WebSocket bridge → `newssite` (live front-page ticker)

---

## 1. Premise

FatBaby already runs ten independent signal-producing processes — `secwatch` (filings),
`prwatch`/`prwatch-body` (press releases), `processor` (structured signal extraction),
`eps-processor`/`eps-reconciler` (EPS), `guidance-watcher` (forward guidance),
`form4-watcher` (insider transactions), `dividend-watcher`, `buyback-watcher`, `nt-watcher`
(late filings). Each writes to its own corner of the event store. None of them today feed a
single, ordered, low-latency, terse headline stream a person or a downstream system can just
watch scroll by.

That's the product: a combined, real-time headline wire in the spirit of MT Newswires — short,
fast, attributed, one line per event, across every signal type FatBaby already tracks. Not a new
research surface; a distribution layer for research FatBaby is already doing.

This builds directly on `docs/headlines/pr-feed.md` (already drafted: press-release → article
pipeline, with a strong attribution/no-invented-facts discipline this document inherits rather than
relitigates) — but widens the scope from "press-release articles" to "every signal type, one feed."

## 2. What already exists (don't rebuild)

- **`feedserver`** (`cmd/feedserver`, package `feedserver/`) — a real TCP server with a custom
  binary framed protocol (`Magic 0xFBAB`, versioned, frame types `Hello/Welcome/Record/Ack/Ping/
  Pong/Behind/Goodbye` — see `feedserver/frame.go`), sequence-based resume/backfill
  (`FromSeqFullBackfill` / `FromSeqRealtimeOnly`), TLS support, and multi-tenant routing via
  `broker.Registry` (per-tenant keys, `config/routes.json`). It reads from an `eventstore.EventStore`
  and streams records to connected clients. **This is the right foundation** — it is not currently
  running, and nothing currently publishes a unified stream into it, but the transport layer is
  built and shouldn't be redesigned.
- **`docs/headlines/pr-feed.md`** — the PR-derived article pipeline (schema, attribution rules,
  quality metrics, build phases). Its `Article` schema is a *specialization* of this document's
  unified headline envelope (§3) for the press-release case specifically.
- **The ten pollers listed above** — each already emits structured events into the eventstore.
  This northstar's job is normalization and distribution, not new signal detection.

## 3. Unified headline envelope

Every signal type gets mapped to one common shape before it reaches the feed. This is what
`feedserver` streams and what any consumer (TCP client, WebSocket browser client, a future partner
API) renders against — one schema, many event types, so a client doesn't need bespoke logic per
signal type to do basic filtering/badging.

```json
{
  "headline_id": "string",       // stable, dedupe-able
  "seq": 0,                       // eventstore sequence number — this IS the resume/backfill key
  "occurred_at": "timestamp",
  "event_type": "filing_discovered | eps_confirmed | guidance_raised | insider_buy | dividend_cut | buyback_authorized | late_filing | pr_article | ...",
  "ticker": "string|null",
  "issuer": "string|null",
  "headline": "string",           // one line, terse, MTWire-style — the whole point
  "importance": 0,                // 1-10, already computed by processor/each watcher
  "source_type": "sec_filing | press_release | derived_signal",
  "source_url": "string|null",    // link to the original filing/release — REQUIRED where it exists
  "detail_url": "string|null"     // link into newssite's full article/detail page
}
```

**Publish gate, inherited from `pr-feed.md`:** anything claiming a fact needs a `source_url` or a
`detail_url` a reader can actually click through to. No headline that can't be traced back to its
source, same discipline as the existing PR-article work.

## 4. Publish path

```
secwatch / prwatch / processor / eps-* / guidance-watcher / form4-watcher /
dividend-watcher / buyback-watcher / nt-watcher
         │
         │  (each already emits its own structured event)
         ▼
  headline-normalizer  ◄── NEW, thin — one mapping function per event_type,
         │                  event_type → unified envelope (§3). No new
         │                  detection logic; pure transform.
         ▼
  feedserver's backing eventstore (append)
         │
         ├──► feedserver TCP clients (existing protocol, unmodified)
         │
         └──► NEW: WebSocket bridge ──► newssite front page (live ticker)
```

**`headline-normalizer`** is the only new backend component. Cheapest correct design: not a new
long-running process, but a small library function each existing watcher calls right after it
already writes its own structured event — one extra `Append` call into the feed's eventstore, no
new poller to keep alive, no new failure mode to monitor (directly relevant after tonight —
SECTION 152/153's whole lesson was "don't add unsupervised processes casually").

## 5. The WebSocket bridge (newssite front page)

`feedserver`'s clients speak its custom binary frame protocol — a browser can't do that directly.
New, small component: a WebSocket handler (likely inside `newssite` itself, since that's already
the public-facing HTTP server, rather than a fifth process) that:

1. Opens an internal read-only subscription to the same feed eventstore feedserver reads from
   (or, simpler: subscribes to feedserver itself as a TCP client, decodes frames, re-encodes as
   JSON over the WebSocket — keeps feedserver as the single source of truth for "what's the current
   sequence/backfill state," rather than two components independently tailing the same store).
2. Sends each new headline as a small JSON message the front-page JS appends to a scrolling ticker.
3. On connect, backfills the last N headlines (matching `FromSeqFullBackfill` semantics) so a
   page load isn't a blank ticker waiting for the next event.

This is the same "same-origin proxy, no new domain/cert" pattern already used for okemily.com's
`/api/` and `/news/` proxies tonight — the WebSocket endpoint lives on newssite's existing origin,
not a new port needing its own DNS/cert.

## 6. Build phases

- **Phase 0:** `headline-normalizer` mapping functions for the signal types that already have the
  clearest one-line headline shape — `eps_confirmed`, `dividend_cut/raise`, `buyback_authorized`,
  `insider_buy` (all already terse, structured signals — the easy 80%). Publish into feedserver's
  store. No consumer yet; verify by reading the store directly.
- **Phase 1:** Bring `feedserver` up under systemd (matching tonight's hardening pattern — this is
  a new always-on process, it needs the same discipline from day one, not bolted on after an
  outage). Verify a raw TCP client can connect, backfill, and receive live records.
- **Phase 2:** WebSocket bridge + newssite front-page ticker UI. This is the visible, demo-able
  milestone.
- **Phase 3:** Fold in `pr-feed.md`'s article pipeline as the `pr_article` event_type — the
  richest, most editorially-sensitive signal type, deliberately last since it inherits the most
  scrutiny (attribution, no-invented-facts) from that existing draft.
- **Phase 4:** `filing_discovered`/`late_filing`/`guidance_*` — the remaining signal types, once
  the pipeline shape is proven on the simpler ones.

## 7. Quality metrics

- **Latency:** signal detected → headline live on the feed. Target: seconds, not minutes — the
  entire value proposition is speed (MTWire's actual differentiator vs. slower wire services).
- **Source-binding:** 100% of headlines resolve to a `source_url` or `detail_url` — inherited
  directly from `pr-feed.md`'s publish gate, applied across every event_type, not just PR articles.
- **Feed continuity:** a client that disconnects and reconnects gets exactly the backfill it asks
  for via `seq` — zero silent gaps. (`feedserver`'s resume protocol already supports this; this is
  a verification target, not new design.)

## 8. Open questions

- Does `headline-normalizer` run inline (each watcher calls it directly, per §4) or as a thin
  fan-in step reading each watcher's own event stream? Inline is simpler and has no new process to
  monitor; fan-in is more decoupled but is exactly the kind of "one more unsupervised background
  process" this whole session has been fixing instances of. Lean inline unless a concrete reason
  emerges not to.
- Rate/importance filtering — does every signal hit the feed, or only above some importance
  threshold? MTWire-style feeds are valuable partly *because* they're selective, not a firehose.
- Multi-tenant story: does the WebSocket bridge need the same tenant-key gating `broker.Registry`
  already does for TCP clients, or is the front-page ticker simply public (same trust level as
  the rest of okemily.com/newssite)?
- Historical/backfill depth on first WebSocket connect — how many headlines is "the last N" in
  practice, and does it differ from feedserver's own backfill semantics?

## 9. What this deliberately does NOT do (yet)

- No new detection/signal logic — every event_type this document names already exists as a
  poller's output today.
- No new always-on process beyond `feedserver` itself (already built, just not running) and the
  WebSocket bridge (folded into `newssite`, not a standalone service) — directly responding to
  tonight's repeated lesson about unsupervised processes being the actual operational risk.
