package processor

import (
	"context"

	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

const PromptTemplate = `You are a senior hedge fund analyst. Analyze this SEC filing/Press Release for %s.
Extract key material events. Ignore boilerplate legal warnings.
Output a JSON object matching this schema:
{
  "id": "string",
  "ticker": "string",
  "timestamp": "RFC3339 timestamp",
  "signal_type": "M&A|Earnings|Legal|Leadership|Other",
  "importance": "integer 1-10",
  "sentiment": "number -1.0 to 1.0",
  "summary": "one sentence",
  "impact_analysis": "short paragraph",
  "raw_metadata": {"key":"value"}
}`

type Provider interface {
	AnalyzeText(ctx context.Context, text string) (*intelligence.Signal, error)
}
