# QUICK START: 8-K Intelligence Engine via Claude CLI
## Recursive Refinement in Practice

**This guide shows how to run the system end-to-end using Claude CLI + FATBABY.**

---

## SETUP (One-time)

### 1. Clone FATBABY and install this northstar

```bash
# Clone FATBABY (or assume you have it)
git clone https://github.com/YOUR_ORG/PRRJECT_FATBABY.git
cd PRRJECT_FATBABY

# Copy northstar files into the project
cp NORTHSTAR_8K_INTELLIGENCE_ENGINE.md ./docs/
cp CLAUDE_REFINEMENT_PROMPT.md ./docs/
```

### 2. Ensure directories exist

```bash
# Create the data structure
mkdir -p var/{secwatch,emily-observations,entity-graph,logs}
mkdir -p cmd/{entity-graph,signal-extractor,signal-observer}
```

### 3. Install Claude CLI (if not already installed)

```bash
# macOS / Linux via Homebrew
brew install anthropic/tap/claude-cli

# Or via direct download
# https://github.com/anthropics/anthropic-sdk-python/releases
```

### 4. Verify CLI access

```bash
# Test that claude CLI can access the Anthropic API
echo "Hello from FATBABY" | claude "Repeat what I said, then suggest next steps."

# You should get a response. If not, set ANTHROPIC_API_KEY:
export ANTHROPIC_API_KEY="sk-ant-..."
```

---

## PHASE 1: CORE EXTRACTION (Week 1-2)

### Step 1: Write the 8-K parser

**File**: `cmd/entity-graph/parser_8k.go`

```go
package main

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

type VoteResult struct {
	NomineeName string
	ForVotes    int64
	AgainstVotes int64
	AbstainVotes int64
	BrokerNonVotes int64
	ApprovalPct float64
}

// Parse8KVotes extracts Item 5.07 voting data from SEC filing text
func Parse8KVotes(body string) ([]VoteResult, error) {
	// Regex to capture director name and vote counts from 8-K Item 5.07
	// This is a simplified example; real regex will be more complex
	
	directorRegex := regexp.MustCompile(
		`(?i)([A-Z][a-z\-']+(?:\s+[A-Z][a-z\-']*)*(?:\s+(?:Jr|Sr|II|III|IV))?)\s+(\d+,?\d+)\s+(\d+,?\d+)\s+(\d+,?\d+)\s+(\d+,?\d+)`,
	)
	
	matches := directorRegex.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no vote results found in 8-K")
	}
	
	var results []VoteResult
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		
		name := strings.TrimSpace(match[1])
		forVotes := parseInt64(match[2])
		againstVotes := parseInt64(match[3])
		abstainVotes := parseInt64(match[4])
		brokerNonVotes := parseInt64(match[5])
		
		total := forVotes + againstVotes + abstainVotes
		approvalPct := 0.0
		if total > 0 {
			approvalPct = float64(forVotes) / float64(total)
		}
		
		results = append(results, VoteResult{
			NomineeName:    name,
			ForVotes:       forVotes,
			AgainstVotes:   againstVotes,
			AbstainVotes:   abstainVotes,
			BrokerNonVotes: brokerNonVotes,
			ApprovalPct:    approvalPct,
		})
	}
	
	return results, nil
}

func parseInt64(s string) int64 {
	s = strings.ReplaceAll(s, ",", "")
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}
```

### Step 2: Add to Emily's tooling

**Extend** `cmd/emily-agent/tools.go`:

```go
// fatbaby_parse_8k_filing parses Item 5.07 vote data
func (s *Server) ToolParse8KFiling(ctx context.Context, req *pb.ToolRequest) (*pb.ToolResponse, error) {
	if req.ParamPath == "" || req.ParamBody == "" {
		return &pb.ToolResponse{
			Text: "Error: missing ParamPath or ParamBody",
		}, nil
	}
	
	// Call the parser
	votes, err := Parse8KVotes(req.ParamBody)
	if err != nil {
		return &pb.ToolResponse{
			Text: fmt.Sprintf("Parse error: %v", err),
		}, nil
	}
	
	// Serialize to JSON
	data, _ := json.MarshalIndent(votes, "", "  ")
	return &pb.ToolResponse{
		Text: string(data),
	}, nil
}
```

### Step 3: Extract entity graph nodes

**File**: `cmd/entity-graph/graph.go`

```go
package main

import (
	"encoding/json"
	"os"
	"time"
)

type PersonNode struct {
	CanonicalID      string                 `json:"canonical_id"`
	Name             string                 `json:"name"`
	Type             string                 `json:"type"` // director, executive, auditor
	FirstAppearance  string                 `json:"first_appearance"`
	LastAppearance   string                 `json:"last_appearance"`
	Filings          []FilingAppearance     `json:"filings"`
}

type FilingAppearance struct {
	Ticker       string  `json:"ticker"`
	CIK          string  `json:"cik"`
	Form         string  `json:"form"`
	FilingDate   string  `json:"filing_date"`
	ApprovalPct  float64 `json:"approval_pct"`
	VoteCount    int64   `json:"vote_count"`
	VoteAgainst  int64   `json:"vote_against"`
}

// WriteNodeToStore appends a person node to the NDJSON store
func WriteNodeToStore(storePath string, node PersonNode) error {
	f, err := os.OpenFile(storePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	
	data, _ := json.Marshal(node)
	f.Write(data)
	f.WriteString("\n")
	return nil
}

// BuildGraph creates a graph of people + relationships from parsed filings
func BuildGraph(votes []VoteResult, ticker, cik, form, filingDate string) ([]PersonNode, error) {
	var nodes []PersonNode
	for _, vote := range votes {
		node := PersonNode{
			CanonicalID:     canonicalize(vote.NomineeName),
			Name:            vote.NomineeName,
			Type:            "director",
			FirstAppearance: filingDate,
			LastAppearance:  filingDate,
			Filings: []FilingAppearance{
				{
					Ticker:      ticker,
					CIK:         cik,
					Form:        form,
					FilingDate:  filingDate,
					ApprovalPct: vote.ApprovalPct,
					VoteCount:   vote.ForVotes,
					VoteAgainst: vote.AgainstVotes,
				},
			},
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func canonicalize(name string) string {
	// Simplified: lowercase + underscores
	// Real implementation: fuzzy matching, known aliases, etc.
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	return name
}
```

### Step 4: Signal scoring

**File**: `cmd/signal-extractor/scorer.go`

```go
package main

import "encoding/json"

type Signal struct {
	SignalID   string  `json:"signal_id"`
	Type       string  `json:"type"` // friction_director, entrenchment, m_and_a_risk, etc.
	Ticker     string  `json:"ticker"`
	Entity     string  `json:"entity"`
	Severity   string  `json:"severity"` // low, medium, high, critical
	Confidence float64 `json:"confidence"`
	Score      float64 `json:"score"`
	Comment    string  `json:"interpretation"`
}

// ScoreFrictionDirector computes a director friction signal
func ScoreFrictionDirector(name, ticker string, approvalPct float64) Signal {
	sig := Signal{
		SignalID:   "friction_" + canonicalize(name) + "_" + ticker,
		Type:       "director_friction",
		Ticker:     ticker,
		Entity:     name,
		Score:      approvalPct,
	}
	
	// Rule: approval < 85% = friction
	if approvalPct < 0.85 {
		sig.Severity = "medium"
		sig.Confidence = 0.78
		if approvalPct < 0.80 {
			sig.Severity = "high"
			sig.Confidence = 0.85
		}
		sig.Comment = "Director showing below-average approval; potential friction with board or shareholders"
	} else {
		sig.Severity = "low"
		sig.Confidence = 0.0
	}
	
	return sig
}

// ScoreEntrenchment detects governance entrenchment (failed vote despite support)
func ScoreEntrenchment(proposal string, forPct float64, supermajorityRequired float64) Signal {
	sig := Signal{
		SignalID:   "entrenchment_" + canonicalize(proposal),
		Type:       "governance_entrenchment",
		Severity:   "low",
		Confidence: 0.0,
	}
	
	if forPct > 0.80 && forPct < supermajorityRequired {
		sig.Severity = "high"
		sig.Confidence = 0.91
		sig.Comment = "Proposal has high shareholder support but fails due to supermajority threshold; indicates board entrenchment"
	}
	
	return sig
}
```

### Step 5: Test on real data

```bash
# Download Schwab's 2026 8-K (fixture)
mkdir -p fixtures/SCHW_2026
cat > fixtures/SCHW_2026/8k.txt << 'EOF'
[Paste the Schwab 8-K text from earlier analysis]
EOF

# Run the parser
go run cmd/entity-graph/*.go cmd/signal-extractor/*.go \
  --input fixtures/SCHW_2026/8k.txt \
  --ticker SCHW \
  --cik 0000086364 \
  --form 8-K \
  --filing_date 2026-05-21

# Check output
cat var/entity-graph/nodes.ndjson | head -2
cat var/entity-graph/signals.ndjson | head -5
```

**Expected output**:
```json
{"canonical_id":"marianne_c_brown","name":"Marianne C. Brown","type":"director","first_appearance":"2026-05-21","last_appearance":"2026-05-21","filings":[{"ticker":"SCHW","cik":"0000086364","form":"8-K","filing_date":"2026-05-21","approval_pct":0.979,"vote_count":1408658965,"vote_against":29435843}]}

{"signal_id":"friction_frank_c_herringer_schw","type":"director_friction","ticker":"SCHW","entity":"Frank C. Herringer","severity":"medium","confidence":0.78,"score":0.843,"interpretation":"Director showing below-average approval; potential friction with board or shareholders"}
```

---

## PHASE 2: EMILY ↔ CLAUDE LOOP (Week 3-4)

### Step 1: Create observation aggregator

**File**: `cmd/signal-observer/observer.go`

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Observation struct {
	Timestamp      string        `json:"timestamp"`
	Status         string        `json:"status"` // needs_refinement, complete, error
	Severity       string        `json:"severity"`
	Subject        string        `json:"subject"`
	Signals        []Signal      `json:"signals"`
	Gaps           []string      `json:"gaps"`
	RequestForClaude string       `json:"request_for_claude"`
}

// PublishObservation writes an observation to var/emily-observations/latest.json
func PublishObservation(obs Observation) error {
	data, _ := json.MarshalIndent(obs, "", "  ")
	return os.WriteFile("var/emily-observations/latest.json", data, 0644)
}

// SummarizeSignals aggregates signals into an observation
func SummarizeSignals(signals []Signal, ticker string) Observation {
	obs := Observation{
		Timestamp: time.Now().Format(time.RFC3339),
		Status:    "needs_refinement",
		Subject:   ticker + " Board Governance Analysis",
		Signals:   signals,
	}
	
	// Count severities
	highCount := 0
	for _, s := range signals {
		if s.Severity == "high" || s.Severity == "critical" {
			highCount++
		}
	}
	
	if highCount >= 3 {
		obs.Severity = "high"
	} else {
		obs.Severity = "medium"
	}
	
	// Identify gaps
	obs.Gaps = []string{
		"No regulatory affiliation data (need FEC/SEC history for directors)",
		"Auditor peer analysis incomplete (need to rank auditors by approval variance)",
		"Director career paths not tracked (need to extract prior board seats)",
	}
	
	obs.RequestForClaude = fmt.Sprintf(
		"Given %d signals for %s, what is the probability of activist intervention in next 12 months? Which additional data sources would improve signal accuracy?",
		len(signals), ticker,
	)
	
	return obs
}
```

### Step 2: Create the Claude refinement runner

**File**: `scripts/run-weekly-refinement.sh`

```bash
#!/bin/bash
set -euo pipefail

# Weekly refinement runner
# Collects observations and asks Claude for improvements

WEEK=$(date +%Y-W%V)
OUTPUT_DIR="var/emily-observations/refinements"
mkdir -p "$OUTPUT_DIR"

# 1. Collect metrics
echo "Collecting metrics..."
FILING_COUNT=$(cat var/secwatch/*.json 2>/dev/null | wc -l || echo "0")
DIRECTOR_COUNT=$(cat var/entity-graph/nodes.ndjson 2>/dev/null | wc -l || echo "0")
SIGNAL_COUNT=$(cat var/entity-graph/signals.ndjson 2>/dev/null | wc -l || echo "0")

# 2. Build the Claude prompt
echo "Building Claude prompt..."
cat > /tmp/refine_prompt.md << EOF
# 8-K Intelligence Engine: Week $WEEK Self-Refinement

## Current Metrics
- Filings processed: $FILING_COUNT
- Directors identified: $DIRECTOR_COUNT
- Signals generated: $SIGNAL_COUNT

## Latest Observations
$(cat var/emily-observations/latest.json 2>/dev/null || echo "No observations yet")

## Questions for You

1. Given these signals, what patterns emerge?
2. Which signals have the highest precision?
3. What new data sources should we add?

Please respond with:
- New signal rules (YAML)
- Data source recommendations
- Code patterns (Go examples)
- Accuracy improvements

Format as JSON for parsing.
EOF

# 3. Call Claude CLI
echo "Calling Claude..."
if command -v claude &> /dev/null; then
  CLAUDE_OUTPUT=$(claude --dangerously-skip-permissions < /tmp/refine_prompt.md 2>&1)
  echo "$CLAUDE_OUTPUT" > "$OUTPUT_DIR/suggestions-$WEEK.json"
  echo "✓ Claude suggestions saved to $OUTPUT_DIR/suggestions-$WEEK.json"
else
  echo "⚠ Claude CLI not found. Install with: brew install anthropic/tap/claude-cli"
  exit 1
fi

# 4. Parse and apply suggestions (pseudocode)
echo "Parsing suggestions..."
python3 - << PYTHON
import json

with open('$OUTPUT_DIR/suggestions-$WEEK.json') as f:
    suggestions = json.load(f)

# Apply new rules to signal scorer
if 'signal_recalibrations' in suggestions:
    for rec in suggestions['signal_recalibrations']:
        print(f"Recalibrating {rec['signal_type']}: {rec['new_rule']}")
        # TODO: hot-reload into signal-extractor

# Log new signals to implement
if 'new_signals_ranked' in suggestions:
    for sig in suggestions['new_signals_ranked']:
        print(f"TODO: Implement {sig['signal_type']} (effort: {sig['effort_hours']}h)")

print("Refinement complete!")
PYTHON

echo ""
echo "✓ Weekly refinement cycle complete!"
echo "Next: Review $OUTPUT_DIR/suggestions-$WEEK.json and commit changes"
```

### Step 3: Schedule the weekly run

```bash
# Add to crontab (every Monday at 09:00 UTC)
crontab -e

# Add this line:
0 9 * * 1 cd /path/to/FATBABY && bash scripts/run-weekly-refinement.sh >> var/logs/refinement.log 2>&1
```

### Step 4: Test the loop

```bash
# Manual run (dry run)
bash scripts/run-weekly-refinement.sh

# Check the output
cat var/emily-observations/refinements/suggestions-$(date +%Y-W%V).json | jq .

# Expected output:
# {
#   "version": "1.0",
#   "immediate_wins": [
#     {
#       "signal_type": "auditor_change",
#       "effort_hours": 4,
#       "accuracy_pct": 82
#     }
#   ],
#   "signal_recalibrations": [...],
#   "new_signals_ranked": [...]
# }
```

---

## PHASE 3: TCP FEED INTEGRATION (Week 5)

### Step 1: Connect to FATBABY signal API

**File**: `cmd/signal-observer/feed.go`

```go
package main

import (
	"net"
	"fmt"
)

// PublishToTCPFeed sends signals to FATBABY's TCP feed
func PublishToTCPFeed(signal Signal, feedHost string, feedPort int) error {
	addr := fmt.Sprintf("%s:%d", feedHost, feedPort)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	
	// Serialize signal as framed protocol
	data, _ := json.Marshal(signal)
	frame := fmt.Sprintf("SIGNAL %d\n%s\n", len(data), string(data))
	
	_, err = conn.Write([]byte(frame))
	return err
}
```

### Step 2: Subscribe to signals

```bash
# Test TCP subscription (from FATBABY)
fatstream subscribe "signal:*" --severity high,critical --output json
```

---

## FULL WORKFLOW: END-TO-END

```bash
# Week 1: Parse 8-K files
echo "=== Week 1: Core Extraction ==="
go run cmd/entity-graph/*.go cmd/signal-extractor/*.go \
  --input fixtures/SCHW_2026/*.txt \
  --output var/entity-graph/

# Week 2: Generate observations
echo "=== Week 2: Signal Aggregation ==="
go run cmd/signal-observer/*.go \
  --signals var/entity-graph/signals.ndjson \
  --output var/emily-observations/

# Week 3: Run Claude refinement
echo "=== Week 3: Claude Refinement ==="
bash scripts/run-weekly-refinement.sh

# Week 4: Review and commit
echo "=== Week 4: Apply Refinements ==="
# Read Claude's suggestions and implement them
cat var/emily-observations/refinements/suggestions-*.json | jq '.immediate_wins[]'

# Week 5: Publish to feed
echo "=== Week 5: TCP Feed ==="
go run cmd/signal-observer/*.go --feed-host localhost --feed-port 6379
```

---

## TROUBLESHOOTING

### "Claude CLI not found"
```bash
brew install anthropic/tap/claude-cli
# Or: pip install anthropic-cli
```

### "No observations generated"
```bash
# Check if signals were created
cat var/entity-graph/signals.ndjson | wc -l

# If empty, check parser logs
tail -100 var/logs/parser.log
```

### "Claude suggestions not parsing"
```bash
# Claude response should be valid JSON
cat var/emily-observations/refinements/suggestions-*.json | jq . || echo "Invalid JSON"

# If invalid, add `jq .` error handling to script
```

---

## NEXT: PRODUCTION DEPLOYMENT

Once the system is running consistently:

1. **Deploy to cloud** (GCP/AWS/Azure)
2. **Add monitoring** (Prometheus + Grafana)
3. **Expose HTTP API** (for traders/researchers)
4. **Integrate with trading systems** (execute on signals)
5. **Measure ROI** (signal accuracy vs market performance)

---

**Start here: `go run cmd/entity-graph/*.go < fixtures/SCHW_2026/8k.txt`**
