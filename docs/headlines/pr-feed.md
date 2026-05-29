# Press Release → Article Feed

**Status:** Draft v0.1
**Date:** May 29, 2026
**Fits into:** `processor` (ingest/clean) → `intelligence` (source doc) → article generation → `feedserver` (publish) → `newssite` (render)

A system that turns the press-release feed into clean, readable, attributed articles and publishes them to a text feed. Sharp headlines, fast turnaround, every item labeled as press-release-derived and linked to its original.

---

## 1. Pipeline

```
PR feed ──► processor.FetchClean ──► intelligence.SourceDocument {source_type:"press_release"}
                                              │
                                              ▼
                                     Article generator (LLM)
                                              │
                                              ▼
                                     Article {headline, body, attribution, provenance}
                                              │
                                              ▼
                                     feedserver publish ──► newssite render
```

You already persist `SourceDocument` for press releases — the article is a derived artifact that points back to it.

## 2. Article schema

```json
{
  "article_id": "string",
  "source_identity": "string",      // -> SourceDocument.Identity
  "issuer": "string",               // the company that issued the release
  "ticker": "string|null",
  "headline": "string",
  "dek": "string",                  // one-line standfirst
  "body": "string",                 // attributed summary/article
  "label": "press_release",         // drives the feed badge
  "ai_generated": true,
  "source_url": "string",           // original release, REQUIRED
  "published_at": "timestamp",
  "issuer_quotes": [                 // pulled verbatim, attributed
    { "speaker": "string", "text": "string" }
  ]
}
```

**Publish gate:** no `source_url` → don't publish. Same render-gate discipline as the presenter.

## 3. Generation rules

The generator turns a `SourceDocument` into an article. Rules:

- **Attribute, don't assert.** Claims from the release are framed as the issuer's: "Acme announced…", "according to the release", "the company said". The article never states a promotional claim as independent fact.
- **Headline is accurate, not hyped.** It describes what was announced. No clickbait, no implication of significance the release doesn't support.
- **No invented facts.** Every figure, name, date, and quote traces to the `CleanedText`. Quotes are verbatim and attributed to a named speaker.
- **Forward-looking / promotional language stays marked as the issuer's.** "Acme expects record growth" — never "Acme will see record growth."
- **Flag, don't bury, the PR nature.** The article reads cleanly but is honest about being a company announcement.

### Generator system prompt (starting point)

```
You write a short news-style article from ONE press release (provided as
cleaned text). Output JSON matching the Article schema.

Rules:
- Use ONLY facts present in the release text. Invent nothing.
- Attribute claims to the issuer: "the company said", "according to the
  release", "{issuer} announced". Never present a promotional claim as
  independent fact.
- Quotes must be verbatim from the text and attributed to a named speaker.
- Headline describes what was announced, accurately, no hype.
- If the source_url is missing, output {"publish": false, "reason": "no source"}.
- Output: { "publish": true, "headline","dek","body","issuer","ticker",
  "issuer_quotes":[...] }
```

## 4. Labeling & provenance (drives reader trust)

In `newssite` render — extend the existing `sourceTypeLabel`:

- Persistent badge on list + detail pages: **Press Release · {issuer}** and an **AI-generated** tag.
- "Read the original release →" link to `source_url` on every article.
- Detail page can show the original cleaned text alongside the generated article (you already store it).

Because the system is open/auditable, this is mostly surfacing what you already capture: the reader can always get from the article to the issuer's own words.

## 5. Build phases

- **Phase 0:** wire generator to existing `SourceDocument` press-release events; generate articles for a handful, eyeball quality and attribution.
- **Phase 1:** publish to `feedserver`; extend `newssite` labels + source links + original-text view.
- **Phase 2:** headline/dek quality tuning; dedup near-identical releases; issuer canonicalization (reuse the entity-graph canon work).
- **Phase 3:** optional — cross-link to FATBABY signals when a release maps to a watched ticker (clearly as two distinct things: the company's announcement vs. your signal).

## 6. Quality metrics

- **Attribution integrity:** 100% of articles frame issuer claims as the issuer's. (Spot-check + automated "asserted vs attributed" lint on promotional verbs.)
- **Source-binding:** 100% of published articles resolve to an original release.
- **Faithfulness:** sampled articles contain no fact absent from the release.
- **Quote fidelity:** quotes match source text verbatim.

## 7. Open questions

- Dedup strategy when the same release hits multiple wires.
- How much editorial framing before "summary of a release" drifts into "our reporting"? (Keep it summary-side.)
- Cadence: publish on ingest, or batch?
- Do you want issuer-claim flagging surfaced to the reader (e.g., forward-looking statements marked inline)?

