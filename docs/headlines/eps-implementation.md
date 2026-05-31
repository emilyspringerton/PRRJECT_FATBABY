# NORTHSTAR: EPS Headlines Feed
## Automated earnings-headline generation from earnings releases

**Status**: Operational — extraction, validation, article generation, oracle reconciliation all running
**Version**: 2.0 (updated from v1.0)
**Date**: May 29, 2026 · updated May 31, 2026
**Architecture**: FATBABY-native — `processor` → `intelligence` → `eps` extraction → article generation → `feedserver` → `newssite`
**First use case**: GAAP diluted EPS headlines from quarterly/annual earnings releases

---

## EXECUTIVE INTENT

Generate fast, accurate, attributed earnings headlines the moment an earnings release lands on the feed: **"{Issuer} reports {period} EPS of ${x}"**, with a full-numbers story behind it. Headline the GAAP figure (the market-news standard when GAAP is reported), carry the full breakdown — GAAP, adjusted, segment, YoY, guidance — in the body.

This is accurate financial reporting at machine speed. The source of truth is filed, audited, mandatory-disclosure data, not a marketing narrative. The discipline that matters here is **correctness**: an earnings headline that inverts a beat/miss or grabs the prior-year number is worse than no headline. Every guardrail below exists to make the number right, not to soften it.

**Editorial policy (fixed):**
- Headline = **GAAP diluted EPS** when GAAP is reported. ("F reports $X EPS" means GAAP, per convention.)
- Full numbers (GAAP, adjusted/non-GAAP, basic vs diluted, segments, YoY, guidance) live in the body.
- Every figure traces to the release. Every article links the original.

---

## PIPELINE

```
Earnings release ──► processor.FetchClean
                          │
                          ▼
            intelligence.SourceDocument {source_type:"press_release"}
                          │
                          ▼
              eps.Extract  ── pull structured EarningsData from cleaned text
                          │
                          ▼
              eps.Validate ── reconcile, sanity-check, cross-check vs filed 8-K
                          │
                          ▼
            article.Generate ── GAAP headline + dek + full-numbers body
                          │
                          ▼
              feedserver.Publish ──► newssite.Render
```

You already persist `SourceDocument` for press releases (URL + cleaned text). The `EarningsData` and `Article` are derived artifacts that point back to it, so the chain from headline → extracted number → original release stays intact and auditable.

---

## THE HARD PART: EPS EXTRACTION

This is where the real engineering is. Companies format earnings releases inconsistently; the extractor has to find the *right* number, not *a* number.

### EarningsData schema

```json
{
  "source_identity": "string",        // -> SourceDocument.Identity
  "issuer": "string",
  "ticker": "string|null",
  "period": {
    "fiscal_quarter": "Q1|Q2|Q3|Q4|FY",
    "fiscal_year": 2026,
    "period_end": "2026-03-31",
    "is_fiscal_calendar_aligned": true  // false for non-calendar fiscal years
  },
  "eps_gaap_diluted": "number|null",    // THE headline number
  "eps_gaap_basic": "number|null",
  "eps_adjusted": "number|null",        // non-GAAP; body only; label required
  "eps_basis": "total|continuing_ops",  // which net figure the EPS is on
  "currency": "USD",
  "net_income": "number|null",
  "shares_diluted": "number|null",
  "revenue": "number|null",
  "yoy_eps": "number|null",             // prior-year same period, for YoY
  "consensus_eps": "number|null",       // body; only with a licensed source
  "surprise_pct": "number|null",
  "guidance": "string|null",
  "extraction_confidence": 0.0,
  "extraction_flags": ["..."]
}
```

### Extraction traps (each must be handled, each is checkable)

1. **Diluted vs basic.** Headline convention is **diluted** GAAP EPS. Extract both; headline uses diluted.
2. **Total vs continuing operations.** Releases often report EPS from continuing ops separately from total. Record which basis (`eps_basis`) and be consistent. Convention leans to total net unless only continuing is given — flag when both exist and they differ.
3. **Prior-period contamination.** The current number sits next to last year's and last quarter's. The single most dangerous error is grabbing the comparison figure as the current one. Anchor extraction to the stated current period; cross-check against `period.period_end`.
4. **Sign / loss handling.** Negatives appear as `(0.42)`, `-0.42`, or "loss of $0.42". Normalize to a signed number. A flipped sign is a beat/miss inversion — the worst failure mode.
5. **GAAP vs adjusted labeling.** Companies lead with whichever flatters. The extractor must label which line is GAAP and which is non-GAAP/"adjusted"/"core"/"pro forma" and never let an adjusted number land in `eps_gaap_diluted`.
6. **Currency & fiscal calendar.** Non-USD reporters; non-calendar fiscal years (a "Q2" ending in October). Capture `period_end` explicitly rather than inferring from quarter label.
7. **Adjusted-only releases.** Some companies report no GAAP EPS. Then `eps_gaap_diluted` is null; the headline uses the reported figure and the dek/body marks it non-GAAP. Never headline a GAAP number that isn't there.

---

## VALIDATION (the correctness gates)

Before an article publishes, `EarningsData` runs gates. Fail → don't publish the headline; route to a review queue or fall back to a bare-print text card.

- **Trace gate:** every number in the headline appears in the release `CleanedText`. No figure is published that can't be located in the source.
- **Reconciliation sanity check:** if `net_income` and `shares_diluted` are both present, `net_income / shares_diluted` should approximate `eps_gaap_diluted` within tolerance. Mismatch → flag.
- **Period gate:** extracted `period_end` must be consistent with the period label and must not equal a prior-period date present in the release.
- **Sign gate:** loss language in the text must agree with a negative EPS sign.
- **Cross-source check (high-value, you already have it):** FATBABY already ingests 8-Ks. When the same quarter's 8-K/Item 2.02 is available, reconcile the press-release EPS against the filed figure. Agreement is strong confidence; disagreement holds the headline. This is a real edge — most newswires don't have the filing graph you do.
- **Confidence threshold:** below threshold → review queue, not auto-publish.

These gates are the product. Speed is worthless if the beat/miss is wrong.

---

## ARTICLE GENERATION

### Headline (GAAP, fills only from populated fields)

- Standard: `{Issuer} reports {period} EPS of ${eps_gaap_diluted}`
- Loss: `{Issuer} reports {period} loss of ${|eps|} per share`
- Adjusted-only: `{Issuer} reports {period} adjusted EPS of ${eps_adjusted}` (basis marked)
- With consensus (if licensed source present): append ` vs ${consensus} expected` and set beat/miss in the dek.

### Body (full numbers, attributed)

GAAP diluted and basic, adjusted/non-GAAP (labeled as the issuer's measure), revenue, YoY, segment detail if present, guidance, and verbatim attributed quotes. Promotional/forward-looking statements framed as the issuer's ("the company expects"), never as fact.

### Generator rules

- Use only facts in the release; invent nothing.
- GAAP diluted is the headline number when present; label it.
- Attribute non-GAAP and forward-looking statements to the issuer.
- Quotes verbatim, attributed to a named speaker.
- Missing `source_url` → `{"publish": false}`.

---

## LABELING & PROVENANCE

In `newssite` (extends existing `sourceTypeLabel`):
- Badge: **Earnings · {issuer}** + **AI-generated** tag.
- "Read the original release →" link on every article (`source_url`).
- Detail page can show the original cleaned text and the extracted `EarningsData` alongside the article — the reader (or you) can audit the headline against the source in one view.

Open/auditable by construction: the headline number, the extracted data, and the issuer's own release are all reachable from the article.

---

## IMPLEMENTATION STATUS

| Phase | Status | What was built |
|---|---|---|
| Phase 0 — Extractor | ✅ Done | `internal/eps/extract.go` + `internal/eps/validate.go` — GAAP diluted EPS extraction with confidence scoring and 8 validation flags |
| Phase 1 — Headlines | ✅ Done | `internal/eps/article.go` `Generate()` — headline + dek + body; `cmd/eps-processor` — runs continuously against prwatch-body event store; `cmd/newssite` now serves `/section/earnings` desk and surfaces EPS articles on front page and ticker pages |
| Phase 2 — Oracle reconciliation | ✅ Done | `internal/eps/oracle.go` + `cmd/eps-reconciler` — scans secwatch event store for 8-K Item 2.02, matches against oracle cases, writes `confirmed`/`contradicts`/`pending` verdicts to `var/eps/oracle.ndjson` |
| Phase 3 — Consensus data | ❌ Not yet | Gated on licensed estimate source |
| Phase 4 — Guidance & segments | ❌ Not yet | Richer body; guidance-change detection |

### Data produced

| File | Contents |
|---|---|
| `var/eps/articles.ndjson` | Published EPS articles (one per qualifying press release) |
| `var/eps/oracle.ndjson` | Oracle cases: extracted EPS + filed EPS + verdict |

### Newssite integration (as of May 31, 2026)

- `/section/earnings` — Earnings desk: all EPS articles sorted by publish date
- Front page sidebar: up to 4 latest EPS articles with GAAP EPS amount
- Ticker page: EPS articles for that ticker with period and EPS fact
- Sections rail: "Earnings" link on every page
- `cmd/newssite -eps-dir` flag (default `var/eps`)

## OPEN QUESTIONS (remaining)

- Consensus data source and licensing terms.
- Review-queue threshold — how conservative before launch confidence is established.
- Guidance: headline-eligible, or body-only?
- Dedup when the same earnings release crosses multiple wires.

---

## SUCCESS METRICS

- **Extraction accuracy:** % of releases where `eps_gaap_diluted`, period, and sign are correct vs hand-labeled truth. **Target this hard in Phase 0 — everything downstream depends on it.**
- **Sign/beat-miss error rate:** must approach zero. This is the metric that matters most.
- **Trace integrity:** 100% of published numbers locatable in the source.
- **Cross-source agreement:** % of headlines reconciled against the filed 8-K.
- **Latency:** release-landed → headline-published.
- **Auto-publish rate:** % clearing confidence gates without review (efficiency, secondary to accuracy).

---

## OPEN QUESTIONS

- Total-net vs continuing-ops EPS as the default headline basis when both are reported.
- Consensus data source and its licensing terms (in or out for v1).
- Review-queue threshold — how conservative before launch confidence is established.
- Guidance: headline-eligible, or body-only?
- Dedup when the same earnings release crosses multiple wires.

---

## ONE-LINE NORTH STAR

GAAP EPS headlines at machine speed, every number traceable to the filed release — fast because it's automated, trusted because it's right and it shows its source.

