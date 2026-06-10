package entitygraph

import (
	"os"
	"testing"
)

func TestParseItem507_BALiveFixture(t *testing.T) {
	text, err := os.ReadFile("/tmp/ba_507_fixture.txt")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	result, err := ParseItem507(string(text))
	t.Logf("err=%v directors=%d proposals=%d auditor=%q",
		err, len(result.DirectorVotes), len(result.Proposals), result.Auditor)
	for i, p := range result.Proposals {
		t.Logf("  proposal[%d]: %q for=%d against=%d", i, p.Description, p.ForVotes, p.AgainstVotes)
	}
	if len(result.Proposals) == 0 {
		t.Errorf("expected proposals from BA filing, got 0 — splitter or vote regex failing")
	}
}
