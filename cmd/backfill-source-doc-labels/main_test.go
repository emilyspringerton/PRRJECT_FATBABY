package main

import (
	"testing"

	"github.com/example/prrject-fatbaby/pkg/intelligence"
)

func TestIsMislabeled_RealSECFilingWithEmptyFormAndPressReleaseType(t *testing.T) {
	doc := intelligence.SourceDocument{
		Form:        "",
		SourceType:  "press_release",
		DocumentURL: "https://www.sec.gov/Archives/edgar/data/320193/000032019326000005/aapl-20260129.htm",
	}
	if !isMislabeled(doc) {
		t.Error("expected this exact real-world broken record shape to be flagged")
	}
}

func TestIsMislabeled_CorrectlyLabeledSECFilingIsNotFlagged(t *testing.T) {
	doc := intelligence.SourceDocument{
		Form:        "8-K",
		SourceType:  "sec_8k",
		DocumentURL: "https://www.sec.gov/Archives/edgar/data/320193/000032019326000005/aapl-20260129.htm",
	}
	if isMislabeled(doc) {
		t.Error("a correctly labeled SEC filing must not be flagged")
	}
}

func TestIsMislabeled_RealPressReleaseIsNotFlagged(t *testing.T) {
	doc := intelligence.SourceDocument{
		Form:        "",
		SourceType:  "press_release",
		DocumentURL: "https://www.prnewswire.com/news-releases/some-company-announces-something.html",
	}
	if isMislabeled(doc) {
		t.Error("a genuine PR Newswire press release must not be flagged -- it's supposed to have an empty Form and source_type press_release")
	}
}

func TestIsMislabeled_NonEmptyFormIsNotFlaggedEvenIfSourceTypeSaysPressRelease(t *testing.T) {
	// Defensive: the signature requires ALL THREE conditions, not just the URL.
	doc := intelligence.SourceDocument{
		Form:        "8-K",
		SourceType:  "press_release", // hypothetically inconsistent, but Form is set -- not this bug's signature
		DocumentURL: "https://www.sec.gov/Archives/edgar/data/320193/000032019326000005/aapl-20260129.htm",
	}
	if isMislabeled(doc) {
		t.Error("a record with a non-empty Form doesn't match this specific bug's signature")
	}
}
