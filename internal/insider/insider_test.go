package insider_test

import (
	"strings"
	"testing"

	"github.com/example/prrject-fatbaby/internal/insider"
)

// ── XML parsing tests ─────────────────────────────────────────────────────────

var sampleForm4XML = `<?xml version="1.0"?>
<ownershipDocument>
  <periodOfReport>2026-06-01</periodOfReport>
  <issuer>
    <issuerCik>0000320193</issuerCik>
    <issuerTradingSymbol>AAPL</issuerTradingSymbol>
  </issuer>
  <reportingOwner>
    <reportingOwnerId>
      <rptOwnerCik>0001214156</rptOwnerCik>
      <rptOwnerName>Cook Timothy D</rptOwnerName>
    </reportingOwnerId>
    <reportingOwnerRelationship>
      <isDirector>0</isDirector>
      <isOfficer>1</isOfficer>
      <isTenPercentOwner>0</isTenPercentOwner>
      <officerTitle>Chief Executive Officer</officerTitle>
    </reportingOwnerRelationship>
  </reportingOwner>
  <nonDerivativeTable>
    <nonDerivativeTransaction>
      <securityTitle><value>Common Stock</value></securityTitle>
      <transactionDate><value>2026-06-01</value></transactionDate>
      <transactionCoding>
        <transactionCode>S</transactionCode>
      </transactionCoding>
      <transactionAmounts>
        <transactionShares><value>50000</value></transactionShares>
        <transactionPricePerShare><value>185.50</value></transactionPricePerShare>
        <transactionAcquiredDisposedCode><value>D</value></transactionAcquiredDisposedCode>
      </transactionAmounts>
    </nonDerivativeTransaction>
    <nonDerivativeTransaction>
      <securityTitle><value>Common Stock</value></securityTitle>
      <transactionDate><value>2026-06-02</value></transactionDate>
      <transactionCoding>
        <transactionCode>A</transactionCode>
      </transactionCoding>
      <transactionAmounts>
        <transactionShares><value>10000</value></transactionShares>
        <transactionPricePerShare><value>0</value></transactionPricePerShare>
        <transactionAcquiredDisposedCode><value>A</value></transactionAcquiredDisposedCode>
      </transactionAmounts>
    </nonDerivativeTransaction>
  </nonDerivativeTable>
</ownershipDocument>`

func TestParseForm4XML_Basic(t *testing.T) {
	txns, err := insider.ParseForm4XML([]byte(sampleForm4XML), "AAPL", "0000320193", "0001234567-26-000001", "2026-06-03")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}
	sale := txns[0]
	if sale.Code != insider.CodeSale {
		t.Errorf("expected code S, got %q", sale.Code)
	}
	if sale.Shares != 50000 {
		t.Errorf("expected 50000 shares, got %v", sale.Shares)
	}
	if sale.PricePerShare != 185.50 {
		t.Errorf("expected price 185.50, got %v", sale.PricePerShare)
	}
	if sale.ValueUSD != 50000*185.50 {
		t.Errorf("expected value %.2f, got %.2f", 50000*185.50, sale.ValueUSD)
	}
	if !sale.Role.IsOfficer {
		t.Error("expected IsOfficer=true")
	}
	if sale.Role.Title != "Chief Executive Officer" {
		t.Errorf("unexpected title: %q", sale.Role.Title)
	}
	if sale.OwnerName != "Cook Timothy D" {
		t.Errorf("unexpected owner: %q", sale.OwnerName)
	}
	award := txns[1]
	if award.Code != insider.CodeAward {
		t.Errorf("expected code A, got %q", award.Code)
	}
}

func TestParseForm4XML_EmptyDoc(t *testing.T) {
	// Empty nonDerivativeTable should return no transactions.
	xml := `<?xml version="1.0"?><ownershipDocument>
		<periodOfReport>2026-06-01</periodOfReport>
		<issuer><issuerCik>0000320193</issuerCik><issuerTradingSymbol>AAPL</issuerTradingSymbol></issuer>
		<reportingOwner>
			<reportingOwnerId><rptOwnerCik>0001</rptOwnerCik><rptOwnerName>John</rptOwnerName></reportingOwnerId>
			<reportingOwnerRelationship><isDirector>1</isDirector><isOfficer>0</isOfficer><isTenPercentOwner>0</isTenPercentOwner></reportingOwnerRelationship>
		</reportingOwner>
		<nonDerivativeTable></nonDerivativeTable>
	</ownershipDocument>`
	txns, err := insider.ParseForm4XML([]byte(xml), "AAPL", "0000320193", "acc-001", "2026-06-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txns) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(txns))
	}
}

func TestParseForm4XML_InvalidXML(t *testing.T) {
	_, err := insider.ParseForm4XML([]byte("<not valid xml"), "AAPL", "0000320193", "acc", "2026-06-01")
	if err == nil {
		t.Error("expected error for invalid XML")
	}
}

func TestTransactionCode_IsConviction(t *testing.T) {
	cases := []struct {
		code       insider.TransactionCode
		conviction bool
	}{
		{insider.CodePurchase, true},
		{insider.CodeSale, true},
		{insider.CodeAward, false},
		{insider.CodeOptionExercise, false},
		{insider.CodeTaxWithholding, false},
		{insider.CodeGift, false},
	}
	for _, c := range cases {
		if got := c.code.IsConviction(); got != c.conviction {
			t.Errorf("code %q: IsConviction()=%v want %v", c.code, got, c.conviction)
		}
	}
}

// ── Signal scoring tests ──────────────────────────────────────────────────────

func makeTx(ticker, code, ownerCIK, ownerName, date string, shares, price float64, isOfficer, isDirector bool, title string) insider.InsiderTransaction {
	return insider.InsiderTransaction{
		Ticker:          ticker,
		Code:            insider.TransactionCode(code),
		OwnerCIK:        ownerCIK,
		OwnerName:       ownerName,
		TransactionDate: date,
		FilingDate:      date,
		Shares:          shares,
		PricePerShare:   price,
		ValueUSD:        shares * price,
		Role: insider.InsiderRole{
			IsOfficer:  isOfficer,
			IsDirector: isDirector,
			Title:      title,
		},
	}
}

func TestScoreInsiderActivity_SellCluster(t *testing.T) {
	txns := []insider.InsiderTransaction{
		makeTx("SCHW", "S", "cik1", "Alice", "2026-05-01", 10000, 80.0, true, false, "CFO"),
		makeTx("SCHW", "S", "cik2", "Bob", "2026-05-05", 20000, 81.0, true, false, "CEO"),
		makeTx("SCHW", "S", "cik3", "Carol", "2026-05-10", 5000, 82.0, false, true, ""),
	}
	sigs := insider.ScoreInsiderActivity(txns, "SCHW")
	var cluster bool
	for _, s := range sigs {
		if string(s.Type) == "insider_sell_cluster" {
			cluster = true
			if s.Severity != "high" {
				t.Errorf("sell cluster severity: want high, got %s", s.Severity)
			}
		}
	}
	if !cluster {
		t.Error("expected insider_sell_cluster signal, got none")
	}
}

func TestScoreInsiderActivity_NoBelowThreshold(t *testing.T) {
	// Only 2 sellers — below the 3-seller threshold.
	txns := []insider.InsiderTransaction{
		makeTx("AAPL", "S", "cik1", "Alice", "2026-05-01", 10000, 185.0, true, false, "CFO"),
		makeTx("AAPL", "S", "cik2", "Bob", "2026-05-03", 5000, 186.0, true, false, "COO"),
	}
	sigs := insider.ScoreInsiderActivity(txns, "AAPL")
	for _, s := range sigs {
		if string(s.Type) == "insider_sell_cluster" {
			t.Error("should not emit sell cluster for 2 sellers")
		}
	}
}

func TestScoreInsiderActivity_NotableBuy(t *testing.T) {
	txns := []insider.InsiderTransaction{
		makeTx("MSFT", "P", "cik1", "Satya Nadella", "2026-06-01", 5000, 420.0, true, false, "Chief Executive Officer"),
	}
	sigs := insider.ScoreInsiderActivity(txns, "MSFT")
	var found bool
	for _, s := range sigs {
		if string(s.Type) == "insider_buy" {
			found = true
			if !strings.Contains(s.Interpretation, "Satya Nadella") {
				t.Errorf("interpretation should mention owner name: %s", s.Interpretation)
			}
		}
	}
	if !found {
		t.Error("expected insider_buy signal for C-level purchase")
	}
}

func TestScoreInsiderActivity_BelowBuyThreshold(t *testing.T) {
	// Non-C-level director buying < $100k.
	txns := []insider.InsiderTransaction{
		makeTx("GE", "P", "cik9", "Jane Director", "2026-06-01", 100, 50.0, false, true, ""),
	}
	sigs := insider.ScoreInsiderActivity(txns, "GE")
	for _, s := range sigs {
		if string(s.Type) == "insider_buy" {
			t.Error("should not emit insider_buy below $100k for non-C-level")
		}
	}
}

func TestScoreInsiderActivity_AwardIgnored(t *testing.T) {
	// Award grants should not produce conviction signals.
	txns := []insider.InsiderTransaction{
		makeTx("AAPL", "A", "cik1", "Tim Cook", "2026-06-01", 100000, 185.0, true, false, "Chief Executive Officer"),
		makeTx("AAPL", "A", "cik2", "Luca Maestri", "2026-06-01", 50000, 185.0, true, false, "CFO"),
		makeTx("AAPL", "A", "cik3", "Kate Adams", "2026-06-01", 30000, 185.0, true, false, "General Counsel"),
	}
	sigs := insider.ScoreInsiderActivity(txns, "AAPL")
	if len(sigs) != 0 {
		t.Errorf("award transactions should produce no conviction signals, got %d", len(sigs))
	}
}

func TestScoreInsiderActivity_ClusterOnlyWithin30Days(t *testing.T) {
	// Three sellers but spread over 60 days — should NOT form a cluster.
	txns := []insider.InsiderTransaction{
		makeTx("SCHW", "S", "cik1", "A", "2026-01-01", 10000, 80.0, true, false, "CFO"),
		makeTx("SCHW", "S", "cik2", "B", "2026-02-01", 10000, 80.0, true, false, "CEO"),
		makeTx("SCHW", "S", "cik3", "C", "2026-03-01", 10000, 80.0, false, true, ""),
	}
	sigs := insider.ScoreInsiderActivity(txns, "SCHW")
	for _, s := range sigs {
		if string(s.Type) == "insider_sell_cluster" {
			t.Error("60-day spread should not trigger 30-day cluster")
		}
	}
}
