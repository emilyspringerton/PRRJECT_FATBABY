# FOLLOW-UP: Ticker Pages & Ticker Search

## A focused build spec extending the Newssite North Star

**Status**: Implementation Spec (follow-up to `newssite-northstar.md`)
**Version**: 1.0
**Date**: 30 May 2026
**Scope**: `/ticker/{symbol}`, `/tickers`, ticker search (`/search`, masthead box, `/api/tickers`)
**Depends on**: North Star §5.3 (Company Desk) and §5.8 (Search), which this document supersedes for tickers.

---

## 0. Why a follow-up

The north star sketched a `/company/{ticker}` desk and a general `/search`. Two things became clear once the data was mapped:

1. **There are no company names in the system.** Nothing in the event store or the entity graph carries an issuer's legal or trade name — every surface is keyed on the ticker (`signalindex` literally keys its maps by `normalizeTicker`, and `SourceDocument`, `FilingAppearance`, and `AuditorRecord` all carry only `ticker`). Calling the page a "company desk" promises data we don't have. **The honest, canonical unit is the ticker.** This document renames the page accordingly and makes the ticker the spine of both features.
2. **Search and the ticker page share one backbone.** Both need the same thing: a single in-memory catalog of every ticker we cover, with a per-ticker rollup. Build it once; the page reads it, search ranks over it. Speccing them together avoids building the catalog twice.

**Naming reconciliation:** the canonical route becomes `/ticker/{symbol}`. `/company/{ticker}` from the north star is kept as a permanent 301 redirect to `/ticker/{symbol}` so nothing that already linked the old shape breaks.

---

## 1. The Ticker Catalog (shared backbone)

A single read model that merges the three ticker-bearing sources into one row per symbol. This is the new load-bearing structure; everything else in this document reads from it.

### 1.1 Sources merged

| Source | Read via | Contributes |
|---|---|---|
| Pipeline signals | `signalindex.Index.Summary()` / `ForTicker` | signal_count, latest_signal, latest_type, per-signal detail |
| Governance signals | `graphread` over `var/entity-graph/signals.ndjson` | severity, entity, interpretation per ticker |
| Source documents | `docindex` over `source_document_persisted` | doc_count, latest filing/PR, forms seen |
| Directors | `graphread` reverse index over `nodes.ndjson` | director_count, friction directors |
| Auditor | `graphread` over `auditors.ndjson` | current auditor + change flag |

A ticker may exist in **some but not all** sources — e.g. a filing is persisted before any signal fires. The catalog must include a symbol if it appears in *any* source, and the page and search must degrade gracefully when a section is empty.

### 1.2 Catalog row (type sketch)

```go
// internal/newssite/catalog
type TickerRow struct {
    Symbol        string    // canonical, upper-cased
    SignalCount   int       // pipeline signals
    GovSignals    int       // entity-graph governance signals
    DocCount      int       // source documents (filings + PRs)
    DirectorCount int       // distinct directors seen
    Auditor       string    // current auditor, "" if unknown
    MaxSeverity   string    // highest live governance severity: critical|high|medium|low|""
    LatestActivity time.Time // max timestamp across all sources
    Forms         []string  // distinct SEC forms seen, e.g. ["8-K","DEF 14A"]
}

type Catalog struct {
    rows map[string]*TickerRow // keyed by Symbol
    // sorted slices cached for directory + search ranking, rebuilt on refresh
}
```

### 1.3 Construction & refresh

Mirror the existing `signalindex.Build` + `signalindex.Tail` idiom exactly — full scan at startup, incremental cursor-based refresh on a ticker. No new dependency, no disk in the hot path.

- `Build(ctx, …)` — full pass: pull `signalindex.Summary()`, scan `docindex`, load the entity-graph NDJSON files, fold into `rows`.
- `Refresh()` on a timer — re-fold the deltas the underlying indexes have already absorbed. The catalog is derived state, so refresh is just a re-roll over the source indexes' current contents (cheap: one row per ticker, hundreds not millions).
- All symbol lookups go through `normalizeTicker` (`strings.ToUpper(strings.TrimSpace(...))`) so `schw`, `SCHW`, and ` schw ` resolve identically.

### 1.4 Reverse index we must add

The entity graph stores directors as `PersonNode` with a `Filings []FilingAppearance` slice — there is **no ticker → directors lookup today**. `graphread` builds that reverse index once at load:

```go
directorsByTicker map[string][]*PersonNode  // symbol -> directors who appear in its filings
auditorByTicker   map[string]AuditorRecord
edgesByTicker     map[string][]*Edge        // via Edge.Companies[]
```

This is the only genuinely new indexing work; the rest is folding data the existing indexes already hold.

---

## 2. Ticker Pages

### 2.1 Route

```
/ticker/{symbol}            canonical
/company/{symbol}           301 -> /ticker/{symbol}   (north-star compatibility)
```

`{symbol}` is normalized before lookup. Unknown symbol → see §2.4.

### 2.2 Page layout (top to bottom)

Reuses the north-star Company Desk composition, now bound to concrete joins:

1. **Masthead line.** `SCHW` · current auditor (or "auditor: unknown") · last activity date · forms seen. No company name — the ticker *is* the title.
2. **Lead.** The ticker's highest-severity *live* governance signal (filter on `valid_through >= today`), rendered with the §6 headline/deck/dateline from the north star. If no live governance signal, fall back to the latest pipeline signal; if none, to the latest filing.
3. **Signal stream.** Merge `signalindex.ForTicker(symbol)` (pipeline signals) with the ticker's entity-graph governance signals, newest first, each linking to its section. Label which desk produced each (byline).
4. **The board (sidebar).** `directorsByTicker[symbol]`, sorted by latest approval % ascending (friction first). Each director: name → `/person/{canonical_id}`, latest approval %, a friction flag if below threshold.
5. **Auditor box.** Current auditor and, if `graphread` saw a change, the prior firm and change date.
6. **Filings & wire.** Source documents for the symbol from `docindex`, SEC filings and press releases visually separated (per north-star honesty rule), each → `/doc/{identity}`.
7. **Fact box.** From the catalog row: total signals, critical/high count, distinct directors, doc count, first/last seen.

### 2.3 Empty / partial states

- Known ticker, no signals yet: show filings + directors + "No signals generated yet for SYMBOL." Do not 404 — we cover it, we just haven't scored it.
- Known ticker, only signals, no entity-graph data: omit the board/auditor sidebar cleanly.

### 2.4 Unknown ticker

Return `404` with a helpful body, not a bare Not Found: "We don't cover **XYZ**." + the masthead search box pre-filled + up to 5 nearest catalog matches (prefix, then substring). This turns a dead end into a search.

### 2.5 The directory (`/tickers`)

The classic "browse all / sitemap" trope and the natural companion to search. A single page listing every catalog symbol, default-sorted by activity (signal count then severity — the order `Summary()` already returns), with a plain alpha toggle (`?sort=alpha`). Each row: symbol → `/ticker/{symbol}`, signal count, max severity dot, last-activity date. Doubles as the human-navigable index and as crawlable link surface for the front page's RSS/sitemap.

---

## 3. Ticker Search

Three entry points over one ranking function. All work without JavaScript; the typeahead is progressive enhancement only.

### 3.1 The masthead box (every page)

A `<form method="get" action="/search">` with a single `q` input, present in the site header on every page. Baseline behaviour is a normal form submit — zero JS. The input also carries a `list=` pointing at a server-rendered `<datalist>` of known symbols, giving native browser autocomplete with **no JavaScript at all**. (Datalist is the cheapest possible typeahead and the correct default.)

### 3.2 The results page (`/search?q=`)

Server-rendered, in-memory, no search server. Two behaviours:

- **Exact resolution → redirect.** If `normalizeTicker(q)` is a known symbol, `302` straight to `/ticker/{symbol}`. This is the "I typed SCHW, take me there" path — the single most common query should never show an interstitial list.
- **Otherwise → ranked results.** Render matches grouped as **Tickers** first (the focus of this document), then optionally People and Stories (deferred to north-star §5.8 search; this spec only requires the Tickers group). Each ticker result: symbol, signal count, max severity, last activity → `/ticker/{symbol}`. Empty query → show the `/tickers` directory inline. No matches → "No tickers match *q*" + nearest substring suggestions.

### 3.3 The typeahead endpoint (`/api/tickers?q=`)

JSON, for progressive enhancement only. Returns ranked matches for live dropdown results when JS is enabled. Contract:

```
GET /api/tickers?q=sc&limit=8
200 {
  "query": "SC",
  "count": 2,
  "tickers": [
    {"symbol":"SCHW","signal_count":14,"max_severity":"high","latest_activity":"2026-05-21T00:00:00Z"},
    {"symbol":"SCCO","signal_count":2,"max_severity":"low","latest_activity":"2026-05-12T00:00:00Z"}
  ]
}
```

Mirror the `signalapi` error/JSON conventions (normalized symbol echoed back, `count`, list). `limit` defaults to 8, capped. Unknown/empty `q` → `200` with `count: 0`, never a 4xx (typeahead must stay quiet).

### 3.4 Ranking

Over the catalog's symbol set, with `normalizeTicker(q)` as the needle:

1. **Exact** symbol match (handled by the redirect in §3.2; in the API it ranks first).
2. **Prefix** match (`SC` → `SCHW`, `SCCO`).
3. **Substring** match (`HW` → `SCHW`).
4. Tie-break within a tier by **activity**: signal_count desc, then max_severity, then symbol asc.

A plain linear scan over a few hundred–few thousand symbols is more than fast enough; keep it linear and note the seam.

### 3.5 Edge cases (call these out in tests)

- **Case & whitespace:** everything goes through `normalizeTicker`. `schw` == `SCHW`.
- **Substring collisions:** short queries like `C` must still prefer exact (`C` if covered) over the flood of substring hits; tier ordering handles this.
- **Dotted / class tickers:** `BRK.B` must survive normalization unchanged (don't strip punctuation in the normalizer).
- **Known-but-unscored tickers:** they appear in search and have a page even with zero signals (catalog includes filing-only symbols).
- **Empty `q`:** results page shows the directory; API returns `count: 0`.

---

## 4. Architecture notes (unchanged philosophy)

- **No new dependency.** Catalog + reverse indexes are plain maps built from the existing stores via the `Build`/`Tail` idiom. `html/template` renders the pages; `encoding/json` serves `/api/tickers`.
- **One binary, same startup.** New read models constructed in `cmd/newssite/main.go` alongside the existing ones; refresh goroutines started the same way; routes mounted on the same `ServeMux`.
- **Hot path touches no disk.** Pages and the API read in-memory maps under an `RWMutex`, exactly like `signalindex`.
- **Migration seam preserved.** When symbol counts or substring search get expensive, the catalog interface is the swap point for a SQLite-backed table + `LIKE`/FTS — the same `TODO(scale)` seam the north star already inherits from `signalindex`. Not now.
- **Search is a function of the catalog, not a service.** No separate index process; ranking is a pure function over the in-memory symbol set, trivially unit-testable.

New packages introduced by this spec:

| Package | Responsibility |
|---|---|
| `internal/newssite/catalog` | merge sources into `TickerRow`s; expose lookup, directory ordering, and search ranking |
| `internal/newssite/graphread` | (from north star) now also builds `directorsByTicker` / `auditorByTicker` / `edgesByTicker` reverse indexes |

---

## 5. Build phases

**T1 — Catalog + ticker page.**
Build `catalog` and the `graphread` reverse indexes. Ship `/ticker/{symbol}` with the full layout, the `/company/{ticker}` 301, and the unknown-ticker 404-with-suggestions. *Done when:* every symbol returned by `signalindex.Summary()` has a working page reachable by URL.

**T2 — Search page + exact-match redirect + directory.**
Ship `/search?q=` (redirect on exact, ranked Tickers group otherwise), the masthead form with the `<datalist>`, and `/tickers`. *Done when:* typing a known symbol anywhere lands on its page in one submit, with zero JavaScript.

**T3 — Typeahead progressive enhancement.**
Ship `/api/tickers?q=` and a ~20-line `fetch`-based dropdown that enhances the masthead box when JS is on. *Done when:* the box shows live ranked results with JS and still fully works without it.

**T4 — Polish.**
Per-ticker RSS (`/ticker/{symbol}/feed.xml`) reusing the north-star feed machinery; severity dots and last-activity on directory/search rows; alpha/active sort toggle on `/tickers`; cross-links so every ticker mention anywhere on the site routes to `/ticker/{symbol}`.

---

## 6. Acceptance criteria

1. `/ticker/{symbol}` resolves for **every** symbol present in any source (signals, governance signals, or filings), not just scored ones.
2. `/company/{ticker}` permanently redirects (301) to `/ticker/{symbol}`.
3. Symbol lookups are case- and whitespace-insensitive; `schw`, `SCHW`, and ` SCHW ` reach the same page.
4. Searching an exact known symbol redirects (302) straight to its ticker page — no interstitial list.
5. Searching a partial string returns prefix matches ahead of substring matches, tie-broken by activity.
6. An unknown ticker page returns 404 with nearest-match suggestions and the search box, never a bare Not Found.
7. The masthead search box and `/search` work with JavaScript disabled; `/api/tickers` is the only JS-dependent path and is purely additive.
8. `/tickers` lists every covered symbol, sortable by activity and alphabetically.
9. No new third-party dependency in `go.mod`; same single binary, same startup command.
10. `go test ./...` passes, including: ranking-order tests (exact > prefix > substring), normalization tests (case/whitespace/dotted tickers), and a catalog-merge test proving a filing-only ticker still gets a page.

---

## 7. Open questions / explicitly deferred

- **People & story search** beyond tickers — deferred to north-star §5.8; this spec delivers only the Tickers group.
- **Fuzzy / typo-tolerant matching** (`SCWB` → `SCHW`) — out of scope for v1; substring + prefix is enough. Revisit with the SQLite/FTS migration.
- **Company display names** — we have none. If a ticker→name map is ever added (e.g. from EDGAR submission metadata already fetched by `secwatch`), it slots into `TickerRow.Name` and the page title without changing routes. Worth a small follow-up, but not blocking.

---

*Tickers are the only thing the data agrees on. So that's what we build the pages and the search around.*
