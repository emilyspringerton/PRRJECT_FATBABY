package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

// intentForSource returns the THE_FIELD intent string for a given SEC source type.
// Each source type maps to a spirit combination that best resonates with the analysis goal.
func intentForSource(sourceType, form string) string {
	switch sourceType {
	case "edgar":
		switch {
		case strings.Contains(form, "8-K"):
			// Aim #23 (burn illusions/expose fraud) + Eligos #15 (strategic foresight)
			return "signal fraud analysis 8-K material event pyromancy strategic intelligence"
		case strings.Contains(form, "10-K"), strings.Contains(form, "10-Q"):
			// Eligos #15 (strategic foresight) + Vassago #03 (foresight) + Marax #21 (systematic mastery)
			return "strategic foresight analysis systematic intelligence financial research"
		case strings.Contains(form, "SC 13D"), strings.Contains(form, "SC 13G"):
			// Bune #26 (wealth synthesis) + Aim #23 (fraud detection)
			return "wealth market governance signal fraud hidden intelligence"
		case strings.Contains(form, "DEF 14A"), strings.Contains(form, "PROXY"):
			// Orobas #55 (truth under pressure) + Botis #17 (reconciliation)
			return "governance truth integrity justice reconciliation decision"
		default:
			// General intelligence routing: Vassago + Eligos
			return "intelligence analysis strategic foresight signal"
		}
	case "pr_newswire", "pr-newswire", "business_wire":
		// Forneus #30 (reputation alchemy) + Vassago #03 (foresight)
		return "reputation social persuasion synthesis foresight market signal"
	default:
		return "intelligence analysis signal market foresight"
	}
}

// archetypeInvokeRequest matches the POST /invoke body for the archetype-engine.
type archetypeInvokeRequest struct {
	Intent        string `json:"intent"`
	Context       string `json:"context"`
	AllowCollapse bool   `json:"allow_collapse"`
}

// archetypeInvokeResponse is the JSON from POST /invoke.
type archetypeInvokeResponse struct {
	Output         string `json:"output"`
	E1Output       string `json:"e1_output"`
	E2Output       string `json:"e2_output"`
	ResonanceState string `json:"resonance_state"`
	PhaseDeltaDeg  float64 `json:"phase_delta_deg"`
	E1Weight       float64 `json:"e1_weight"`
}

// ArchetypeProvider implements Provider using the THE_FIELD archetype-engine HTTP service.
// It routes each filing type through a spirit-stack selected by intentForSource, then
// extracts a Signal from the synthesized E₁/E₂ output via claude-haiku JSON parsing.
type ArchetypeProvider struct {
	// EngineURL is the base URL of the archetype-engine service (default: http://localhost:8090).
	EngineURL string
	// Fallback is called when the archetype engine is unavailable. Usually HaikuProvider.
	Fallback Provider
	// HaikuProvider is used to parse the synthesized output into a Signal.
	Haiku *HaikuProvider

	http *http.Client
}

// NewArchetypeProvider creates an ArchetypeProvider.
// engineURL: archetype-engine address, e.g. "http://localhost:8090"
// haiku: used to parse the synthesized text output into a Signal
// fallback: Provider to use if the engine is unreachable (pass haiku for seamless degradation)
func NewArchetypeProvider(engineURL string, haiku *HaikuProvider, fallback Provider) *ArchetypeProvider {
	return &ArchetypeProvider{
		EngineURL: engineURL,
		Fallback:  fallback,
		Haiku:     haiku,
		http:      &http.Client{Timeout: 90 * time.Second},
	}
}

// AnalyzeText routes the text through the archetype engine then parses a Signal.
// The text is expected to be prefixed with `source_type=X\nform=Y\n\n` (as written by stub-backfill).
func (a *ArchetypeProvider) AnalyzeText(ctx context.Context, text string) (*intelligence.Signal, error) {
	sourceType, form := parseSourceMeta(text)
	intent := intentForSource(sourceType, form)

	// Build the context block: the raw text plus the base analyst prompt.
	contextBlock := fmt.Sprintf(PromptTemplate, "the SEC filing/Press Release below") +
		"\n\nDo not include any text outside the JSON object.\n\n" + text

	raw, err := json.Marshal(archetypeInvokeRequest{
		Intent:        intent,
		Context:       contextBlock,
		AllowCollapse: false,
	})
	if err != nil {
		return a.fallbackAnalyze(ctx, text, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.EngineURL+"/invoke", bytes.NewReader(raw))
	if err != nil {
		return a.fallbackAnalyze(ctx, text, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		// Engine not running — degrade to fallback.
		return a.fallbackAnalyze(ctx, text, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return a.fallbackAnalyze(ctx, text, err)
	}

	var invokeResp archetypeInvokeResponse
	if err := json.Unmarshal(body, &invokeResp); err != nil || invokeResp.Output == "" {
		return a.fallbackAnalyze(ctx, text, fmt.Errorf("archetype invoke decode: %w", err))
	}

	// Extract JSON Signal from the synthesized output.
	sig, err := parseSignalFromText(invokeResp.Output)
	if err != nil {
		// Synthesized output may not be clean JSON — ask haiku to parse it.
		if a.Haiku != nil {
			sig, err = a.Haiku.AnalyzeText(ctx, invokeResp.Output)
			if err == nil && sig != nil {
				if sig.RawMetadata == nil {
					sig.RawMetadata = make(map[string]string)
				}
				sig.RawMetadata["resonance_state"] = invokeResp.ResonanceState
				sig.RawMetadata["phase_delta_deg"] = fmt.Sprintf("%.1f", invokeResp.PhaseDeltaDeg)
				sig.RawMetadata["provider"] = "archetype"
				return sig, nil
			}
		}
		return a.fallbackAnalyze(ctx, text, err)
	}

	if sig.RawMetadata == nil {
		sig.RawMetadata = make(map[string]string)
	}
	sig.RawMetadata["resonance_state"] = invokeResp.ResonanceState
	sig.RawMetadata["phase_delta_deg"] = fmt.Sprintf("%.1f", invokeResp.PhaseDeltaDeg)
	sig.RawMetadata["intent"] = intent
	sig.RawMetadata["provider"] = "archetype"
	return sig, nil
}

func (a *ArchetypeProvider) fallbackAnalyze(ctx context.Context, text string, cause error) (*intelligence.Signal, error) {
	if a.Fallback != nil {
		return a.Fallback.AnalyzeText(ctx, text)
	}
	return nil, fmt.Errorf("archetype engine failed and no fallback: %w", cause)
}

// parseSignalFromText tries to extract a JSON Signal from arbitrary text output.
func parseSignalFromText(text string) (*intelligence.Signal, error) {
	raw := strings.TrimSpace(text)
	// Strip markdown code fence.
	if strings.HasPrefix(raw, "```") {
		raw = raw[strings.Index(raw, "\n")+1:]
		if idx := strings.LastIndex(raw, "```"); idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimSpace(raw)
	}
	// Find the first JSON object in the output.
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in archetype output")
	}
	raw = raw[start : end+1]
	var sig intelligence.Signal
	if err := json.Unmarshal([]byte(raw), &sig); err != nil {
		return nil, fmt.Errorf("parse signal from archetype output: %w", err)
	}
	return &sig, nil
}

// parseSourceMeta extracts source_type and form from the text prefix written by stub-backfill.
// Format: "source_type=X\nform=Y\n\n<content>"
func parseSourceMeta(text string) (sourceType, form string) {
	for _, line := range strings.SplitN(text, "\n", 4) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "source_type=") {
			sourceType = strings.TrimPrefix(line, "source_type=")
		} else if strings.HasPrefix(line, "form=") {
			form = strings.TrimPrefix(line, "form=")
		}
	}
	return
}
