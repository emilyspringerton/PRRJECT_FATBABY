# Content Type Taxonomy — what exists today, and where "prtype" fits

Founder real-time, 2026-09-02: "do we have a good sense of how we are storing the data in terms
of content type?" This doc answers that directly, from the real code, not from a proposed
redesign — then names the one real, minimal addition made alongside it (press-release
provider/subtype).

## The honest answer: partially, and it's one flat field, not a hierarchy

There is a real, working, already-broadly-used content-type field: **`SourceType`**
(`pkg/intelligence.SourceDocument.SourceType`, `json:"source_type"`). It is NOT a formal enum —
it's a bare string, set by string-literal at each producer site, with no single file defining the
full value set. Grepping the real, current call sites gives the real, current taxonomy:

| `SourceType` value | Set by | What it actually is |
|---|---|---|
| `sec_8k` | `internal/processor` (`SourceTypeForForm`) | An 8-K SEC filing |
| (other SEC forms, via `SourceTypeForForm`) | `internal/processor/worker.go` | 10-K/10-Q/etc., one value per real form type |
| `press_release` | `cmd/pr-indexer` | A press release body (PRNewswire today) |
| `market_movers` | `internal/newssite` | Quick-template daily mover writeups — this is your "TINA-style quick template" category, already real and already separate |
| `emily_commentary` | `internal/newssite` | Emily-authored analysis/commentary — the closest existing thing to "contributed content" |

This is a real, working **content type** dimension — every one of `internal/newssite/render.go`'s
own display/labeling branches (`sourceTypeLabel`, the `/wire` page, RSS) already switches on it.
It is genuinely not nothing, and doesn't need to be invented from scratch.

**What's actually missing** is the second axis: a **subtype/provider** dimension *within* a
`SourceType`. Before this pass, `press_release` had no way to say *which wire service* — the
`Source` field on the underlying `eventstore.Event` was hardcoded to the literal `"prnewswire"`
string everywhere in `prwatch/runner.go`, regardless of what the `Client` was actually configured
to scrape (a real, found bug, fixed alongside this doc — see CHANGELOG).

## The fix: `SourceProvider`, not a parallel taxonomy

Rather than invent a new `content_type`/`prtype` pair of fields running alongside the existing
`SourceType`, this pass adds one new field next to it:

```go
// pkg/intelligence/source_document.go
SourceType     string // existing — "press_release", "sec_8k", ...
SourceProvider string // NEW — "prnewswire", "businesswire", ... only meaningful for press_release
```

Reusing `SourceType` as the top-level "content type" (rather than introducing a second,
competing field with the same job) keeps every existing `SourceType == "press_release"` check
across the codebase (`internal/newssite/render.go`, `apiserver/server.go`,
`internal/newssite/docindex`) correct with zero changes. `SourceProvider` is the real
subtype/prtype slot: empty for anything that isn't a press release (SEC filings have exactly one
real provider — EDGAR — so a subtype field would carry no information there), populated for press
releases with which wire service it came from.

Real flow, end to end:

```
prwatch.Client (scrapes one wire service)
  -> RunnerConfig.SourceName ("prnewswire" | "businesswire", defaults "prnewswire")
  -> PressReleaseDiscovered.Source / eventstore.Event.Source
  -> prwatch.BodyFetchedEvent.Source (mirrored through crawler.go)
  -> pr-indexer sets SourceDocument.SourceProvider = ev.Source
  -> docindex.DocSummary.SourceProvider
  -> GET /v1/press-releases/{ticker}?provider=businesswire (signalapi)
  -> EDIS's [edis_press_releases ticker=X provider=businesswire] shortcode
```

## Would this generalize to guidance articles / markets-desk / contributed content?

Yes, and it should use the exact same two-field shape, not a new one per category:

- **Quick-template guidance/EPS articles** (the "TINA" style ask) already partially exist as
  `market_movers` — if/when there's more than one *template family* generating that content, the
  same `SourceProvider`-shaped field (maybe literally reused, maybe a sibling `TemplateName`) is
  the right slot, not a new top-level type.
- **Contributed content** (a named analyst/columnist, not Emily) is a real, new `SourceType`
  value (e.g. `contributed`), with the contributor's own byline as its `SourceProvider`-equivalent
  — same shape as press-release-provider, applied to a person instead of a wire service.
  Not built — named here as the next real instance of the same pattern, not a new pattern.
- **Third-party ingested data** (a whole other business's feed) is also just a new `SourceType`
  value at the point it's actually ingested — the event-sourced pipeline (`eventstore` +
  `source_document_persisted`) doesn't care what produced a `SourceDocument`, only that one real
  producer emits one.

The one thing this taxonomy deliberately does NOT try to solve here: a formal, centrally-defined
enum/constants file for every `SourceType` value (today they're still bare string literals
scattered across `internal/processor`, `cmd/pr-indexer`, `internal/newssite`). That's a real,
separate, lower-risk cleanup (turn scattered literals into named constants, add a lint/test that
fails on an unrecognized value reaching a renderer) — worth doing before a third or fourth
`SourceType` shows up, not required to ship BusinessWire.

## BusinessWire specifically: real, honest current blocker

This pass wires the entire type/subtype pipeline (client → event → SourceDocument → docindex →
signalapi → EDIS) end to end and proves it with the one real, already-working provider
(`prnewswire`). It does **not** ship a working BusinessWire scraper. Checked directly, not
assumed: `businesswire.com`'s news-list and newsroom pages both return HTTP 403 to this session's
own fetch tooling (bot-protection, not a URL mistake — tried the general news page, an
industry-specific page, and their own RSS help page), and their real RSS feed URLs are
subscriber-token-gated (`?rss=<opaque token>`), not a generic public endpoint prwatch could poll
the way it polls PRNewswire's plain HTML today. Writing scrape regexes against markup nobody in
this session has actually seen would be guessing, not shipping — this is flagged, not silently
worked around. `prwatch.RunnerConfig.SourceName` and the whole downstream pipeline are ready the
moment someone with real browser access to businesswire.com hands over its actual list-page HTML
structure or a real, working feed URL.
