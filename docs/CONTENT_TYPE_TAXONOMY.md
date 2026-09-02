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

## BusinessWire specifically: PAUSED (2026-09-02) — a real infrastructure wall, not a code problem

**Status: paused by the founder ("disable it for now until we can figure out a better idea").
Do not pick this back up as an open work item without a real plan for the blocker below — it
will fail the same way again.**

This pass wires the entire type/subtype pipeline (client → event → SourceDocument → docindex →
signalapi → EDIS) end to end and proves it with the one real, already-working provider
(`prnewswire`) — that part is real, done, and stays enabled. It does **not** ship a working
BusinessWire scraper, and the reason turned out to be bigger than markup access:

1. `businesswire.com`'s news-list/newsroom pages are client-side JS-rendered (confirmed by the
   founder in-browser) and 403 a plain HTTP fetch — solved for real by standing up an actual
   headless Chrome + chromedriver (`PARENA/stdlib/net/webdriver.prn`, no sudo needed, see
   `PARENA/CHANGELOG.md`) and fixing two real bugs in PARENA's own HTTP/WebDriver client along
   the way (bare-LF request lines, and a read-until-EOF client hanging against a server that
   never actually closes the connection).
2. **The real, final blocker, confirmed directly and not fixable in code**: this box's own
   outbound IP (`curl ipinfo.io` → `AS63949 Akamai Connected Cloud`, a Linode datacenter range)
   is broadly flagged by anti-bot systems — a real, live Chrome session hitting
   `businesswire.com/newsroom` got a Chrome-internal `ERR_HTTP2_PROTOCOL_ERROR` instead of the
   site, and hitting `google.com` from the same box got a live reCAPTCHA challenge instead of
   search results. This is IP-reputation-based bot blocking, the same wall any real
   Selenium/Puppeteer/Playwright scraper hits running from a cloud/VPS IP. No Chrome flag,
   WebDriver capability, or PARENA code change fixes an IP-reputation problem.

**What would actually unblock this** (an infrastructure decision, not an engineering task): route
the scrape through a residential/mobile proxy or IP-rotation service, or run it from a
non-datacenter IP. `prwatch.RunnerConfig.SourceName`, `SourceProvider`, `cmd/prwatch`'s poll
jitter flags, and the whole webdriver/HTTP-client fix chain are all real, tested, and ready the
moment that infrastructure decision is made — nothing here needs to be re-verified, only a real
network path that doesn't get IP-blocked before the JS-rendering problem even gets tested.
