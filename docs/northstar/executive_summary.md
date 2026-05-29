# EXECUTIVE SUMMARY: 8-K Intelligence Engine Implementation Kit
## Three Documents, One System

**Created**: May 28, 2026  
**For**: FATBABY financial intelligence platform  
**Objective**: Build a self-improving entity intelligence system that extracts governance signals from SEC filings and refines itself via Claude CLI

---

## THE PROBLEM

SEC 8-K filings contain **layers of intelligence** that are worthless in isolation:
- Director names and vote approval percentages
- Vote margins that reveal board friction
- Failed governance proposals that indicate entrenchment
- Signatory titles that track power shifts
- Auditor changes that signal risk

**Alone**: These are data points.  
**Together, across 500+ companies over 3+ years**: These become a predictive graph of:
- Who controls corporate America (director network)
- Where governance is breaking down (friction signals)
- Which companies are M&A targets or activist bait (composite risk)
- Which directors are losing influence (temporal decay)

**The goal**: Build a machine that extracts this intelligence automatically, learns from mistakes via Claude CLI feedback, and publishes actionable signals to traders.

---

## THE SOLUTION: THREE DOCUMENTS

### 1. **NORTHSTAR_8K_INTELLIGENCE_ENGINE.md**
**Purpose**: Architecture & implementation roadmap  
**Length**: ~600 lines  
**Audience**: Engineers, product managers  

**Contains**:
- The four-layer architecture (filing ingest → entity extraction → signal scoring → observation publishing)
- Detailed description of six signal dimensions (entity, vote, market, influence, security, temporal)
- Eight-week phased implementation roadmap (Phases 1-4)
- Data model schemas (nodes, edges, signals)
- Integration points with FATBABY
- Success metrics and risk mitigation

**Key sections**:
- Layer 3 (Signal Extraction) has detailed examples for each of the 6 signals
- Implementation Roadmap breaks down week-by-week deliverables
- Data Model shows exact JSON schemas

**When to read this**: First. It's the bible. Understand the architecture before writing code.

---

### 2. **CLAUDE_REFINEMENT_PROMPT.md**
**Purpose**: Weekly self-improvement template for Claude CLI  
**Length**: ~500 lines  
**Audience**: Emily agent, observation-watcher, Claude CLI  

**Contains**:
- Template for collecting metrics (filings processed, signals generated, accuracy)
- Section for parsing errors detected by Emily (name failures, incomplete extraction)
- Section for signal anomalies (signals that fired unexpectedly, missed signals, contradictions)
- Data enrichment requests (regulatory affiliations, lobbying data, activist tracking)
- Pattern discoveries from recent filings (clustering, entrenchment, auditor concentration)
- Specific refinement requests for Claude (signal rule recalibrations, new signals to build, data sources to add)
- Expected response format (JSON that observation-watcher can parse)

**Key sections**:
- "Parsing & Extraction Gaps" → Emily identifies what's failing
- "Signal Anomalies & Contradictions" → Emily identifies logical issues
- "Refinement Requests for Claude" → Specific areas needing improvement
- "Response Format" → JSON structure Claude should return

**When to read this**: After understanding the architecture. This is the weekly feedback loop template.

---

### 3. **QUICK_START_CLAUDE_CLI.md**
**Purpose**: Hands-on implementation guide with code examples  
**Length**: ~400 lines  
**Audience**: Engineers implementing the system  

**Contains**:
- Step-by-step setup instructions
- Working Go code for 8-K parser, entity graph, signal scorer
- Integration with Emily's tooling
- Test instructions on real Schwab data
- Emily ↔ Claude loop implementation (observation aggregator, refinement runner)
- Bash scripts for weekly scheduling
- TCP feed integration
- Troubleshooting guide

**Key sections**:
- Phase 1: Core extraction (with Go code)
- Phase 2: Emily ↔ Claude loop (with bash scripts)
- Phase 3: TCP feed integration
- Full end-to-end workflow example

**When to read this**: During implementation. Copy-paste the code, run the examples, test on fixtures.

---

## HOW THEY WORK TOGETHER

### The Flow

```
Engineer reads NORTHSTAR (understand the system)
  ↓
Engineer uses QUICK_START (implements Phase 1-2 in 2 weeks)
  ↓
System parses 8-K, extracts signals, publishes observations to var/emily-observations/latest.json
  ↓
Weekly scheduler runs CLAUDE_REFINEMENT_PROMPT template
  ↓
Claude CLI reads observation + metrics + gaps + anomalies
  ↓
Claude returns JSON with: new rules, new signals, data sources, code examples
  ↓
Observation-watcher parses JSON and hot-reloads rules into signal-extractor
  ↓
Next cycle improves on previous feedback
```

### Document Relationships

| When you're... | Read... | To learn... |
|---|---|---|
| Planning the system | NORTHSTAR | The 4-layer architecture, 6 signal types, 8-week roadmap |
| Implementing Phase 1 | QUICK_START Phase 1 | How to write the 8-K parser + entity graph builder |
| Implementing Phase 2 | QUICK_START Phase 2 | How to build Emily ↔ Claude feedback loop |
| Adding new signals | NORTHSTAR Layer 3 + CLAUDE_REFINEMENT | What signals exist, how Claude suggests new ones |
| Debugging signals | CLAUDE_REFINEMENT "Anomalies" section | Common failure patterns Emily detects |
| Scheduled weekly refinement | CLAUDE_REFINEMENT full template | What metrics to collect, what to ask Claude |
| Deploying to production | NORTHSTAR "Integration with FATBABY" | How signals flow into TCP feed |

---

## KEY INSIGHTS FROM THE SCHWAB 8-K EXAMPLE

The documents use a real filing (Charles Schwab 2026 annual meeting 8-K) to illustrate every concept:

1. **Entity Intelligence**: Marianne Brown (97.9% approval) vs Frank Herringer (84.3% approval)
   - Brown: high-trust director
   - Herringer: friction director (needs monitoring)
   - Signal: Friction signals are early warnings of director replacement

2. **Vote Patterns**: Board declassification vote got 92% shareholder support but failed
   - Reason: 80%-of-outstanding-shares supermajority threshold
   - Signal: Supermajority entrenchment (governance flashpoint)
   - Interpretation: Board is using structural defenses

3. **Market Relatedness**: Herringer's friction score (84.3%) propagates to his other board seats
   - Signal: Director-linked stocks may share governance risk
   - Trade idea: If Herringer is at 84.3% on SCHW, monitor his other companies

4. **Influence**: Carolyn Schwab-Pomerantz (founder family member)
   - Signal: Family control indicator (founder concentration proxy)
   - Interpretation: Impacts regulatory positioning, M&A likelihood

5. **Security/M&A Risk**: Classified board maintained despite voter intent
   - Signal: Company in "fortress mode"
   - Interpretation: Either pre-M&A defense OR activist resistance

6. **Temporal**: Track Herringer's approval YoY (2024: 89.1% → 2025: 86.5% → 2026: 84.3%)
   - Signal: Declining trend suggests director on borrowed time
   - Projection: If trend continues, <80% in 2027-2028 → likely replacement

---

## IMPLEMENTATION TIMELINE

**Minimal Viable Product (MVP)** = 4 weeks to launch

```
Week 1: Core Extraction
  ├─ 8-K Item 5.07 parser (Go)
  ├─ Entity canonicalization
  ├─ 6 basic signals implemented
  └─ Test on Schwab 2026 8-K ✓

Week 2-3: Data & Signals
  ├─ Expand to 10+ companies
  ├─ Build temporal signals (YoY trend detection)
  ├─ Cross-company director linking
  └─ TCP feed integration

Week 4: Emily ↔ Claude Loop
  ├─ Observation aggregator
  ├─ Claude CLI integration
  ├─ Weekly refinement runner
  └─ First manual refinement cycle ✓

Week 5+: Continuous Improvement
  ├─ Weekly Claude-driven refinements
  ├─ New signals discovered
  ├─ Accuracy tracking
  └─ Production monitoring
```

---

## SUCCESS METRICS

**By end of Week 4**:
- ✅ 8-K parser works (100% accuracy on Schwab, 95%+ on peer companies)
- ✅ 50+ directors identified and linked
- ✅ 200+ signals generated
- ✅ Emily ↔ Claude loop operational (3+ refinement cycles)
- ✅ At least 1 new signal type discovered via Claude feedback

**By end of Week 8**:
- ✅ 50+ companies tracked
- ✅ 500+ directors with relationships
- ✅ 1000+ signals with temporal trends
- ✅ Friction signals accuracy >75% (correlate with director replacement)
- ✅ M&A risk signals accuracy >60% (precede activist 13D)
- ✅ Related-stocks signals: Herringer-linked stocks >0.4 correlation
- ✅ Regulatory enrichment: 20+ directors with documented affiliations

---

## RECURSIVE SELF-IMPROVEMENT MECHANISM

The secret sauce: **Claude CLI as the learning loop**

```
Monday 09:00 UTC: Automated weekly refinement
│
├─ Emily publishes: "Last 7 days: {metrics}, {anomalies}, {gaps}"
│
├─ CLAUDE_REFINEMENT_PROMPT template fills in metrics + observations
│
├─ Claude CLI processes: "Given these signals + gaps, what should we improve?"
│
├─ Claude returns: "I see 3 issues: (1) name parsing fails on hyphens, (2) friction
│                   signal misfires on small boards, (3) missing auditor changes.
│                   Here's the fix..." + YAML rules + Go skeleton code
│
├─ Observation-watcher parses Claude's response
│
├─ Rules hot-reload into signal-extractor (no restart)
│
└─ Next cycle benefits from previous feedback
```

**This is NOT traditional machine learning.** It's:
- Symbolic (rules-based)
- Interpretable (each rule is explicit)
- Fast (no retraining)
- Human-auditable (Claude explains its reasoning)
- Bootstrapping from domain knowledge (6 signal types + observation framework)

---

## DEPLOYMENT OPTIONS

### Option 1: Standalone (Testing/MVP)
```bash
# Just run the core logic
go run cmd/{entity-graph,signal-extractor,signal-observer}/*.go
# Signals publish to var/emily-observations/
# Weekly Claude refinement via cron
```

### Option 2: FATBABY Integration (Recommended)
```bash
# Integrate into FATBABY's architecture
# Signals publish to TCP feed
# Emily agent triggers Claude refinement
# Observation-watcher applies hot-reloads
# Consumers subscribe via fatstream CLI
```

### Option 3: Production (Full Stack)
```bash
# Deploy to Kubernetes
# API endpoint for traders/researchers
# Prometheus monitoring
# PagerDuty alerting on new high-severity signals
# Backtesting engine to measure signal alpha
```

---

## FAQ

**Q: Will this actually predict M&A?**  
A: Not perfectly, but the composite signal (classified board + governance friction + founder family) has high signal quality. Backtest on 2022-2025 data to measure accuracy.

**Q: How often does Claude need to refine?**  
A: Weekly is ideal (catches patterns before they grow). Can be triggered manually or on-demand.

**Q: Can this be automated without Claude?**  
A: Yes, but you lose the "recursive self-improvement" part. Static rules work for 80% of cases. Claude helps with the long tail.

**Q: What about privacy / insider info?**  
A: All data is public SEC filings. No insider info. Safe for trading.

**Q: How much does Claude API cost?**  
A: ~$0.10 per refinement run (Claude processes ~500 lines of metrics/observations). ~$4-5/month ongoing.

---

## NEXT STEPS

1. **Read NORTHSTAR** (30 min) → understand the architecture
2. **Read QUICK_START Phase 1** (1 hour) → implement the 8-K parser
3. **Test on Schwab fixture** (30 min) → verify parsing works
4. **Implement Phase 2** (1 week) → Emily ↔ Claude loop
5. **Run weekly refinements** (ongoing) → watch system improve

**Total time to MVP**: 3-4 weeks  
**Total time to production**: 8-10 weeks

---

## DOCUMENTS IN THIS KIT

1. **NORTHSTAR_8K_INTELLIGENCE_ENGINE.md** (600 lines)
   - Architecture blueprint
   - 8-week roadmap
   - Data schemas
   - Integration points

2. **CLAUDE_REFINEMENT_PROMPT.md** (500 lines)
   - Weekly feedback template
   - Metrics collection
   - Anomaly detection
   - Refinement requests

3. **QUICK_START_CLAUDE_CLI.md** (400 lines)
   - Working Go code
   - Bash scripts
   - Test examples
   - Troubleshooting

4. **This file** (200 lines)
   - Executive summary
   - Document relationships
   - Timeline
   - FAQ

**Total**: ~1700 lines of documentation + runnable code  
**Ready to implement?** Start with NORTHSTAR, then QUICK_START Phase 1.

---

**Questions? Comments? Feedback?**  
File issues in the FATBABY repo or email the implementation team.

**Status**: Ready for implementation (2026-05-28)  
**Next review**: 2026-06-27 (after Week 4 MVP)
