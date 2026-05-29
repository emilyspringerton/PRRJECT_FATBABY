# NORTHSTAR: The Anchor
## Synthetic Presenter & Broadcaster-Register Voice Model for FATBABY

**Status**: Implementation Framework
**Version**: 1.0
**Date**: May 29, 2026
**Architecture**: FATBABY-native (sits atop the existing signal feed)
**Codename**: The Anchor

---

## EXECUTIVE INTENT

Build a **persistent synthetic presenter** that makes FATBABY's intelligence consumable by a human being — a recognizable character with a polished broadcaster-register voice that takes structured signals off the feed and delivers them as short, watchable, *auditable* explainer segments.

The ambition is real: a memorable face and voice for the platform, eventually packageable as its own product. The discipline is also real: the presenter earns trust by **showing its work and being right over time**, never by impersonating journalism or any individual journalist.

**Core insight**: A firehose of 8-K vote math is illegible to the people it could most help. A presenter is an *affordance* — it lowers the cost of comprehension and gives the user a one-click path from "the Anchor said this" to "here is the SEC filing that proves it." That second half is what turns a presenter into an instrument of agency rather than persuasion.

---

## THE REFRAME (read this before anything else)

The original brief asked for a "credible and trusted news representative." This document keeps the credibility and discards the impersonation, because the two are separable and only one of them is load-bearing.

- **Credibility we keep**: a professional, authoritative delivery; a consistent, recognizable voice; a polished broadcaster *register*.
- **Trust mechanism we use**: disclosure that it's AI, plus verifiable sourcing on every claim, plus a track record the user can check.
- **What we explicitly refuse**: making the synthetic source *appear to be* a real newsroom or a real journalist so that audiences extend it the trust they've reserved for actual journalism. That's borrowed authority, and borrowed authority pointed at financial content is the one thing this project will not be.

Credibility through transparency, not credibility through costume. Everything below enforces that distinction structurally.

---

## ARCHITECTURE: FIVE LAYERS

```
FATBABY TCP signal feed
        │
        ▼
[L1] Signal selector   ─ which signals warrant a segment
        │
        ▼
[L2] Script generator  ─ signal object → constrained five-beat script (render-gated)
        │
        ▼
[L3] Voice model       ─ broadcaster-register synthesis, blended, no individual
        │
        ▼
[L4] Render + persona  ─ avatar/voice + persistent disclosure chrome
        │
        ▼
[L5] Provenance UI     ─ "show your work" panel, source links, feedback loop
```

The presenter adds **no new data**. It is a delivery surface over signals FATBABY already produces.

---

## LAYER 3 (FOCUS): THE VOICE MODEL

This is the new piece. The goal is to capture the *register* of broadcast delivery without reproducing any identifiable person.

### 3.1 What "broadcaster register" decomposes into

The recognizable "anchor sound" is a bundle of learnable acoustic and prosodic features, none of which belong to any individual:

- **Prosody**: measured pace (slower than conversational), deliberate phrase-final lengthening, controlled pitch declination across sentences.
- **Cadence**: the characteristic "newsreader" stress pattern — emphasis on nouns and numbers, even spacing of stressed syllables.
- **Timbre**: warm, low-to-mid fundamental, minimal vocal fry, clean articulation.
- **Affect**: calm authority, low emotional variance, "trust me, I have this handled" steadiness.
- **Studio acoustics**: dry, close-mic'd, compressed dynamic range.

These are *genre features*. The target is the centroid of the genre, not any speaker in it.

### 3.2 The blend mandate (hard constraint)

- The model is trained/tuned to produce a **meta-blend** — a synthetic voice that is identifiably *no one*.
- **No identifiable individual** may be reconstructable from the output. If a listener can name the anchor, the model has failed and must be retuned or have that contributor removed.
- Validation: run periodic speaker-identification / similarity checks against a held-out set of real broadcasters. Output that scores above a similarity threshold to any single real person is rejected.

### 3.3 Tunable parameters (the "single tunable model" goal)

Expose a small control surface so the register can be dialed:

- `formality` (conversational ↔ formal newsreader)
- `pace_wpm`
- `authority` (warm/approachable ↔ gravitas)
- `energy` (calm digest ↔ urgent alert — capped; see disclosure rules)
- `pitch_baseline`, `pitch_range`
- studio_reverb / compression preset

### 3.4 Training-data sourcing — options and the legal reality

Routes, fastest-and-cleanest first:

1. **Licensed/recorded talent (recommended).** Audition and record voice actors performing the broadcaster register under contract. Cleanest rights, fully defensible, distinctive. Use a performer agreement with a synthesis/likeness rider that scopes uses (including the product path) and obtains explicit written consent to clone/blend the voice. Auditioning is fine; only signed, consenting takes enter the model.
2. **Licensed corpora / partnership.** If you license footage from a broadcaster (your industry connections), the *license* must explicitly cover model training and synthesis, and individual on-air talent's consent is a separate question the licensor may or may not be able to grant. Confirm both.
3. **Scraped broadcast footage.** Whether collecting copyrighted broadcasts at scale is fair use is **genuinely contested and not something this document can resolve.** It is a question for your IP counsel before a byte is collected, not a settled fact. Treat any "it's fair use" assumption as unverified until counsel signs off.

**Recommendation**: build on route 1. It gets you the register, clean rights, and no individual-likeness exposure, and it doesn't stake the project on an unresolved fair-use bet.

---

## LAYER 2: SCRIPT GENERATION (render-gated)

Scripts are generated *from* the signal object, never written freely. Every segment must contain five beats, each tracing to a field: **what fired · the numbers · what it might mean (hedged) · what would make this wrong · confidence + source.**

**Render gate**: if `source.primary_doc_url`, `confidence`, or `falsifier` is missing, the segment does not render as video — it falls back to a text card. The generator is barred from buy/sell/hold language and from speaking any number not present in the data. (Full schema and prompt live in the templates doc.)

This is what makes "real data" hold by construction: the presenter structurally cannot fabricate a figure or assert something it can't source.

---

## LAYER 4-5: DISCLOSURE, PROVENANCE, PERSONA

### Disclosure (non-removable)
- Persistent, non-dismissible **AI-GENERATED** badge on every segment.
- "Not investment advice. Automated, probabilistic." on every segment.
- Position-held disclosure for any named security.

### Provenance ("show your work")
- Source panel with the actual filing, exact item/section, the numbers.
- Expandable "why this fired" (rule + threshold + trend).
- Visible confidence and its falsifier.
- A "this looks wrong" path that feeds the existing Emily/Claude refinement loop.

### Persona
- Consistent name, look, voice; recurring framing; stable skeptical editorial voice.
- Explicitly a construct. Its authority is the disclosure plus the track record, full stop.

---

## PRODUCTIZATION PATH

If The Anchor becomes its own product, the constraints travel with it unchanged:
- blended, no-individual voice;
- disclosure baked in as a default, not a toggle;
- provenance-linking shipped as a core feature.

"A disclosed synthetic presenter for your data" is a viable product (cf. existing synthetic-media companies). "A make-it-look-like-real-news engine" is not — it's a liability with a logo. The line that protects the primary use case is the same line that protects the product.

---

## REGULATORY SURFACE (flag — not legal advice)

Financial content to retail consumers carries obligations regardless of who delivers it:
- analysis vs. recommendation framing;
- not-investment-advice and automated-nature disclaimers;
- disclosure of positions in named securities;
- record-keeping of what was said and when.

Get a securities attorney on the script templates before public launch. Pair them with the IP counsel handling the training-data question.

---

## IMPLEMENTATION ROADMAP

- **Phase 0 — Validate (2 wks).** Off-the-shelf avatar + stock broadcaster TTS, 5 hand-picked signals, manual scripts, the source panel. Does the affordance actually improve comprehension?
- **Phase 1 — Pipeline (3-4 wks).** Automate selector → render-gated script → render for the live feed; ship disclosure + provenance UI.
- **Phase 2 — Voice model (4-6 wks).** Record consenting talent; train the blended register model; wire the tunable parameters; pass the no-individual similarity gate.
- **Phase 3 — Persona + feedback.** Lock the character; wire "this looks wrong" into the Emily loop.
- **Phase 4 — (optional) Productize.** In-house synthetic face, packaging, multi-tenant.

---

## SUCCESS METRICS

- **Comprehension lift**: can a user correctly restate a signal after a segment vs. after raw JSON?
- **Verification rate**: % of segments where the user clicks through to the source (higher = the agency mechanism is working).
- **Source-binding integrity**: 100% of rendered segments resolve to a primary document. No exceptions; this is a gate, not a KPI.
- **No-individual integrity**: 0 outputs above the speaker-similarity threshold to any real broadcaster.
- **Zero undisclosed segments**: the AI label fires on 100% of renders.

---

## OPEN QUESTIONS

- Which signals deserve a segment vs. a text card? (Not everything needs a face.)
- Cadence: per-signal, or a digest?
- How much persona personality before it undercuts auditability?
- `energy` parameter ceiling — how much urgency is honest given probabilistic signals?
- Build vs. buy on the voice/avatar for the long term.

---

## THE ONE-LINE NORTH STAR

A recognizable synthetic voice that makes FATBABY legible and **always shows the receipts** — credibility earned through transparency, never borrowed through costume.
