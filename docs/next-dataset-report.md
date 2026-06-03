# Next Dataset Report — SEC & Press Release Extraction Opportunities

**Date:** 2026-06-03  
**Context:** EINHORN_INDUSTRIAL / prrject-fatbaby signal intelligence pipeline  
**Purpose:** Identify the highest-value data sets we can scrape from SEC EDGAR and raw press releases to extend Jon Stockwell's divergence engine and the broader governance signal corpus.

---

## What We Already Extract

| Source | Data | Pipeline stage | Signal output |
|---|---|---|---|
| SEC EDGAR 8-K | Item 5.07 (proxy vote results) | entity-graph | director_friction, director_decay, governance_entrenchment, activist_risk, compensation_concern |
| SEC EDGAR SC 13D/13G | Activist ownership filings | schd13-watcher | activist_risk accuracy records |
| SEC EDGAR 8-K Item 2.02 | Used as date marker only | earnings-calendar, eps-reconciler | Confirms earnings date; EPS body not fully parsed |
| PR Newswire | Press release discovery + body | prwatch + prwatch-body | |
| PR Newswire bodies | EPS (earnings per share) | eps-processor | oracle cases → eps-reconciler |
| PR Newswire bodies | Forward guidance | guidance-watcher | raises / lowers / maintains |

**Current gap:** 50-ticker watchlist. All 8-K subtypes flow in but only Item 5.07 (proxy) and Item 2.02 (as a date) are structurally parsed. Form 4, Item 5.02, and most press release event categories are untouched.

---

## Tier 1 — Build Next

### 1. Form 4 — Insider Transactions (SEC EDGAR)

**What it is:** Every purchase, sale, and option exercise by officers, directors, and >10% holders. Filed within 2 business days.

**Why it matters:** This is the #1 missing signal for Jon's divergence engine. Jon's doc explicitly lists "insider transaction activity vs analyst consensus" as a core divergence source. An officer buying 50k shares while the stock is flat is a setup. A CEO dumping in an open window before a downward revision is the other side.

**How to scrape:**
- Add `"4"` to `allowed_forms` in `config/watchlist.json` for each ticker — secwatch already handles the form routing
- Form 4 XML is machine-readable and stable: `<transactionDate>`, `<transactionCode>` (P=open-market buy, S=open-market sell, A=award, M=option exercise), `<transactionShares>`, `<transactionPricePerShare>`, `<securityTitle>`
- Net out awards (A, M) from open-market transactions (P, S) — awards are compensation noise, P/S are insider conviction signals
- Aggregate by role (officer vs director) and window (30/90 days)

**Signals to emit:** `insider_buy` (open-market purchase, especially CEO/CFO), `insider_sell_cluster` (≥3 officers selling within 30 days), `insider_net_buy_ratio` (buys/(buys+sells) over trailing 90 days)

**Implementation:** New `cmd/form4-watcher` reading `filing_discovered` events where `form_type == "4"`. Fetches Form 4 XML primary document. Writes to `var/form4`. New signal type in entity-graph consumes it. Mirrors the schd13-watcher pattern exactly.

**Parsing complexity:** Low. XML schema is stable and well-documented. No NLP.

---

### 2. 8-K Item 5.02 — Leadership Departure/Appointment (SEC EDGAR)

**What it is:** 8-K filed within 4 business days when a director or principal officer (CEO, CFO, COO, General Counsel) departs or is appointed. Item 5.02 is the mandatory trigger.

**Why it matters:** Leadership changes are structural governance events. CFO departure within 6 months of a guidance raise is a red flag. New CEO from an activist-adjacent background signals board capitulation — a potential setup. These filings already flow through secwatch → processor into `source_document_persisted`. They just need a parser.

**How to scrape:** No new watcher needed. Add `ParseItem502(text string)` to `internal/entitygraph/parser.go` alongside the existing `ParseItem507`. Extract:
- `departure_type`: resignation / retirement / termination (text classification)
- `role`: CEO / CFO / COO / General Counsel / Director
- `person_name`
- `replacement_named` bool (whether a successor was simultaneously announced)

**Signals to emit:** `leadership_departure` (role + involuntary/voluntary flag), `leadership_appointment`, `cfo_departure` (elevated severity — CFO departures precede restatements at a higher base rate)

**Parsing complexity:** Low-medium. Regex + simple NLP for departure type. The "retirement" vs "termination" disambiguation is the hard part (companies soften language deliberately).

---

## Tier 2 — High Value, More Complexity

### 3. PR Newswire — Dividend Announcements

**What it is:** Press releases announcing regular quarterly dividends, increases, cuts, suspensions, and special/extraordinary dividends.

**Why it matters:** A dividend cut is one of the highest-urgency bearish signals — often paired with deteriorating governance signals, precedes significant price dislocation. A special dividend signals management confidence in cash generation and can compress options IV after announcement.

**How to scrape:** Already fetching bodies via prwatch-body. New `cmd/dividend-watcher` reading `pr_body_fetched` events:
- Keyword filter: "declares", "quarterly dividend", "cash dividend", "per share", "suspended dividend"
- Extract: `event_type` (regular/increase/decrease/suspension/special), `amount_per_share`, `record_date`, `payment_date`
- Compare to prior quarter for increase/decrease magnitude

**Signals to emit:** `dividend_cut` (high severity), `dividend_raise` (bullish), `special_dividend` (one-time capital return)

**Jon integration:** Dividend cut + `governance_entrenchment` on same ticker = short setup. Dividend raise while governance signals decline = compression trade candidate.

**Parsing complexity:** Medium. Regular vs special distinction requires context. Amount extraction with regex is reliable. Percentage-change calculation requires a prior-period cache.

---

### 4. PR Newswire — Share Buyback Announcements

**What it is:** Press releases announcing new repurchase authorizations, completion of existing programs, and suspension of repurchases.

**Why it matters:** New buyback authorization = float reduction + management conviction signal. Suspension of an existing program = quiet negative, often underreported.

**How to scrape:** Same `pr_body_fetched` pipeline. Keywords: "share repurchase", "buyback", "repurchase program", "authorized to repurchase", "completed its repurchase". Extract: `authorization_amount_usd`, `program_status` (new/completed/suspended).

**Signals to emit:** `buyback_authorization` (with size), `buyback_suspension`

**Parsing complexity:** Medium. Dollar amounts reliable. Market-cap percentage requires price feed (Jon's market data stub covers this path once wired).

---

### 5. 8-K Item 2.02 — Earnings Release Full Extraction (SEC EDGAR)

**What it is:** The filed earnings release. Currently used only to confirm EPS oracle dates. The actual financial data — segment revenue, beat/miss vs prior guidance, GAAP/non-GAAP reconciliation — is not extracted.

**Why it matters:** Closes the full loop: press release EPS → oracle case → filed 8-K confirmation → beat/miss magnitude → guidance revision at filing (delta between PR and 8-K is itself a signal).

**What to add:** Extend eps-reconciler to also extract: `revenue_actual`, `revenue_yoy_pct`, `beat_miss` classification vs the oracle case, `guidance_revised_at_filing` flag (company changed guidance in 8-K vs prior PR).

**Signals to emit:** `earnings_beat` / `earnings_miss` with magnitude, `guidance_revision_at_filing`

**Parsing complexity:** High. Revenue tables in 8-K text are inconsistently formatted. Guidance revision detection requires comparing current text against a prior-period snapshot.

---

## Tier 3 — Useful, Lower Priority

### 6. 13F — Institutional Holdings (SEC EDGAR)

Quarterly snapshot of positions held by managers with ≥$100M AUM. Filed within 45 days of quarter-end. The 45-day lag is a significant drawback. Most useful for: detecting new institutional entrants at the same time as governance deterioration signals (possible fresh activist build), or confirming that an activist who filed a 13D is now exiting via declining 13F position.

**Implementation:** EDGAR EFTS already used for 13D/13G. Same pattern. Parse XML information table, diff against prior quarter. High volume — scope to watchlist tickers only.

---

### 7. NT 10-K / NT 10-Q — Late Filing Notifications (SEC EDGAR)

When a company cannot file its annual or quarterly report on time, it files an NT. These are quiet red flags. Common reasons: auditor disputes, internal control weaknesses, restatements, material weakness identification. These precede bad news by 30–60 days on average.

**Implementation:** Add `"NT 10-K"` and `"NT 10-Q"` to `allowed_forms` in watchlist.json. Simple keyword classification of reason text: `restatement`, `material_weakness`, `auditor_dispute`, `system_transition`.

**Signals to emit:** `late_filing` with reason classification. Severity: high for restatement/material-weakness, warn otherwise.

---

## Summary Table

| Dataset | Source | Parsing | Infrastructure | Jon value | Build order |
|---|---|---|---|---|---|
| **Form 4** (insider transactions) | SEC EDGAR | Low (XML) | New `cmd/form4-watcher` | Critical | 1 |
| **8-K Item 5.02** (leadership) | SEC EDGAR | Low-medium | Extend entity-graph | High | 2 |
| **Dividend PRs** | PR Newswire | Medium | New `cmd/dividend-watcher` | High | 3 |
| **Buyback PRs** | PR Newswire | Medium | Extend dividend-watcher | High | 4 |
| **8-K Item 2.02** (earnings full) | SEC EDGAR | High | Extend eps-reconciler | Medium | 5 |
| **13F** (institutional holdings) | SEC EDGAR | Low (XML) | New cmd, high volume | Medium | 6 |
| **NT 10-K/Q** (late filings) | SEC EDGAR | Low | Extend watchlist + parser | Medium | 7 |

---

## Recommendation

**Build Form 4 first.** Highest signal, lowest parsing complexity. The XML schema is stable and machine-readable — no NLP. The `cmd/form4-watcher` pattern is nearly identical to `cmd/schd13-watcher` (same EFTS approach, same event store, different form type and document format). It directly closes the biggest gap in Jon's divergence engine.

**Then 8-K Item 5.02** via the existing entity-graph parser — zero new infrastructure, just a new `ParseItem502` function and one new signal type.

**Then dividend-watcher** — dividend cuts are the clearest short-setup catalyst in the corpus and can be extracted from existing prwatch-body data with modest new parsing.
