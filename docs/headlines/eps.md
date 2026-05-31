# NORTHSTAR: EPS Headlines Feed — Phased / Self-Improving

**Status**: Operational — Phases 0–2 complete; self-improvement loop active via oracle reconciliation
**Version**: 2.0
**Date**: May 29, 2026 · updated May 31, 2026
**Architecture**: FATBABY-native — `processor` → `intelligence` → `eps` → `article` → `feedserver` → `newssite`, with an async `reconciler` + the existing Emily/Claude refinement loop closing the cycle.

---

## EXECUTIVE INTENT

Generate GAAP EPS headlines the moment an earnings release crosses the wire, and make the system **measurably more accurate over time** by feeding the filed 8-K back as ground truth.

Two facts shape the whole design:
1. **The press release is the speed source.** It crosses first; the headline fires off it alone.
2. **The filed 8-K (Item 2.02) is the truth source.** It lands minutes-to-hours later, pull-only, rate-limited. It can't gate the headline, but it can *judge* it.

The gap between (1) and (2) is the engine. Every headline is a prediction; every filing that follows is a graded answer. That grading signal is what drives recursive self-improvement: the extractor learns from its own corrected mistakes against an authoritative oracle, and the eval set ratchets so it can never silently regress.

**Editorial policy (fixed):** headline GAAP diluted EPS; full numbers in the body; every figure traceable to the release; every article links the original.

---

## THE SELF-IMPROVEMENT LOOP

```
        ┌──────────────────────────────────────────────────────┐
        │                                                        │
        ▼                                                        │
  press release ─► eps.Extract ─► in-doc gates ─► PUBLISH headline
        │                                              │         │
        │                                              ▼         │
        │                                    async reconciler    │
        │                              (polls 8-K, rate-limited)  │
        │                                              │         │
        │                          ┌───────────────────┤         │
        │                     CONFIRM                CONTRADICT   │
        │                          │                   │         │
        │                   mark verified        emit correction │
        │                          │                   │         │
        │                          ▼                   ▼         │
        │                   ┌─────────────────────────────┐      │
        └───────────────────│  labeled case store (oracle) │      │
                            │  every release + extraction  │      │
                            │  + filed truth + verdict     │      │
                            └─────────────┬───────────────┘      │
                                          │                      │
                                          ▼                      │
                            growing eval / regression set        │
                                          │                      │
                                          ▼                      │
                       Emily/Claude refinement of eps.Extract ───┘
                       (every change gated against the eval set)
```

The loop's invariant: **no extractor change ships unless it improves or holds every metric on the accumulated eval set.** That's what makes it self-*improving* rather than self-*drifting*.

---

## PUBLISH vs RECONCILE (the latency split)

**Synchronous, at wire time — all self-contained in the release text:**
- *Trace gate*: every headline number appears in `CleanedText`.
- *Reconciliation sanity*: `net_income / shares_diluted ≈ eps_gaap_diluted` within tolerance (when both present).
- *Sign gate*: loss language agrees with negative sign.
- *Period gate*: extracted `period_end` matches the period label, is not a prior-period date in the release.
- *Confidence gate*: below threshold → review queue, not auto-publish.

**Asynchronous, when the 8-K lands — the oracle:**
- Poll for the matching 8-K/Item 2.02 (CIK + period). Rate-limited well under EDGAR's 10 req/s, exponential backoff on 429, prefer the daily bulk submissions index over per-company polling.
- Reconcile filed EPS against the published headline.
- Confirm → mark verified. Contradict → correction path (below). Either way → write a labeled case to the oracle store.

---

## CORRECTION PATH

Newswire discipline: publish fast, correct cleanly. When the 8-K contradicts a headline:
- Generate a correction article linked to the original, stating the corrected figure and that it now matches the filed 8-K.
- Update the feed item; preserve the original for audit (you already keep source docs).
- Log the case as a high-value training example — contradictions are the most informative inputs to the loop.

A visible, honest correction mechanism is a feature, not an embarrassment. It's also what keeps a fast feed trustworthy.

---

## PHASES

Each phase produces a feedback signal that powers the next. Phases don't just add features — they tighten the loop.

### Phase 0 — Extractor + oracle harness (make-or-break)
- **Build**: `eps.Extract` (GAAP diluted, basic, adjusted, basis, period, sign, currency) + in-doc gates. Build the labeled case store from day one.
- **Bootstrap the oracle**: run extraction over *historical* releases where the 8-K already exists, reconcile in batch (no latency pressure offline). This gives you a labeled set and a real accuracy number before a single live headline.
- **Signal produced**: baseline extraction accuracy, error taxonomy (which traps actually bite: prior-period contamination? adjusted-vs-GAAP mislabel? sign?).
- **Exit**: extraction accuracy and sign-error rate on the held-out historical set clear launch thresholds.

### Phase 1 — Live headlines + async reconciler
- **Build**: GAAP headline + dek generation; publish to `feedserver`; `newssite` labels + source link. Stand up the rate-limited `reconciler` poller.
- **Signal produced**: live confirm/contradict verdicts streaming into the oracle store; real release→headline and release→filing latency distributions.
- **Improvement mechanism**: first live cases enter the eval set; first round of Emily/Claude rule refinement against observed errors.
- **Exit**: confirm rate stable; correction path exercised end-to-end on real contradictions.

### Phase 2 — Close the loop (recursive improvement turns on)
- **Build**: the eval-gated refinement cycle. Accumulated oracle cases become the regression suite. Emily/Claude proposes extractor changes; each is scored against the full eval set; only non-regressing improvements merge.
- **Signal produced**: a monotonic accuracy curve and a shrinking error taxonomy over successive cycles.
- **Improvement mechanism**: this *is* the recursive step — the system's own graded outputs become the training signal for its next version.
- **Exit**: demonstrated multi-cycle improvement with zero sign/beat-miss regressions.

### Phase 3 — Full body + review-queue learning
- **Build**: full-numbers story (GAAP + adjusted labeled, segments, YoY, guidance, attributed quotes); route low-confidence/flagged extractions to human review.
- **Signal produced**: human review decisions — a second, high-quality label source feeding the same oracle store.
- **Improvement mechanism**: review decisions expand the eval set, especially for the hard cases the in-doc gates flag.
- **Exit**: auto-publish rate rising while accuracy holds (the loop is buying you coverage without buying error).

### Phase 4 — Consensus & guidance (gated on data licensing)
- **Build**: beat/miss vs a licensed consensus source; guidance-change detection.
- **Signal produced**: beat/miss accuracy as its own tracked metric with its own oracle check.
- **Note**: consensus data is licensed, not free — this phase is gated on a data agreement, not just engineering.

---

## METRICS THAT RATCHET

The loop's job is to push these monotonically; the eval gate forbids regressions.

- **Extraction accuracy** (vs oracle): the headline metric. Tracked per cycle.
- **Sign / beat-miss error rate**: must approach and stay near zero. The eval gate treats any regression here as a hard block.
- **Confirm rate** (headlines later confirmed by the filed 8-K): the live trust signal.
- **Correction rate** and **time-to-correction**: lower and faster over time.
- **Trace integrity**: 100% of published numbers locatable in source (invariant, not a target).
- **Auto-publish rate**: rising *only while* accuracy holds — coverage bought honestly.
- **Eval-set size**: grows every cycle; a bigger oracle is a stronger gate.

---

## GUARDRAILS ON THE LOOP

A self-improving system needs constraints so "improvement" stays honest:
- **No-regression gate**: a change that lifts overall accuracy but worsens sign error does not ship.
- **Oracle integrity**: the filed 8-K is the truth source; the system never grades itself against its own past output.
- **Human review on the hard tail**: low-confidence and contradiction cases get a human, and those labels are weighted in the eval set.
- **Trace + attribution invariants**: never relaxed by the loop. Faster/looser extraction that breaks traceability is a regression by definition.
- **Correction transparency**: corrections stay visible and linked; the loop optimizes accuracy, never the appearance of accuracy.

---

## OPEN QUESTIONS

- Total-net vs continuing-ops as default headline basis when both reported.
- Reconciler polling cadence and the bulk-index vs per-CIK tradeoff under the rate cap.
- Review-queue confidence threshold, tightened as the oracle grows.
- Consensus data source + licensing for Phase 4.
- Dedup when one release crosses multiple wires.

---

## ONE-LINE NORTH STAR

Every headline is a prediction the filed 8-K later grades; the system turns those grades into a feed that gets measurably more accurate each cycle — fast because it's automated, trusted because it's right, improving because it checks itself against the filing.

