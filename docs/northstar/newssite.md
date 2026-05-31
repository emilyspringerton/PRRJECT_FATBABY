# NORTHSTAR: The Newssite as a Newspaper

## Turning the FATBABY event store into a usable financial-intelligence publication

**Status**: P0–P3 complete; P4 in progress (RSS + succession watch + live done; corrections box + accessibility pass outstanding)
**Version**: 1.1
**Date**: 30 May 2026 · updated 31 May 2026
**Scope**: `cmd/newssite`, `internal/newssite/*` (and a small set of new read-model packages)
**Architecture**: FATBABY-native — Go standard library only, no external dependencies, no CMS, no database (yet)

---

## 1. Executive intent

We are sitting on a newsroom's worth of intelligence and presenting it like a filing cabinet.

The pipeline already extracts director-level governance signals, activist-risk composites, vote histories, board relationships, auditor changes, and cleaned primary-source text across hundreds of companies. The news site (`:8082`) shows **one** of those surfaces: a flat reverse-chronological list of `source_document_persisted` records and a full-text detail page. No signals. No companies. No people. No sections. No search. The richest data in the system never reaches the page.

**The north star:** make the newssite *actually usable* by mapping the data we already have onto the furniture of a real newspaper — front page, sections, datelines, bylines, breaking-news ticker, company desks, people pages, the wire, the archive. We invent no new data. We expose what exists, using tropes every reader already knows how to navigate.

**Definition of "usable" for this document:**

1. A reader landing on `/` immediately sees *what matters most right now* — not the most recent document, but the highest-severity signal.
2. Every company we cover has a page. Every director we track has a page. Both are reachable in one click from anything that mentions them.
3. A reader can answer "what's happening with `SCHW`?", "who is this director and where else do they sit?", and "show me everything critical this week" without knowing what an accession number is.
4. The primary source is always one click away, but is never the *first* thing shown.

**Non-goals (explicit):** No WordPress. No headless CMS. No SQLite or Postgres in v1. No React/SPA. No auth, accounts, comments, or write paths. This stays a read-only, server-rendered Go binary. See §11.

---

## 2. The data we already have (and where it lives)

| Surface | Lives in | Shape | Exposed on site today? |
|---|---|---|---|
| **Source documents** | `var/secwatch/` eventstore, `source_document_persisted` | ticker, source_type, form, document_url, cleaned_text, char_count, persisted_at | ✅ (the only thing) |
| **Pipeline signals** | `var/secwatch/` eventstore, `signal_generated`; indexed by `internal/signalindex` | ticker, signal_type, importance (int), sentiment (float), summary, impact_analysis, raw_metadata{form, filing_date} | ❌ |
| **Governance signals** | `var/entity-graph/signals.ndjson` | type (13 kinds), entity, severity, confidence, score, interpretation, valid_through, metadata | ❌ |
| **People (directors/execs/auditors)** | `var/entity-graph/nodes.ndjson` | canonical_id, name, type, first/last appearance, filing_count, centrality, per-filing vote counts & approval % | ❌ |
| **Board relationships** | `var/entity-graph/edges.ndjson` | source, target, board_co_member, companies[], strength | ❌ |
| **Auditor records** | `var/entity-graph/auditors.ndjson` | ticker, auditor, filing_date (+ change detection) | ❌ |
| **Ticker rollups** | `internal/signalindex` (`TickerSummary`) | signal_count, latest_signal, latest_type | ❌ |
| **Live signal stream** | dashboard SSE `:8080`; `signal_generated` tail | event-by-event push | ❌ (separate app) |
| **Query API** | `signalapi` `:9091` — `/v1/signals`, `/v1/signals/{ticker}`, `/v1/signals/{ticker}/latest` | JSON, filterable by `signal_type`, `min_importance`, `from`, `limit` | n/a (API, not page) |

The governance-signal taxonomy we can headline against (from `internal/entitygraph/signals.go`):

`director_friction` · `nomination_rejection` · `director_decay` · `high_trust_director` · `governance_entrenchment` · `broker_nonvote_anomaly` · `compensation_concern` · `auditor_change` · `abstention_spike` · `activist_risk` · `director_link` · `family_control` · `governance_health_index`

Each carries a `severity` (low / medium / high / critical), a `confidence`, a `score`, and a human-readable `interpretation` string — which is, conveniently, already written in something close to news prose.

---

## 3. The newspaper mapping (the core idea)

Every classic news-site trope has a natural data source already in the system. This table *is* the design.

| Newspaper trope | What it becomes here | Backed by |
|---|---|---|
| **Front page / above the fold** | Lead story = highest-severity unexpired governance signal; secondary leads below it | entity-graph signals sorted by severity × confidence × recency |
| **Breaking-news banner / ticker tape** | Scrolling strip of `critical`/`high` signals from the last 24–48h | `nomination_rejection`, `activist_risk`, `governance_entrenchment` |
| **Sections / desks** | One section per signal family: **Governance**, **Activism Watch**, **Boardroom**, **Auditor Watch**, **Pay & Proxy**, **The Wire** (press releases), **Filings** (raw SEC) | signal_type → section map |
| **Headline** | Generated per signal (see §7) | signal type + entity + ticker + score |
| **Standfirst / deck / kicker** | The signal `interpretation` string, trimmed | entity-graph signal |
| **Byline** | The desk that filed it: *"By the Entity-Graph Desk"*, *"By the Wire Desk"*, *"By SEC Watch"* | originating pipeline stage |
| **Dateline** | `TICKER — filing_date.` prefix on the lede | filing metadata |
| **Topic / tag page** | **Company desk** at `/company/{ticker}` — every signal, filing, director, and auditor for one issuer | signalindex + entity-graph join on ticker |
| **People-we-cover / staff pages** | **Director dossier** at `/person/{canonical_id}` — vote history, approval trend, every board they sit on | nodes + edges |
| **Related coverage / "see also"** | Board co-members, other companies a director touches, other filings by same ticker | entity-graph edges |
| **Most read / trending** | **Most-watched** rail: companies by signal volume & severity this week | TickerSummary |
| **Op-ed / analysis** | The `impact_analysis` field on pipeline signals, shown as a pull-quote | intelligence.Signal |
| **Fact box / sidebar** | Vote counts, approval %, broker non-votes, confidence — the numbers behind the story | FilingAppearance |
| **The wire service** | Press releases as a chronological feed, clearly labelled vs. SEC filings | source_type = press_release |
| **Obituaries** *(handle with a light touch)* | **"On the way out"** rail — `director_decay` directors, framed as *succession watch*, not death | director_decay signals |
| **Corrections box** | The recursive self-improvement loop: when a rule changes and re-scores prior signals, note it | rule version / config changes |
| **The morgue / archive** | The append-only event store — every record permalinked by identity & sequence | eventstore |
| **Live blog / the desk** | SSE-backed "incoming" page that streams new signals as they land | dashboard SSE feed |
| **Masthead / colophon** | A page stating what this is, what the signals mean, base rates, and limitations | static |

The point of leaning on tropes: a reader who has used *any* news site already knows that the big thing at the top is important, that a section link narrows the topic, that a byline tells you who's responsible, and that a tag is a thing they can click to see more. We get usability for free by not inventing a new vocabulary.

---

## 4. Information architecture (sitemap)

All served from the single `newssite` binary on `:8082`.

```
/                         ✅ Front page — leads + ticker tape + sections rail + most-watched
/section/{slug}           ✅ Desk page — one signal family, newest first
                             slugs: governance, activism, boardroom, auditor, pay, wire, filings
/ticker/{symbol}          ✅ Ticker desk — signals, filings, directors, auditor, fact box
/company/{ticker}         ✅ 301 redirect → /ticker/{symbol}  (URL shape changed; old links still work)
/person/{canonical_id}    ✅ Director dossier — SVG sparkline, vote history, board interlocks, signals
/doc/{identity}           ✅ The filing — enriched primary source (keeps existing URL shape)
/wire                     ✅ The Wire — press releases only, chronological
/breaking                 ✅ Critical & high signals (the banner's full page)
/tickers                  ✅ Ticker directory — all covered symbols, sortable
/search?q=                ✅ Search across tickers; exact-match redirects straight to /ticker/
/api/tickers?q=           ✅ JSON typeahead endpoint for the masthead search box
/archive                  ✅ The morgue — full reverse-chron event index, permalinked
/live                     ❌ SSE stream of incoming signals — not yet built (P3 remainder)
/about                    ✅ Masthead / colophon — what signals mean, base rates, caveats
/healthz                  ✅ Liveness (unchanged ops surface)
```

Backwards compatibility: `/` and `/doc/{identity}` already exist and keep working; everything else is additive. No existing permalink breaks.

---

## 5. Page specifications

### 5.1 The Front Page (`/`)

The most important change in the whole project. Today `/` is `ReadLatest(...)` rendered as a list. It becomes an *edited front page*.

- **Lead story (hero):** the single highest-ranked live signal. Ranking = `severity_weight × confidence × recency_decay`, restricted to `valid_through >= today`. Critical always outranks high. Big headline, deck from `interpretation`, dateline, byline, link to the company desk and the filing.
- **Secondary leads (2–4):** next-ranked signals, smaller headlines, in a column.
- **Ticker tape (top of page):** horizontal strip of the latest `critical`/`high` items — headline + ticker, each linking to its story. Pure HTML/CSS marquee-style scroll; no JS required.
- **Sections rail:** the seven desks, each showing its own latest headline so the front page previews the whole paper.
- **Most-watched rail (sidebar):** top companies this week by signal volume and max severity, each linking to its company desk.
- **The Wire strip:** last few press releases, clearly separated from filings.
- **Empty state:** keep the honest current copy ("the processor has not run or no filings have been discovered") but route it through the new layout.

Ranking is computed once at request time over the in-memory read models (§9) — cheap, no storage change.

### 5.2 Section / Desk pages (`/section/{slug}`)

A filtered front page. One signal family, newest first, paginated by `?before={seq}`. Header explains the desk in one sentence ("**Activism Watch** — entrenchment and friction co-occurring; base rate ~60% for a 13D within six months"). Each item: headline, deck, dateline, byline, company link.

Signal-type → section map:

| Section | Signal types |
|---|---|
| Governance | governance_health_index, governance_entrenchment, family_control |
| Activism Watch | activist_risk, director_friction, nomination_rejection, director_link |
| Boardroom | high_trust_director, director_decay, abstention_spike |
| Auditor Watch | auditor_change |
| Pay & Proxy | compensation_concern, broker_nonvote_anomaly |
| The Wire | (press releases) |
| Filings | (raw SEC source documents) |

### 5.3 The Ticker Desk (`/ticker/{symbol}`) ✅ Built — see `newssite-tickers.md` for full spec

The tag page. Everything we know about one issuer, in newspaper order:

- **Masthead line:** ticker, company name (if known), current auditor, last filing date.
- **Lead:** the company's highest-severity live signal.
- **Signal stream:** all signals for the ticker, newest first (from `signalindex.ForTicker` + entity-graph signals merged).
- **The board (sidebar):** directors with latest approval %, sorted by friction (lowest approval first). Each links to the person page. Friction directors flagged.
- **Auditor box:** current auditor and any recorded change with date.
- **Filings list:** source documents for the ticker, linking to `/doc/{identity}`.
- **Fact box:** counts — total signals, critical/high count, distinct directors tracked, first/last seen.

### 5.4 The Director Dossier (`/person/{canonical_id}`) ✅ Built

The "people we cover" page — the data nobody has ever seen rendered.

- **Header:** name, role (director/executive/auditor), centrality ("sits on N boards"), first/last appearance.
- **Approval trend:** per-filing approval % over time, drawn as an inline SVG sparkline (no chart library — hand-emitted `<polyline>`). Friction years marked.
- **Vote fact box (per appearance):** for / against / abstain / broker-non-vote counts, approval %, ticker, form, date.
- **The interlocks ("also on the board of"):** board co-members from edges, and the other companies this director touches — each a link. This is the cross-company risk-propagation story made visible.
- **Signals about this person:** friction, decay, high-trust, family-control, director-link.

### 5.5 The Filing (`/doc/{identity}`)

Keep the existing route and the cleaned-text body, but stop treating the document as the whole story:

- **Above the body:** a headline (form + ticker + the strongest signal derived from this filing, if any), dateline, byline ("By SEC Watch" / "By the Wire Desk").
- **Signal callouts:** any signals whose metadata ties them to this filing, rendered as a sidebar with links to their sections.
- **Fact box:** form, source type, char count, persisted-at, original `document_url` (kept, validated by the existing `isSafeLink`).
- **Related coverage:** other filings for the same ticker; the company desk link.
- **Body:** the cleaned text, in the existing serif reading column.

### 5.6 The Wire (`/wire`)

Press releases only (`source_type = press_release`), chronological, labelled distinctly from SEC filings so a reader never confuses a company's own marketing with a regulatory disclosure. This honesty is itself a usability feature.

### 5.7 Breaking (`/breaking`)

The banner's full page: all `critical` and `high` signals from the last 48h, severity then recency. This is the "front page if you only had ten seconds" view.

### 5.8 Search (`/search?q=`)

Server-side, in-memory, no index server. Match against ticker, director name, headline, deck, and (cheaply) body substrings. Group results by type: Companies, People, Stories, Filings. Start simple — case-insensitive substring over the read models — and leave the seam open for the SQLite FTS migration noted in §11.

### 5.9 The Archive / Morgue (`/archive`)

The append-only store, surfaced honestly: reverse-chronological list of every record, permalinked by identity and sequence, filterable by event type. This is where "expose our data as much as possible" becomes literal — nothing is hidden, everything is addressable.

### 5.10 The Live Desk (`/live`)

Progressive enhancement only. The page renders a static recent-signals list server-side; if JS is on, it subscribes to the existing SSE feed and prepends new signals as they arrive. Works fully without JavaScript. No framework — a dozen lines of `EventSource`.

### 5.11 The Masthead (`/about`)

What this publication is, what each signal type means, the base rates we claim (e.g. the ~60% 13D-within-six-months figure for `activist_risk`), confidence semantics, and a plain limitations statement: signals are model-derived, not investment advice. A newspaper has a masthead; an intelligence product needs a methodology note. Same page.

---

## 6. Headlines, decks, datelines, bylines

The system's `interpretation` strings are already nearly publishable. The job is to turn each signal into a four-part unit:

- **Kicker** (section label): `ACTIVISM WATCH`
- **Headline** (generated, ≤ ~80 chars): see table
- **Dateline** (lede prefix): `SCHW — 21 May 2026.`
- **Deck** (standfirst): the trimmed `interpretation`
- **Byline**: the desk that produced it

### 6.1 Headline templates per signal type

| Signal | Headline template |
|---|---|
| nomination_rejection | `{Director} fails to win majority at {Ticker}` |
| activist_risk | `{Ticker} shows activist setup: entrenchment meets friction` |
| director_friction | `{Director} draws {100−score}% opposition at {Ticker}` |
| governance_entrenchment | `{Ticker} board defense blocks majority-backed proposal` |
| director_decay | `Support for {Director} keeps sliding at {Ticker}` |
| auditor_change | `{Ticker} switches auditors to {Auditor}` |
| compensation_concern | `Investors push back on {Ticker} executive pay` |
| broker_nonvote_anomaly | `Unusual broker non-vote levels at {Ticker}` |
| abstention_spike | `Abstentions spike on {Ticker} proposal` |
| family_control | `Founder-family grip persists on {Ticker} board` |
| high_trust_director | `{Director} re-elected with strong backing at {Ticker}` |
| director_link | `{Director}'s friction spans {N} boards` |
| governance_health_index | `{Ticker} governance health: {grade}` |

Headlines must degrade gracefully when a field is missing (no director name → fall back to the ticker-level phrasing). Headline generation is pure and table-driven, so it lives in one well-tested function and is trivial to tune later.

### 6.2 Byline → desk map

| Originating data | Byline |
|---|---|
| entity-graph governance signal | By the Entity-Graph Desk |
| pipeline `signal_generated` | By the Signals Desk |
| SEC source document | By SEC Watch |
| press release | By the Wire Desk |

---

## 7. House style (the broadsheet look)

Lean *into* the newspaper metaphor visually; it does usability work for us. Keep it dependency-free CSS.

- **Type:** the existing Georgia serif body stays for reading columns; a single sans-serif (system-ui) for headlines, kickers, datelines, and nav — the classic serif-body / sans-furniture split.
- **Hierarchy:** the front-page lead headline is genuinely large; secondary leads clearly smaller; section kickers small, uppercase, letter-spaced.
- **Severity colour:** a restrained, consistent palette — critical=red, high=amber, medium=slate, low=muted. Used on kickers and the ticker tape, never on body text.
- **Rules & columns:** hairline horizontal rules between stories (already in the current CSS), a two-column front page on wide screens collapsing to one on mobile, via plain CSS grid/flex.
- **Datelines** in small caps; **fact boxes** in a bordered sidebar with sans-serif labels.
- **No web fonts, no icon packs, no JS for layout.** The whole stylesheet stays inline or in one `//go:embed` file. The current single `<style>` block is the seed; grow it, don't replace the philosophy.

---

## 8. Architecture (kept deliberately simple)

You asked to keep dependencies out and skip the CMS — so we do, and the codebase is already shaped for it.

**Principles**

1. **One binary, standard library only.** `net/http` + `html/template`. No router, no ORM, no DB. Same as today.
2. **Read models built at startup by tailing the stores.** This is exactly the pattern `internal/signalindex` already uses (it even carries a `TODO(scale)` marking the SQLite seam). We add siblings, not a database.
3. **Render with `html/template`, not `fmt.Fprint`.** The current hand-concatenated HTML in `render.go` won't survive seven page types. Move to embedded templates; keep the existing escaping discipline (`html.EscapeString` → template auto-escaping).
4. **Background refresh.** Read models refresh on a ticker (e.g. every few seconds) by reading new records past their cursor — the same cursor-resume idiom the rest of the pipeline uses. Pages read from the in-memory models, so requests stay fast and never touch disk in the hot path.

**New internal packages (small, focused)**

| Package | Responsibility |
|---|---|
| `internal/newssite/docindex` | tail `source_document_persisted`; documents by ticker, by identity, by source_type |
| `internal/newssite/graphread` | load `var/entity-graph/*.ndjson`; people, edges, auditors, governance signals; lookups by ticker & canonical_id |
| `internal/newssite/edition` | the "editor": rank signals, build the front page, map types→sections, generate headlines |
| `internal/newssite/view` | `html/template` set + a typed view-model per page; one render entry point per route |

Reuse `internal/signalindex` directly for pipeline signals — it already does ticker indexing and sorted inserts.

**Wiring** stays in `cmd/newssite/main.go`: open the eventstore (as now), construct the indexes, start their refresh goroutines, mount the routes on a `*http.ServeMux`, keep the existing graceful-shutdown block.

**What we explicitly do not add:** no `gorilla/mux`, no templating engine beyond stdlib, no Tailwind/build step, no SQLite — yet (§11).

---

## 9. Build phases

Each phase is independently shippable and leaves the site working.

**P0 — Reskin & templatize (foundation). ✅ Complete.**
`html/template` with broadsheet house style. Front page is a real layout; `/doc/` has fact box and dateline.

**P1 — Surface the signals (the unlock). ✅ Complete.**
`graphread` + `signalindex` + `edition` package. Lead story is the highest-severity live signal. Ticker tape, `/breaking`, section routing all live. Front page historical-article suppression added (backfill filings >90 days old route to ticker pages only, not the front page).

**P2 — Sections, companies, people. 🔄 Mostly complete.**
- ✅ `/section/{slug}` — all seven desks
- ✅ `/ticker/{symbol}` — full layout per `newssite-tickers.md` spec; replaces `/company/{ticker}` (301 redirect kept)
- ✅ `/tickers` — directory sorted by activity and alpha
- ✅ `/person/{canonical_id}` — director dossier with SVG sparkline (approval trend over time), per-filing vote breakdown table, signals about the person, board-interlock co-director grid

*P2 complete.* Full navigation flow works: front page → ticker desk → director dossier → another ticker, zero URL typing required.

**P3 — Wire, search, archive, live. ✅ Complete.**
- ✅ `/wire` — press releases only
- ✅ `/search?q=` — exact-match redirect + prefix/substring ranking; masthead datalist typeahead
- ✅ `/api/tickers?q=` — JSON typeahead for progressive enhancement
- ✅ `/archive` — reverse-chron event index
- ✅ `/live` — static snapshot + EventSource SSE (`/live/events`); JS-off baseline works; JS-on prepends new cards on each graph refresh

**P4 — Polish & honesty. 🔄 In progress.**
- ✅ `/about` — masthead/colophon with signal definitions and base rates
- ✅ ARCHIVE badge + "Indexed DATE" byline for historical filings (date provenance fix)
- ✅ RSS feed — `/feed.xml` (front page) + `/section/{slug}/feed.xml` (per desk); `<link rel="alternate">` in front-page `<head>`
- ❌ Corrections box wired to rule-version changes — not yet built
- ✅ Succession watch rail — front-page sidebar showing up to 5 `director_decay` directors with approval % and link to person dossier
- ❌ Accessibility pass (semantic headings, contrast audit) — not yet done

---

## 10. Acceptance criteria ("done" for v1)

1. `/` leads with the highest-severity live signal, not the newest document, and shows a ticker tape, sections rail, and most-watched rail.
2. All thirteen governance signal types render with a headline, deck, dateline, and byline, routed to the correct section.
3. Every company with data has a reachable `/company/{ticker}` desk; every tracked director has a `/person/{canonical_id}` dossier with vote history and board interlinks.
4. Every ticker and director name shown anywhere is a link to its page.
5. Press releases are visually distinct from SEC filings everywhere they appear.
6. Search returns companies, people, stories, and filings for a plain-text query.
7. The raw record behind any story is reachable in one click via `/doc/` or `/archive`.
8. No new third-party dependency appears in `go.mod`; the site is still one binary started the same way.
9. Every page renders without JavaScript; `/live` is the only JS, and it's additive.
10. `go test ./...` passes, including render/golden tests for headline generation and the front-page editor.

---

## 11. Out of scope / future seams

- **WordPress / any CMS** — explicitly excluded. This is a generated publication over an event store, not an editorial tool.
- **SQLite-backed read models & full-text search** — the migration seam is already marked in `signalindex` (`TODO(scale)`). Adopt it only when startup scan latency or search cost demands it; the `docindex`/`graphread` interfaces should make the swap mechanical.
- **Auth, accounts, comments, write paths** — none. Read-only.
- **Real-time charts / heavy client JS** — no. Inline SVG sparklines cover the one visualization we need.
- **De-duplication / entity resolution beyond current canonicalization** — consume what the entity graph already canonicalizes; don't reinvent it here.

---

## 12. Risks & notes

- **Startup scan cost.** Building read models by scanning the whole store grows with the store. Mitigate with cursor-based incremental refresh from day one (the pipeline already does this); the SQLite seam is the escape hatch.
- **Headline quality.** Generated headlines can read awkwardly on edge cases. Keep generation table-driven and golden-tested so tuning is safe and localized.
- **Signal expiry.** Respect `valid_through`; an expired `activist_risk` must not headline the front page weeks later. Ranking filters on it.
- **Source honesty.** Press releases are issuer-authored. Labelling them clearly is non-negotiable — mislabeling marketing as disclosure is the one failure mode that damages trust.
- **Not advice.** The masthead must say, plainly, that signals are model-derived and not investment advice.

---

*The data is already here. This document is about finally putting it on the front page.*
