# NORTHSTAR: 8-K Intelligence Engine
## Recursive Self-Improving Financial Entity Graph System

**Status**: Implementation Framework  
**Version**: 1.0  
**Date**: May 28, 2026  
**Architecture**: FATBABY-native (Go + Claude CLI feedback loops)

---

## EXECUTIVE INTENT

Build a **self-improving entity intelligence system** that:
1. Ingests SEC 8-K filings (and derivative forms: 10-K vote sections, proxy statements)
2. Extracts **people, relationships, vote patterns, and governance signals** as a knowledge graph
3. Surfaces **six intelligence dimensions** (entity, vote, market, influence, security, temporal)
4. Uses **Claude CLI as the self-improvement engine**: observations from Emily (the agent) trigger Claude prompts that refine extraction rules, detect new signal patterns, and improve the model
5. Publishes **traded insights** via the FATBABY TCP feed for downstream consumption

**Core insight**: A single 8-K contains layers of intelligence (entity network, board friction, M&A defense, regulatory positioning) that are worthless in isolation but form a predictive signal graph when indexed across 500+ companies over time.

---

## ARCHITECTURE: FOUR LAYERS

### Layer 1: Filing Ingest & Parsing
**Owned by**: `secwatch` package (FATBABY)  
**Status**: Operational for 8-K, 10-K vote sections, proxy statements  

**Current capability**:
- EDGAR ticker → CIK resolution
- Watchlist-based polling (configurable forms: 8-K, DEF 14A, PRES14A)
- Submission JSON parsing (recent filings endpoint)
- URL construction for primary documents

**What must be added** (recursive refinement):
- **Form 8-K Item 5.07 parser** (vote results) → extract:
  - Nominee names, approval %s, vote counts (for/against/abstain/broker-NV)
  - Failed proposals (80% threshold detection)
  - Director friction scores (non-unanimous votes)
- **Proxy statement parser** (Schedule 14A) → extract:
  - Board member bios, affiliations, prior roles
  - Executive compensation (NEO list)
  - Ownership structure, activist notifications
- **Error recovery & format variance** (Emily observes parsing failures, Claude suggests regex/field refinements)

**Feedback loop**: Emily watches `var/secwatch/` for parsing errors → publishes observation → Claude CLI refines regex patterns → deployment

---

### Layer 2: Entity Extraction & Graph Construction
**Owned by**: New package `cmd/entity-graph` (Go + graph database)  
**Status**: To be built

**What this layer does**:
1. **Canonicalize names** (fuzzy match across filings: "John Smith" vs "J. Smith" vs "John T. Smith")
2. **Build co-occurrence edges**: If two people appear in the same filing, create edge with metadata (role, company, date)
3. **Classify nodes**: director, executive, auditor, advisor, signatory, family member
4. **Score relationships**: frequency of co-appearance, sequence (who appears first in voting results), temporal drift

**Data structure** (in-memory + NDJSON append-only store):
```json
{
  "node_id": "marianne_c_brown_schwab_2026",
  "name": "Marianne C. Brown",
  "canonical_id": "marianne-c-brown",
  "type": "director",
  "filings": [
    {
      "ticker": "SCHW",
      "cik": "0000086364",
      "form": "8-K",
      "filing_date": "2026-05-21",
      "vote_approval": 0.979,
      "vote_count": 1408658965,
      "vote_against": 29435843
    }
  ]
}
```

**Edges** (people-to-people, people-to-company):
```json
{
  "source": "marianne-c-brown",
  "target": "frank-c-herringer",
  "edge_type": "board_co-member",
  "company": "SCHW",
  "start_date": "2026-01-01",
  "count_filings": 1,
  "metadata": {
    "herringer_friction": 0.157,
    "brown_friction": 0.021
  }
}
```

**Recursive refinement** (Emily's role):
- Detect canonicalization failures (same person appears as two nodes)
- Flag outlier approval %s (e.g., one director at 50% vs others at 95%)
- Identify "bridge nodes" (people connecting otherwise-isolated companies)

---

### Layer 3: Signal Extraction & Scoring
**Owned by**: New package `cmd/signal-extractor` (Go + rules engine)  
**Status**: To be built

**Six signal dimensions** (from the earlier analysis):

#### 3.1 Entity Intelligence Signals
- **High-trust director** (>95% approval, >3 years tenure, unanimous history)
- **Friction director** (<85% approval, widening dissent trend)
- **Nominee rejection** (failed to get elected; indicates activist opposition or governance crisis)
- **Auditor change** (new auditor on next filing; risk signal)
- **Family director score** (Schwab-Pomerantz seat as founder control proxy)

**Signal example**:
```json
{
  "signal_id": "friction_herringer_schwab_2026",
  "type": "director_friction",
  "severity": "medium",
  "ticker": "SCHW",
  "person": "frank-c-herringer",
  "approval_pct": 0.843,
  "historical_trend": [0.891, 0.865, 0.843],
  "interpretation": "Declining approval suggests activist targeting or board misalignment",
  "confidence": 0.78
}
```

#### 3.2 Vote Pattern Signals
- **Failed structural votes** (e.g., 92% for but 80% supermajority threshold blocks it)
- **Outsized broker non-vote** (>15% non-votes = liquidity/retail signal)
- **Abstention spikes** (unusual abstention rate may indicate shareholder confusion or protest)
- **Supermajority entrenchment** (classified board maintained despite clear vote intent)

**Signal example**:
```json
{
  "signal_id": "entrenchment_schwab_2026",
  "type": "governance_entrenchment",
  "severity": "high",
  "ticker": "SCHW",
  "proposal": "Declassify Board",
  "for_pct": 0.920,
  "failed_due_to": "80% of outstanding shares supermajority",
  "interpretation": "Board is using structural defenses against clear shareholder preference",
  "related_risk": "M&A defense or activist targeting likely",
  "confidence": 0.91
}
```

#### 3.3 Market / Related-Stocks Signals
- **Director-linked companies** (shared board members → shared governance risk)
- **Friction score propagation** (if Herringer is at 84.3% on SCHW, apply a discount to his other board seats)
- **Auditor peer signals** (Deloitte at 10+ brokerages; audit quality correlation across peers)
- **Sector governance drift** (e.g., all fintech brokers suddenly have higher abstention rates)

**Signal example**:
```json
{
  "signal_id": "related_stock_herringer",
  "type": "director_link",
  "person": "frank-c-herringer",
  "companies": ["SCHW", "OtherCorp1", "OtherCorp2"],
  "friction_score": 0.157,
  "recommendation": "Herringer friction at SCHW (84.3%) implies governance risk across his portfolio",
  "trade_idea": "Consider short or reduced exposure to other Herringer-board companies"
}
```

#### 3.4 Influence & Power Signals
- **Regulatory revolving door** (board member = ex-SEC/FINRA official)
- **Lobbying network** (cross-reference with FARA database: which directors lobby for financial regulation?)
- **Political donor pattern** (FEC records: consistent donor to specific party/candidate = political alignment)
- **Think tank affiliations** (AEI, Brookings, CFR board members = influence nodes)

**Signal example** (requires manual enrichment):
```json
{
  "signal_id": "regulatory_exposure_brown",
  "type": "influence_node",
  "person": "marianne-c-brown",
  "prior_roles": ["SEC Commissioner 2010-2015", "FINRA Board Advisor 2016-2020"],
  "current_boards": ["SCHW", "OtherBank"],
  "interpretation": "Brown's SEC/FINRA history suggests Schwab is positioning for regulatory engagement",
  "confidence": 0.65
}
```

#### 3.5 Security & Risk Signals
- **Classified board persistence** (despite shareholder votes) = takeover defense active
- **GC/Legal signatory changes** (Peter Morgan's role → track across filings for turnover)
- **Pre-M&A governance** (activist 13D filed in last 90 days? Classified board + resistance = M&A prep)
- **Executive comp drift** (NEO pay increases >25% YoY while stock flat = ESG red flag)

**Signal example**:
```json
{
  "signal_id": "ma_risk_schwab",
  "type": "security_event",
  "severity": "medium",
  "ticker": "SCHW",
  "indicators": [
    "Classified board maintained despite 92% shareholder vote",
    "Frank Herringer (director) showing friction (84.3% approval)",
    "Board declassification vote triggered (suggests activist pressure)",
    "Supermajority threshold enforced = management entrenchment"
  ],
  "interpretation": "Company is in defensive posture; possible activist raid or M&A target",
  "actionable": "Monitor for 13D filings, listen to Q&A calls for acquisition/activist mentions"
}
```

#### 3.6 Temporal Intelligence Signals
- **Vote margin drift** (director approval declining YoY)
- **Compensation approval trend** (NEO pay votes declining = ESG shift)
- **Auditor tenure** (same auditor for 20+ years = potential audit fatigue)
- **Director replacement cadence** (retirement patterns = succession planning window)

**Signal example**:
```json
{
  "signal_id": "temporal_herringer",
  "type": "director_decay",
  "person": "frank-c-herringer",
  "ticker": "SCHW",
  "approval_history": [
    { "year": 2024, "pct": 0.891 },
    { "year": 2025, "pct": 0.865 },
    { "year": 2026, "pct": 0.843 }
  ],
  "trend": "declining",
  "projection": "If trend continues, <80% in 2027-2028",
  "interpretation": "Herringer is on borrowed time; likely to not be renominated in 12-18 months"
}
```

**Feedback loop** (Emily's job):
- Monitor signal database for contradictions (e.g., high-trust director suddenly has friction signal)
- Flag missing signals (8-K filed but no vote signal emitted → parsing error)
- Score signal accuracy retrospectively (did our "M&A risk" signal precede actual M&A event?)

---

### Layer 4: Observation Publication & Feedback Loop
**Owned by**: New package `cmd/signal-observer` + Claude CLI integration  
**Status**: To be built (Emily ↔ Claude feedback)

**Emily's workflow** (runs every 6 hours):
1. Scan `var/emily-observations/latest.json` (from Layer 3)
2. Aggregate signals: if 3+ high-severity signals in same company, publish observation
3. **Write observation** to `var/emily-observations/` with:
   - Signal summary
   - Confidence scores
   - Gaps (missing data, unparsed fields)
   - Requests for Claude refinement

**Example observation** (Emily → Claude):
```json
{
  "timestamp": "2026-05-28T12:00:00Z",
  "status": "needs_refinement",
  "severity": "high",
  "subject": "SCHW Board Governance Crisis",
  "signals": [
    {
      "id": "entrenchment_schwab_2026",
      "type": "governance_entrenchment",
      "score": 0.91
    },
    {
      "id": "friction_herringer_schwab_2026",
      "type": "director_friction",
      "score": 0.78
    },
    {
      "id": "ma_risk_schwab",
      "type": "security_event",
      "score": 0.73
    }
  ],
  "gaps": [
    "No regulatory affiliation data for Marianne Brown (need FEC/SEC history)",
    "Herringer's other board seats not yet parsed (need to extract from proxy statements)",
    "Auditor Deloitte peer analysis incomplete (need to rank auditors by approval variance)"
  ],
  "request_for_claude": "Given the three signals above and Schwab's 2026 proxy, what is the probability of activist intervention in 2027? What additional data should we extract?"
}
```

**Claude CLI refines** (via `claude --dangerously-skip-permissions`):
1. **Rule discovery**: "I notice Herringer's approval declined 4.8pp YoY. Add a rule: if any director declines >4pp YoY, flag as friction."
2. **Signal recalibration**: "Your M&A risk signal is good, but add a weight: if classified board + founder family member + failing governance vote, multiply risk by 1.5."
3. **Gap closing**: "To get Marianne Brown's regulatory history, query SEC EDGAR for her prior roles in comment letters. Here's a regex to parse that:"
4. **New dimension detection**: "I see a pattern: every time a declassification vote fails, the following year has activist 13D activity within 6 months. Add a new signal: 'Post-Failure Activist Prediction.'"

**Observation → Claude → Code → Observation loop**:
```
Emily publishes observation
  ↓
Claude CLI processes via prompt (NORTHSTAR-claude-refine.md)
  ↓
Claude suggests code changes (new parsing rules, signal refinements)
  ↓
Developer implements OR Claude Code auto-refines
  ↓
Next cycle, Emily measures improvement (signal accuracy, gap closure)
  ↓
Feedback feeds back into Claude's next prompt
```

---

## IMPLEMENTATION ROADMAP

### Phase 1: Core Extraction (Weeks 1-2)
**Deliverable**: End-to-end 8-K parsing → entity graph → signal export

**Checklist**:
- [ ] Form 8-K Item 5.07 parser (Go + regex)
  - Extract nominee names, vote counts, approval %s
  - Detect failed proposals (supermajority threshold logic)
  - Unit tests against 10 Schwab-like filings
- [ ] Entity canonicalization (fuzzy name matching, known aliases)
  - Integrate `github.com/joncrlsn/damerau-levenshtein` for edit distance
  - Build alias dictionary from prior filings
- [ ] Graph storage (NDJSON append-only in `var/secwatch/entity-graph.ndjson`)
  - Node schema: person + filings + roles
  - Edge schema: co-occurrence + metadata
- [ ] Signal scoring (hardcoded rules for signals 3.1–3.6)
  - Approval % → friction score (85% threshold)
  - Trend detection (3-year historical if available)
  - Supermajority logic (80% of *outstanding*, not votes cast)
- [ ] Export to TCP feed (via FATBABY `cmd/signal-api`)

**Test criteria**:
- Parse Schwab 2026 8-K correctly: extract all 4 directors, their %s, and failed declassification
- Produce 3+ signals per company
- 90%+ precision on vote count extraction (compare to filing text)

---

### Phase 2: Recursive Refinement Loop (Weeks 3-4)
**Deliverable**: Emily ↔ Claude feedback loop operational

**Checklist**:
- [ ] Emily integration (`cmd/emily-agent` extension)
  - POST `/observe` endpoint: accept observation JSON
  - Write to `var/emily-observations/latest.json`
  - Publish "Claude, refine entity extraction" prompt
- [ ] Claude CLI integration (`cmd/observation-watcher` extension)
  - Poll `var/emily-observations/latest.json` every 10 min
  - Shell out: `claude --dangerously-skip-permissions < refine_prompt.md`
  - Parse Claude's suggestions (rules, new signals, data sources)
  - Emit to `var/emily-observations/claude_suggestions.json`
- [ ] Rule engine for signal refinement
  - Accept Claude-suggested rules in YAML
  - Hot-reload into signal scorer without restart
  - Version rules (date + Claude feedback version)
- [ ] Accuracy tracking
  - After 4 weeks, measure: did friction signals correlate with director replacement?
  - Did M&A risk signals precede activist 13D filings?
  - Feed back into next Claude prompt as "ground truth"

**Test criteria**:
- 10 refine cycles completed
- At least 2 new signals discovered via Claude suggestions
- Rule hit rate >80% (rules don't fire on data outside their domain)

---

### Phase 3: Multi-Company Intelligence (Weeks 5-6)
**Deliverable**: Cross-company entity graph + related-stocks signals

**Checklist**:
- [ ] Expand watchlist to 20–50 financial companies
  - Brokerages: SCHW, TD, IBKR, SOFI, HOOD, etc.
  - Banks: JPM, BLK, GS, MS, WFC, etc.
  - FinTech: COIN, MSTR, UPST, etc.
- [ ] Graph merge across companies
  - Identify shared directors (e.g., Herringer on 3 boards)
  - Build "director centrality" metric
  - Flag "bridge nodes" (people connecting otherwise-isolated sectors)
- [ ] Director-linked stock signals
  - If Herringer is at 84.3% on SCHW, apply friction score to his other companies
  - Measure correlation: do Herringer-linked stocks move together?
- [ ] Auditor peer signals
  - Deloitte audits how many SCHW-peer companies?
  - Is audit quality correlated across Deloitte clients?
- [ ] Temporal cross-company drift
  - Do director friction scores increase industry-wide in recession?
  - Do M&A risk signals cluster?

**Test criteria**:
- 50+ directors identified across companies
- 20+ shared director relationships
- Prove director-link signal has alpha vs random selection

---

### Phase 4: Influence & Security Enrichment (Weeks 7-8)
**Deliverable**: Regulatory, lobbying, and M&A risk signals

**Checklist**:
- [ ] Regulatory revolving door
  - Query SEC EDGAR for director names in comment letters
  - Cross-reference FEC donor database (www.fec.gov API)
  - Build "regulatory affiliation" score for each director
- [ ] Lobbying network (requires FARA data)
  - Download FARA disclosures (opengovus.com or DIY scrape)
  - Match director names to lobbying clients
  - Identify financial services lobbying focus
- [ ] Activist 13D/13G integration
  - Monitor Edgar for Schedule 13D filings on watchlist companies
  - Correlate with governance friction signals (did we predict this?)
  - Measure signal accuracy retrospectively
- [ ] M&A preparation signals
  - Classified board + founder family seat + governance friction = M&A risk multiplier
  - GC changes = potential transaction planning
  - Executive comp changes = management transition risk
- [ ] Executive turnover tracking
  - Peter J. Morgan III (GC) as canonical node
  - Track across filings: is he moving to another company?
  - Predict CEO changes (CFO → CEO pipeline)

**Test criteria**:
- 10+ directors with regulatory affiliations identified
- Lobbying data matched for 5+ directors
- M&A risk signals published
- Retrospective accuracy: did high-scoring signals precede M&A events?

---

## RECURSIVE SELF-IMPROVEMENT PROMPT
**For Claude CLI (Weekly Refinement)**

**File**: `NORTHSTAR-claude-refine.md`

```markdown
# 8-K Intelligence Engine: Weekly Self-Refinement

## Current State
- Signals generated: {signal_count}
- Accuracy (vs ground truth): {accuracy_pct}%
- Gaps identified: {gap_count}
- Coverage: {company_count} companies, {filing_count} filings

## Observations from Emily (Last 7 Days)
{emily_observations_json}

## Questions for You
1. Given the signals above, what patterns emerge that we're missing?
2. Which signals have the highest precision? Which need recalibration?
3. What new data sources should we add? (Regulatory history, lobbying, political donors)
4. Are there director network patterns we should model explicitly?
5. How would you score "M&A risk" differently?

## Retrospective Accuracy
- Friction signals → Director replacement within 12 months: {accuracy}%
- Entrenchment signals → Activist 13D within 6 months: {accuracy}%
- Related-stocks signals → Correlation >0.3: {accuracy}%

## Action Items for Next Sprint
Based on your suggestions:
1. {claude_suggestion_1}
2. {claude_suggestion_2}
3. {claude_suggestion_3}

Please provide:
- **New signal rules** (in YAML format)
- **Data source recommendations** (with API/scrape approach)
- **Accuracy improvements** (what to measure, how to validate)
- **Code patterns** (Go examples for new extractors)
```

---

## DATA MODEL: SCHEMA

### Node: Person
```json
{
  "canonical_id": "frank-c-herringer",
  "name": "Frank C. Herringer",
  "aliases": ["Frank Herringer", "F. Herringer"],
  "type": "director|executive|auditor|signatory|advisor",
  "entity_count": 3,
  "first_appearance": "2022-01-15",
  "last_appearance": "2026-05-21",
  "sectors": ["financial_services", "brokerage"],
  "regulatory_affiliations": [],
  "lobbying_affiliations": [],
  "political_donor_party": null
}
```

### Node: Company
```json
{
  "ticker": "SCHW",
  "cik": "0000086364",
  "name": "The Charles Schwab Corporation",
  "sector": "brokerage",
  "filing_count": 12,
  "last_filing": "2026-05-21",
  "board_size": 11,
  "founder_control": 0.15,
  "governance_risk_score": 0.68,
  "m2a_risk_score": 0.62
}
```

### Edge: Co-Appearance
```json
{
  "source": "frank-c-herringer",
  "target": "marianne-c-brown",
  "edge_type": "board_co-member",
  "companies": ["SCHW"],
  "filings": [{"ticker": "SCHW", "form": "8-K", "date": "2026-05-21"}],
  "strength": 1,
  "metadata": {
    "shared_committee": "Nominating & Governance Committee"
  }
}
```

### Signal
```json
{
  "signal_id": "friction_herringer_schwab_2026",
  "type": "director_friction|governance_entrenchment|m_and_a_risk|regulatory_revolving_door|activist_prediction|auditor_concern|ceo_succession_window",
  "ticker": "SCHW",
  "entity": "frank-c-herringer",
  "severity": "low|medium|high|critical",
  "confidence": 0.78,
  "score": 0.843,
  "detection_date": "2026-05-21",
  "valid_through": "2027-05-21",
  "interpretation": "...",
  "action_recommended": "...",
  "ground_truth": null,
  "accuracy_retrospective": null
}
```

---

## INTEGRATION WITH FATBABY

### Where This Lives
```
cmd/
  ├── secwatch/           (existing)
  ├── signal-api/         (existing)
  ├── emily-agent/        (existing, extend)
  ├── observation-watcher/ (existing, extend)
  ├── entity-graph/       (NEW)
  ├── signal-extractor/   (NEW)
  └── signal-observer/    (NEW)

var/
  ├── secwatch/
  │   ├── watchlist.json
  │   ├── recent-filings/
  │   └── raw-bodies/
  ├── emily-observations/
  │   ├── latest.json
  │   └── archive/
  ├── entity-graph/
  │   ├── nodes.ndjson
  │   ├── edges.ndjson
  │   └── signals.ndjson
  └── logs/
```

### Feedback Flow
```
secwatch polls EDGAR
  ↓
emily-agent receives new filings
  ↓
signal-extractor emits signals
  ↓
emily-observations publishes summary
  ↓
observation-watcher triggers Claude CLI
  ↓
Claude suggests rule refinements
  ↓
Rules hot-reload into signal-extractor
  ↓
Next cycle improves on previous feedback
```

### TCP Feed Integration
Signals publish to FATBABY TCP feed:
```json
{
  "message_type": "signal",
  "signal_id": "friction_herringer_schwab_2026",
  "ticker": "SCHW",
  "severity": "medium",
  "confidence": 0.78,
  "action": "monitor_activist_13d",
  "timestamp": "2026-05-28T14:30:00Z"
}
```

Consumers (trading systems, research teams) subscribe via TCP:
```bash
fatstream subscribe "signal:*" --severity medium,high
```

---

## SUCCESS METRICS

### Week 4 (End of Phase 2)
- ✅ 10+ companies parsed with 8-K data
- ✅ 50+ directors identified
- ✅ 200+ signals generated
- ✅ Emily ↔ Claude loop operational (3+ refine cycles)
- ✅ At least 1 new signal type discovered via Claude feedback

### Week 8 (End of Phase 4)
- ✅ 50+ companies tracked
- ✅ 500+ directors with relationships
- ✅ 1000+ signals with temporal trends
- ✅ Retrospective accuracy >75% on friction signals (did they correlate with director replacement?)
- ✅ Retrospective accuracy >60% on M&A risk signals (did they precede activist 13D?)
- ✅ Related-stocks signals tested: Herringer-linked stocks show >0.4 correlation
- ✅ Influence enrichment: 20+ directors with regulatory affiliations, lobbying data

### Ongoing (Post-Launch)
- Monitor signal accuracy quarterly
- Measure correlation: do high-severity signals precede market events?
- Track director careers: predict replacements, CEO transitions
- Publish "best signals of the year" analysis (what worked, what didn't)

---

## RISKS & MITIGATIONS

| Risk | Mitigation |
|------|-----------|
| Parsing failures on form variance | Emily alerts on unparsed items; Claude suggests regex refinements |
| False positives (noise signals) | Require 2+ corroborating signals before publishing; accuracy tracking |
| Name canonicalization errors | Fuzzy match + manual curation; Emily flags high-edit-distance matches |
| Survivorship bias (only public directors) | Acknowledge in docs; focus on publicly traded companies |
| Regulatory data lag | Combine EDGAR, FEC, FARA with 30-day staleness threshold |
| Trading on signals before public info | Ensure all inputs are public SEC filings; no insider info |

---

## CLAUDE CLI INVOCATION TEMPLATE

**Weekly Refinement Run**:
```bash
# Collect this week's observations
cat var/emily-observations/latest.json > /tmp/obs.json

# Generate weekly accuracy report
cat > /tmp/refine_prompt.md << 'EOF'
# 8-K Intelligence Engine: Week {N} Self-Refinement

[Emily observations above]

[Accuracy metrics below]

Please suggest:
1. New signal types (if patterns emerge)
2. Rule recalibrations (if accuracy drift detected)
3. Data source additions (if gaps are critical)
4. Code patterns for next phase

Provide YAML rules + Go code snippets where applicable.
EOF

# Invoke Claude with context
claude --dangerously-skip-permissions < /tmp/refine_prompt.md > /tmp/claude_suggestions.json

# Parse suggestions and auto-commit to rules engine
python3 scripts/apply-claude-suggestions.py /tmp/claude_suggestions.json

# Publish results
emily-agent publish-observation \
  --observation-type "claude_feedback" \
  --file /tmp/claude_suggestions.json
```

---

## IMPLEMENTATION CHECKLIST: MINIMAL VIABLE PRODUCT

**To launch Week 4 (Core extraction + Emily loop)**:

- [ ] 8-K Item 5.07 parser (Go)
- [ ] Entity graph storage (NDJSON + in-memory)
- [ ] 6 signal types implemented
- [ ] Emily observation publishing
- [ ] Claude CLI integration (manual run, not automated yet)
- [ ] Schwab 2026 8-K parses correctly + 3+ signals generated
- [ ] TCP feed export (one test consumer)

**Total new LOC**: ~2000 (Go parsing, graph building, signal scoring)

---

## FUTURE EXPANSIONS

1. **Real-time alerts** (Slack/email when high-severity signals detected)
2. **Director forensics** (deep-dive reports: "All of Herringer's board seats, ranked by risk")
3. **Predictive modeling** (ML: given current director approval %, predict replacement likelihood)
4. **Portfolio recommendations** (if high M&A risk on SCHW, reduce position; increase on cheaper peer)
5. **Activist investor tracking** (who files 13D? cross-reference with known activist profiles)
6. **IPO director preparedness** (pre-IPO company board → will directors be ready for public market scrutiny?)

---

## APPENDIX: SIGNAL TAXONOMY

**Seven Signal Families** (from Phase 3-4 roadmap):

1. **Entity Intelligence** (director friction, high-trust classification, family control, auditor changes)
2. **Vote Patterns** (failed proposals, supermajority entrenchment, abstention spikes, broker non-vote analysis)
3. **Market Relatedness** (director-linked companies, auditor peer signals, sector governance drift)
4. **Influence & Power** (regulatory revolving door, lobbying networks, political alignment)
5. **Security & Risk** (M&A defense posture, GC transitions, executive comp drift, pre-M&A signals)
6. **Temporal Trends** (vote margin decay, director replacement cadence, auditor tenure)
7. **Derived Composite** (M&A risk score, activist vulnerability score, governance health index)

---

## DOCUMENT VERSION HISTORY

| Version | Date | Author | Notes |
|---------|------|--------|-------|
| 1.0 | 2026-05-28 | Claude (FATBABY context) | Initial northstar for 8-K intelligence engine with recursive refinement |

---

**Next: Implementation begins Week 1. Feedback loop operational by Week 4.**
