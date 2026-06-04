package main

import (
	"strings"
	"testing"

	"github.com/example/prrject-fatbaby/internal/eps"
)

func TestCaseID_Stable(t *testing.T) {
	p := eps.EarningsPeriod{FiscalQuarter: "Q3", FiscalYear: 2026}
	id1 := caseID("pr:abc123", p)
	id2 := caseID("pr:abc123", p)
	if id1 != id2 {
		t.Errorf("caseID not stable: %q != %q", id1, id2)
	}
}

func TestCaseID_HasPrefix(t *testing.T) {
	p := eps.EarningsPeriod{FiscalQuarter: "Q1", FiscalYear: 2027}
	id := caseID("pr:xyz", p)
	if !strings.HasPrefix(id, "eps:") {
		t.Errorf("caseID should start with 'eps:', got %q", id)
	}
}

func TestCaseID_DifferentInputsGiveDifferentIDs(t *testing.T) {
	p := eps.EarningsPeriod{FiscalQuarter: "Q2", FiscalYear: 2026}
	id1 := caseID("pr:aaa", p)
	id2 := caseID("pr:bbb", p)
	if id1 == id2 {
		t.Errorf("different source identities produced same caseID: %q", id1)
	}
	// Same source, different period.
	p2 := eps.EarningsPeriod{FiscalQuarter: "Q3", FiscalYear: 2026}
	id3 := caseID("pr:aaa", p2)
	if id1 == id3 {
		t.Errorf("different periods produced same caseID: %q", id1)
	}
}

func TestCaseID_Format(t *testing.T) {
	p := eps.EarningsPeriod{FiscalQuarter: "Q4", FiscalYear: 2025}
	id := caseID("pr:test", p)
	// Should be "eps:" followed by 16 hex chars (8 bytes of SHA-256).
	const prefix = "eps:"
	if !strings.HasPrefix(id, prefix) {
		t.Fatalf("wrong prefix: %q", id)
	}
	hex := id[len(prefix):]
	if len(hex) != 16 {
		t.Errorf("hex part should be 16 chars (8 bytes), got %d: %q", len(hex), hex)
	}
	for _, c := range hex {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("non-hex char %q in caseID: %q", c, id)
		}
	}
}
