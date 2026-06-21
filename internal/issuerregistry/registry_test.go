package issuerregistry_test

import (
	"testing"

	"github.com/example/prrject-fatbaby/internal/identity"
	"github.com/example/prrject-fatbaby/internal/issuerregistry"
)

func TestNew(t *testing.T) {
	refs := map[string][]identity.SecurityRef{
		"0000789019": {
			{Exchange: "NASDAQ", Symbol: "MSFT", CIK: "0000789019", Source: "sec", Confidence: 1.0},
		},
	}
	reg := issuerregistry.New(refs)
	if reg == nil {
		t.Fatal("New returned nil")
	}
	// Modifying the input should not affect the registry (deep copy).
	refs["0000789019"][0].Symbol = "MODIFIED"
	got := reg.ResolveByCIK("0000789019")
	if len(got) == 0 || got[0].Symbol != "MSFT" {
		t.Errorf("deep copy: got Symbol=%q, want MSFT", got[0].Symbol)
	}
}

func TestResolveByCIK(t *testing.T) {
	reg := issuerregistry.New(map[string][]identity.SecurityRef{
		"0000789019": {
			{Symbol: "MSFT", Source: "sec", Confidence: 1.0},
		},
		"0000320193": {
			{Symbol: "AAPL", Source: "sec", Confidence: 1.0},
		},
	})

	got := reg.ResolveByCIK("0000789019")
	if len(got) != 1 || got[0].Symbol != "MSFT" {
		t.Errorf("ResolveByCIK(MSFT CIK): %v, want [{Symbol:MSFT}]", got)
	}
	got2 := reg.ResolveByCIK("0000320193")
	if len(got2) != 1 || got2[0].Symbol != "AAPL" {
		t.Errorf("ResolveByCIK(AAPL CIK): %v, want [{Symbol:AAPL}]", got2)
	}
	// Unknown CIK → nil or empty.
	got3 := reg.ResolveByCIK("DOES-NOT-EXIST")
	if len(got3) != 0 {
		t.Errorf("unknown CIK: want empty, got %v", got3)
	}
}

func TestResolveReturnsCopy(t *testing.T) {
	reg := issuerregistry.New(map[string][]identity.SecurityRef{
		"0000789019": {
			{Symbol: "MSFT", Source: "sec", Confidence: 1.0},
		},
	})
	got := reg.ResolveByCIK("0000789019")
	got[0].Symbol = "MUTATED"
	// Second call should still return the original.
	got2 := reg.ResolveByCIK("0000789019")
	if got2[0].Symbol != "MSFT" {
		t.Errorf("resolve should return a copy; got %q after mutation", got2[0].Symbol)
	}
}

func TestNilRegistry(t *testing.T) {
	var reg *issuerregistry.IssuerRegistry
	if got := reg.ResolveByCIK("any"); len(got) != 0 {
		t.Errorf("nil registry: want empty, got %v", got)
	}
}
