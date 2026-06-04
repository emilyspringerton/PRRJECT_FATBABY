package main

import (
	"testing"
)

func TestClassifyReason_Restatement(t *testing.T) {
	reason, _ := classifyReason("The Company is unable to file timely due to the need to restate prior period financial statements.")
	if reason != ReasonRestatement {
		t.Errorf("want restatement, got %s", reason)
	}
}

func TestClassifyReason_MaterialWeakness(t *testing.T) {
	reason, _ := classifyReason("Identification of a material weakness in internal controls over financial reporting requires additional review.")
	if reason != ReasonMaterialWeakness {
		t.Errorf("want material_weakness, got %s", reason)
	}
}

func TestClassifyReason_AuditorDispute(t *testing.T) {
	reason, _ := classifyReason("The registrant requires additional time to resolve matters with its independent public accounting firm.")
	if reason != ReasonAuditorDispute {
		t.Errorf("want auditor_dispute, got %s", reason)
	}
}

func TestClassifyReason_SystemTransition(t *testing.T) {
	reason, _ := classifyReason("The company recently completed an ERP system implementation and requires additional time.")
	if reason != ReasonSystemTransition {
		t.Errorf("want system_transition, got %s", reason)
	}
}

func TestClassifyReason_AdditionalReview(t *testing.T) {
	reason, _ := classifyReason("The registrant needs additional time to complete its review of complex transactions.")
	if reason != ReasonAdditionalReview {
		t.Errorf("want additional_review, got %s", reason)
	}
}

func TestClassifyReason_Unknown(t *testing.T) {
	reason, _ := classifyReason("The registrant was unable to file by the required deadline.")
	if reason != ReasonUnknown {
		t.Errorf("want unknown, got %s", reason)
	}
}

func TestScoreNTFiling_Restatement_HighSeverity(t *testing.T) {
	f := NTFiling{
		Ticker:     "GE",
		FormType:   "NT 10-K",
		FilingDate: "2026-06-01",
		Reason:     ReasonRestatement,
	}
	sig := scoreNTFiling(f)
	if sig.Severity != "high" {
		t.Errorf("restatement should be high severity, got %s", sig.Severity)
	}
}

func TestScoreNTFiling_Unknown_MediumSeverity(t *testing.T) {
	f := NTFiling{
		Ticker:     "GE",
		FormType:   "NT 10-Q",
		FilingDate: "2026-06-01",
		Reason:     ReasonUnknown,
	}
	sig := scoreNTFiling(f)
	if sig.Severity != "medium" {
		t.Errorf("unknown reason should be medium severity, got %s", sig.Severity)
	}
}

func TestPadCIK(t *testing.T) {
	cases := []struct{ in, want string }{
		{"320193", "0000320193"},
		{"0000320193", "0000320193"},
		{"1", "0000000001"},
	}
	for _, c := range cases {
		if got := padCIK(c.in); got != c.want {
			t.Errorf("padCIK(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
