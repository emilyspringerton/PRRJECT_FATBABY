package buyback_test

import (
	"testing"

	"github.com/example/prrject-fatbaby/internal/buyback"
	"github.com/example/prrject-fatbaby/internal/entitygraph"
)

func TestClassify_Authorization(t *testing.T) {
	ev := buyback.Classify(
		"AAPL Announces $90 Billion Share Repurchase Program",
		"Apple Inc. announced today that the Board of Directors has authorized a new share repurchase program of up to $90 billion.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != buyback.EventAuthorization {
		t.Errorf("want authorization, got %s", ev.EventType)
	}
	if ev.AuthorizedUSD != 90e9 {
		t.Errorf("authorized USD: want 90B, got %.0f", ev.AuthorizedUSD)
	}
}

func TestClassify_Suspension(t *testing.T) {
	ev := buyback.Classify(
		"Company Suspends Share Repurchase Program",
		"In light of current market conditions, management has decided to suspend its share repurchase program.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != buyback.EventSuspension {
		t.Errorf("want suspension, got %s", ev.EventType)
	}
}

func TestClassify_Completion(t *testing.T) {
	ev := buyback.Classify(
		"MSFT Completes $60 Billion Buyback Program",
		"Microsoft announced it has completed its previously authorized $60 billion share repurchase program.",
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != buyback.EventCompletion {
		t.Errorf("want completion, got %s", ev.EventType)
	}
}

func TestClassify_Update_NoSignal(t *testing.T) {
	ev := buyback.Classify(
		"Q1 Repurchase Activity Update",
		"During Q1, the Company repurchased 1.5 million shares under its existing buyback program.",
	)
	if ev == nil {
		t.Fatal("expected event (update)")
	}
	if ev.EventType != buyback.EventUpdate {
		t.Errorf("want update, got %s", ev.EventType)
	}
	sigs := buyback.Score(ev)
	if len(sigs) != 0 {
		t.Errorf("update should produce no signals, got %d", len(sigs))
	}
}

// TestClassify_CustomerRepurchaseRate_NotABuyback is a regression test for a
// real false positive found live (S170-06): an ecommerce marketing press
// release using "repurchase rate" as a customer-retention metric, nothing to
// do with a stock buyback, got classified as EventCompletion because the
// switch statement's completion case checked "does a completion verb appear
// anywhere" AND "does a buyback word appear anywhere" independently, with no
// proximity requirement between them -- the real article text was "...the
// finish line...repurchase rate..." with no actual connection.
func TestClassify_CustomerRepurchaseRate_NotABuyback(t *testing.T) {
	ev := buyback.Classify(
		`Decile Warns of the "First-Order Payback Trap" in Beauty Ecommerce`,
		`Brands must shift focus from initial returns to long-term customer lifetime value. Beauty `+
			`brands average an 84% first-order payback rate but only a 35% repurchase rate, revealing `+
			`a major gap between acquisition efficiency and long-term revenue. The first order is not `+
			`the finish line. It is the starting line. Tactics like tailored Gift With Purchase (GWP) `+
			`programs drive a 10-20% greater repurchase rate within a six-month window.`,
	)
	if ev != nil {
		t.Errorf("expected nil (not buyback-related) for a customer-retention metric article, got %+v", ev)
	}
}

// TestClassify_ForwardLookingBoilerplate_NotABuyback is a regression test for
// a second real false positive (S170-06): a routine proxy-fight press
// release whose forward-looking-statements/risk-factors legal boilerplate
// generically lists "our share repurchase program" as one of many risk
// factors -- not an actual announcement. Same root cause as the customer-
// repurchase-rate case: the completion check had no real proximity
// requirement, so an unrelated "complet/finish/exhaust" word anywhere in a
// 20KB press release was enough.
func TestClassify_ForwardLookingBoilerplate_NotABuyback(t *testing.T) {
	ev := buyback.Classify(
		"Pacira BioSciences Reminds Stockholders to Vote the BLUE Proxy Card",
		`Forward-Looking Statements: statements in this release involve risks and uncertainties, `+
			`including those associated with determining the fair value of the company; the `+
			`anticipated funding or benefits of our share repurchase program; and factors discussed `+
			`in the "Risk Factors" section of Pacira's most recent Annual Report on Form 10-K.`,
	)
	if ev != nil && ev.EventType != buyback.EventUpdate {
		t.Errorf("expected nil or EventUpdate for boilerplate risk-factor language, got %s", ev.EventType)
	}
}

// TestClassify_RealCompletionAmongBoilerplate_StillDetected guards against
// overcorrecting the two regressions above into losing real buyback content:
// Docusign's real quarterly results genuinely reported "record share
// buybacks" and a real repurchases-of-common-stock dollar figure, alongside
// unrelated product-feature text that happens to contain the word
// "completed" ("...completed agreements..." -- not near any repurchase/
// buyback word, correctly not what should drive classification here). The
// buyback-content gate must still fire and the dollar figure must still
// parse; per this classifier's own existing convention (see
// TestClassify_Update_NoSignal), routine quarterly repurchase-activity
// mentions without an explicit authorize/suspend/complete-program statement
// are EventUpdate, not a scored event -- that's correct here too, not a
// regression.
func TestClassify_RealCompletionAmongBoilerplate_StillDetected(t *testing.T) {
	ev := buyback.Classify(
		"Docusign Announces First Quarter Fiscal 2027 Financial Results",
		`Docusign delivered strong financial results through durable revenue growth, substantial `+
			`free cash flow, and record share buybacks. Repurchases of common stock were $317.5 `+
			`million compared to $183.4 million in the same period last year. Separately, our IAM `+
			`platform lets teams monitor contracts in the background, surfacing completed agreements `+
			`and the insights they contain.`,
	)
	if ev == nil {
		t.Fatal("expected a real buyback event to still be detected")
	}
	if ev.AuthorizedUSD != 317.5e6 {
		t.Errorf("authorized USD: want 317.5M, got %.0f", ev.AuthorizedUSD)
	}
}

// TestExtractUSD_IgnoresUnrelatedEarlierDollarFigure is a regression test
// for a third real bug found alongside the two classification false
// positives (S170-06): extractUSD used to take "the first dollar amount
// anywhere in the document," with no relation to the buyback mention at
// all. A real press release reports many unrelated dollar figures (revenue,
// cash flow) before the actual buyback figure -- picking the first one
// silently mislabels an unrelated number as the buyback program's size.
func TestExtractUSD_IgnoresUnrelatedEarlierDollarFigure(t *testing.T) {
	ev := buyback.Classify(
		"Example Corp Announces First Quarter Results",
		`First Quarter Financial Highlights: Revenue was $830.2 million, a 9% year-over-year `+
			`increase. Net cash provided by operating activities was $321.7 million compared to `+
			`$251.4 million in the same period last year. Free cash flow was $289.4 million `+
			`compared to $227.8 million in the same period last year. Cash, cash equivalents, and `+
			`investments were $1.0 billion at the end of the quarter. Repurchases of common stock `+
			`were $317.5 million compared to $183.4 million in the same period last year, `+
			`completing the Company's authorized share repurchase program.`,
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.AuthorizedUSD != 317.5e6 {
		t.Errorf("AuthorizedUSD: want the buyback-adjacent figure 317.5M, got %.0f (likely picked up an unrelated earlier dollar amount)", ev.AuthorizedUSD)
	}
}

// TestClassify_NCIBRenewal_IsAuthorization is a regression test for a real
// genuine event that was misclassified for the opposite reason as the false
// positives above (S170-06): CAE's real "renewal of normal course issuer
// bid" (NCIB, standard Canadian buyback terminology) press release doesn't
// use the word "repurchase" anywhere near its actual authorize/renew verb
// -- "repurchase" only appears many sentences later describing purchase
// mechanics. reAuthorize's old tight proximity window couldn't see it at
// all; it only got (accidentally) tagged as a buyback event before this fix
// via the same loose completion-matching bug fixed above. This must be
// authorization, not silently dropped as EventUpdate.
// TestExtractUSD_ExcludesPriorPeriodComparisonFigure is a regression test
// for a fourth real bug found live while verifying the anchor-based
// extraction fix above (S170-06): anchoring dollar-figure search to the
// classification-driving regex's own match position (instead of the
// generic buyback gate) fixed picking an unrelated authorized-cap-vs-actual-
// spend mixup, but introduced a new failure mode -- "$X compared to $Y in
// the same period last year" is extremely common in earnings releases, and
// $Y (last year's figure) can sit closer to the anchor than $X (this
// period's real figure). $Y is structurally never the right number here.
func TestExtractUSD_ExcludesPriorPeriodComparisonFigure(t *testing.T) {
	ev := buyback.Classify(
		"Example Corp Announces Quarterly Results",
		`Repurchases of common stock were $317.5 million compared to $183.4 million in the same `+
			`period last year, completing the Company's authorized share repurchase program.`,
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.AuthorizedUSD != 317.5e6 {
		t.Errorf("AuthorizedUSD: want this period's figure 317.5M, got %.0f (likely picked up the prior-year comparison figure)", ev.AuthorizedUSD)
	}
}

func TestClassify_NCIBRenewal_IsAuthorization(t *testing.T) {
	ev := buyback.Classify(
		"CAE announces renewal of normal course issuer bid",
		`CAE Inc. today announced that it has received regulatory approval to renew its normal `+
			`course issuer bid ("NCIB") to purchase, for cancellation, up to 16,073,033 of its common `+
			`shares commencing June 10, 2026, and ending June 9, 2027. The maximum number of common `+
			`shares that may be repurchased under the program represents approximately five percent `+
			`of CAE's issued and outstanding shares.`,
	)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.EventType != buyback.EventAuthorization {
		t.Errorf("want authorization, got %s", ev.EventType)
	}
}

func TestClassify_NoBuyback(t *testing.T) {
	ev := buyback.Classify(
		"Company Reports Record Revenue",
		"Revenue grew 25% year over year driven by strong demand across all segments.",
	)
	if ev != nil {
		t.Errorf("expected nil for non-buyback PR, got %+v", ev)
	}
}

func TestScore_Authorization(t *testing.T) {
	ev := &buyback.BuybackEvent{
		Ticker:        "AAPL",
		EventType:     buyback.EventAuthorization,
		AuthorizedUSD: 90e9,
		PublishedAt:   "2026-06-01",
	}
	sigs := buyback.Score(ev)
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(sigs))
	}
	if sigs[0].Type != entitygraph.SignalBuybackAuthorization {
		t.Errorf("type: want buyback_authorization, got %s", sigs[0].Type)
	}
}

func TestScore_Suspension(t *testing.T) {
	ev := &buyback.BuybackEvent{
		Ticker:      "GE",
		EventType:   buyback.EventSuspension,
		PublishedAt: "2026-06-01",
	}
	sigs := buyback.Score(ev)
	if len(sigs) == 0 {
		t.Fatal("expected signal")
	}
	if sigs[0].Type != entitygraph.SignalBuybackSuspension {
		t.Errorf("type: want buyback_suspension, got %s", sigs[0].Type)
	}
	if sigs[0].Severity != entitygraph.SeverityMedium {
		t.Errorf("suspension should be medium, got %s", sigs[0].Severity)
	}
}

func TestScore_NoTicker(t *testing.T) {
	ev := &buyback.BuybackEvent{Ticker: "", EventType: buyback.EventAuthorization}
	if sigs := buyback.Score(ev); len(sigs) != 0 {
		t.Errorf("no-ticker: expected 0 signals, got %d", len(sigs))
	}
}

func TestExtractUSD_Billion(t *testing.T) {
	ev := buyback.Classify("X", "Board approved a $2.5 billion share repurchase program.")
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.AuthorizedUSD != 2.5e9 {
		t.Errorf("USD: want 2.5B, got %.0f", ev.AuthorizedUSD)
	}
}

func TestExtractUSD_Million(t *testing.T) {
	ev := buyback.Classify("X", "The company authorized a $500 million repurchase program.")
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.AuthorizedUSD != 500e6 {
		t.Errorf("USD: want 500M, got %.0f", ev.AuthorizedUSD)
	}
}
