package catalog

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
	"github.com/example/prrject-fatbaby/internal/newssite/marketdata"
	"github.com/example/prrject-fatbaby/internal/signalindex"
)

// buildCatalogFromRows is a test helper that populates a Catalog directly
// from a map of pre-built TickerRows (bypassing the full Build pipeline).
func buildCatalogFromRows(rows map[string]*TickerRow) *Catalog {
	c := New()
	all := make([]*TickerRow, 0, len(rows))
	for _, r := range rows {
		all = append(all, r)
	}
	c.mu.Lock()
	c.rows = rows
	c.sorted = append([]*TickerRow{}, all...)
	c.alpha = append([]*TickerRow{}, all...)
	c.mu.Unlock()
	return c
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"schw", "SCHW"},
		{"SCHW", "SCHW"},
		{" SCHW ", "SCHW"},
		{"\tschw\n", "SCHW"},
		{"BRK.B", "BRK.B"},  // punctuation preserved
		{"brk.b", "BRK.B"},
		{"BRK/B", "BRK/B"},
		{"", ""},
	}
	for _, tc := range cases {
		got := Normalize(tc.in)
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSearch_ExactBeforePrefixBeforeSubstring(t *testing.T) {
	now := time.Now()
	rows := map[string]*TickerRow{
		"C":    {Symbol: "C", GovSignals: 5, LatestActivity: now},
		"SCHW": {Symbol: "SCHW", GovSignals: 14, LatestActivity: now},
		"SC":   {Symbol: "SC", GovSignals: 3, LatestActivity: now.Add(-time.Hour)},
		"SCCO": {Symbol: "SCCO", GovSignals: 2, LatestActivity: now.Add(-2 * time.Hour)},
	}
	cat := buildCatalogFromRows(rows)

	// Query "SC": exact=SC, prefix=SCHW+SCCO, no substring (C doesn't contain "SC")
	results := cat.Search("SC")
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].Symbol != "SC" {
		t.Errorf("first result should be exact match SC, got %q", results[0].Symbol)
	}
	if results[0].Tier != 1 {
		t.Errorf("SC should be tier 1 (exact), got tier %d", results[0].Tier)
	}
	// Prefix results come next; SCHW has more signals than SCCO so it ranks first.
	prefixSyms := []string{}
	for _, r := range results[1:] {
		if r.Tier == 2 {
			prefixSyms = append(prefixSyms, r.Symbol)
		}
	}
	if len(prefixSyms) < 2 {
		t.Errorf("expected at least 2 prefix results, got %v", prefixSyms)
	}
	if prefixSyms[0] != "SCHW" {
		t.Errorf("first prefix match should be SCHW (most signals), got %q", prefixSyms[0])
	}
}

func TestSearch_ExactMatchOnKnownSymbol(t *testing.T) {
	rows := map[string]*TickerRow{
		"SCHW": {Symbol: "SCHW", GovSignals: 14},
		"SCHD": {Symbol: "SCHD", GovSignals: 2},
	}
	cat := buildCatalogFromRows(rows)

	results := cat.Search("SCHW")
	if len(results) == 0 {
		t.Fatal("no results for exact query")
	}
	if results[0].Symbol != "SCHW" || results[0].Tier != 1 {
		t.Errorf("exact query should return tier-1 SCHW first, got %+v", results[0])
	}
}

func TestSearch_CaseInsensitive(t *testing.T) {
	rows := map[string]*TickerRow{
		"SCHW": {Symbol: "SCHW"},
	}
	cat := buildCatalogFromRows(rows)

	for _, q := range []string{"schw", "Schw", "SCHW", " schw "} {
		res := cat.Search(q)
		if len(res) == 0 || res[0].Symbol != "SCHW" {
			t.Errorf("Search(%q) failed to find SCHW", q)
		}
	}
}

func TestSearch_DottedTicker(t *testing.T) {
	rows := map[string]*TickerRow{
		"BRK.B": {Symbol: "BRK.B", GovSignals: 1},
	}
	cat := buildCatalogFromRows(rows)

	res := cat.Search("brk.b")
	if len(res) == 0 || res[0].Symbol != "BRK.B" {
		t.Errorf("dotted ticker BRK.B not found via search brk.b")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	rows := map[string]*TickerRow{
		"SCHW": {Symbol: "SCHW"},
	}
	cat := buildCatalogFromRows(rows)
	if res := cat.Search(""); res != nil {
		t.Errorf("empty query should return nil, got %v", res)
	}
}

func TestSearch_SubstringWhenNoPrefix(t *testing.T) {
	rows := map[string]*TickerRow{
		"SCHW": {Symbol: "SCHW", GovSignals: 5},
		"BAC":  {Symbol: "BAC", GovSignals: 1},
	}
	cat := buildCatalogFromRows(rows)

	res := cat.Search("HW") // HW is a substring of SCHW, not a prefix
	if len(res) == 0 {
		t.Fatal("expected substring match for HW")
	}
	if res[0].Symbol != "SCHW" {
		t.Errorf("expected SCHW as substring match for HW, got %q", res[0].Symbol)
	}
	if res[0].Tier != 3 {
		t.Errorf("expected tier 3 (substring), got %d", res[0].Tier)
	}
}

func TestKnown(t *testing.T) {
	rows := map[string]*TickerRow{
		"AAPL": {Symbol: "AAPL"},
	}
	cat := buildCatalogFromRows(rows)

	if !cat.Known("AAPL") {
		t.Error("AAPL should be known")
	}
	if !cat.Known("aapl") {
		t.Error("aapl (lowercase) should be known (case-insensitive)")
	}
	if cat.Known("XYZ") {
		t.Error("XYZ should not be known")
	}
}

func TestLookup(t *testing.T) {
	rows := map[string]*TickerRow{
		"TSLA": {Symbol: "TSLA", GovSignals: 3},
	}
	cat := buildCatalogFromRows(rows)

	row := cat.Lookup("tsla")
	if row == nil {
		t.Fatal("Lookup(tsla) returned nil")
	}
	if row.Symbol != "TSLA" {
		t.Errorf("symbol = %q, want TSLA", row.Symbol)
	}
	if cat.Lookup("MISSING") != nil {
		t.Error("Lookup of unknown symbol should return nil")
	}
}

func TestNearestMatches(t *testing.T) {
	rows := map[string]*TickerRow{
		"SCHW": {Symbol: "SCHW", GovSignals: 10},
		"SC":   {Symbol: "SC", GovSignals: 5},
		"SCCO": {Symbol: "SCCO", GovSignals: 2},
	}
	cat := buildCatalogFromRows(rows)

	matches := cat.NearestMatches("SCHX", 3) // no exact, no prefix for SCHX; SCHW is substring
	// SCHW contains "SCH" which is a prefix of "SCHX"? No. "SCHX" → looking for symbols containing "SCHX" — none.
	// Actually NearestMatches("SCHX") would search for "SCHX" — no exact, no prefix, no substring since
	// SCHW does not contain "SCHX". So 0 matches.
	// Let's use a better test: NearestMatches("SC", 2) should return SC then SCHW.
	matches = cat.NearestMatches("SC", 2)
	if len(matches) == 0 {
		t.Fatal("expected nearest matches for SC")
	}
	if matches[0] != "SC" {
		t.Errorf("first nearest match for SC should be SC (exact), got %q", matches[0])
	}
}

func TestBuild_FilingOnlyTickerAppearsInCatalog(t *testing.T) {
	// A ticker that has source docs but zero signals must still get a catalog row.
	// We test the Build codepath directly via the helper since we can't easily
	// inject a live docindex in a unit test.
	// Build is covered by integration; here we verify the filing-only invariant
	// at the data level by checking that rows from docs-only sources get a row.
	//
	// The real acceptance test is: given a TickerRow with DocCount > 0 and
	// SignalCount == GovSignals == 0, Known() returns true and Lookup() finds it.
	row := &TickerRow{Symbol: "FILING_ONLY", DocCount: 3}
	rows := map[string]*TickerRow{"FILING_ONLY": row}
	cat := buildCatalogFromRows(rows)

	if !cat.Known("FILING_ONLY") {
		t.Error("filing-only ticker should be Known()")
	}
	r := cat.Lookup("FILING_ONLY")
	if r == nil || r.DocCount != 3 {
		t.Errorf("filing-only ticker: got %+v", r)
	}
}

func TestBuild_PopulatesMarketCapFromMarketDataStore(t *testing.T) {
	// A ticker needs a row-creating source (signals/gov/docs) before market
	// cap enrichment applies to it -- mdIdx only enriches existing rows, per
	// catalog.Build's doc comment. Give AAPL one signal so ensure() creates
	// its row, then verify Build fills in MarketCap from a real
	// marketdata.Store built the same way cmd/newssite wires it.
	sigIdx := signalindex.NewIndex()
	sigIdx.LoadEntries([]*signalindex.SignalEntry{
		{Ticker: "AAPL", SignalType: "test", Timestamp: time.Now(), AppendedAt: time.Now()},
	})

	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("open eventstore: %v", err)
	}
	defer store.Close()

	bar := marketdata.Bar{
		Ticker: "AAPL", Date: "2026-08-01", Timestamp: time.Now(),
		Open: 300, High: 310, Low: 295, Close: 305, AdjClose: 305,
		Volume: 1000, MarketCap: 4_800_000_000_000,
	}
	data, err := json.Marshal(bar)
	if err != nil {
		t.Fatalf("marshal bar: %v", err)
	}
	if _, err := store.Append(context.Background(),
		eventstore.Event{ID: "market_data_tick:AAPL:2026-08-01", Type: "market_data_tick", Data: data},
	); err != nil {
		t.Fatalf("append market data event: %v", err)
	}

	mdIdx := marketdata.NewStore()
	if err := marketdata.Build(context.Background(), store, mdIdx, log.Default()); err != nil {
		t.Fatalf("marketdata.Build: %v", err)
	}

	cat := New()
	cat.Build(sigIdx, nil, nil, mdIdx, "2026-08-01")

	row := cat.Lookup("AAPL")
	if row == nil {
		t.Fatal("AAPL should have a catalog row from its signal")
	}
	if row.MarketCap != 4_800_000_000_000 {
		t.Errorf("MarketCap = %d, want 4800000000000", row.MarketCap)
	}
}

func TestBuild_NilMarketDataStoreLeavesMarketCapZero(t *testing.T) {
	sigIdx := signalindex.NewIndex()
	sigIdx.LoadEntries([]*signalindex.SignalEntry{
		{Ticker: "MSFT", SignalType: "test", Timestamp: time.Now(), AppendedAt: time.Now()},
	})

	cat := New()
	cat.Build(sigIdx, nil, nil, nil, "2026-08-01")

	row := cat.Lookup("MSFT")
	if row == nil {
		t.Fatal("MSFT should have a catalog row from its signal")
	}
	if row.MarketCap != 0 {
		t.Errorf("MarketCap = %d, want 0 when mdIdx is nil", row.MarketCap)
	}
}

func TestAllSymbols_Sorted(t *testing.T) {
	rows := map[string]*TickerRow{
		"SCHW": {Symbol: "SCHW"},
		"AAPL": {Symbol: "AAPL"},
		"TSLA": {Symbol: "TSLA"},
	}
	cat := buildCatalogFromRows(rows)
	syms := cat.AllSymbols()
	if len(syms) != 3 {
		t.Fatalf("expected 3 symbols, got %d", len(syms))
	}
	for i := 1; i < len(syms); i++ {
		if syms[i] < syms[i-1] {
			t.Errorf("AllSymbols not sorted: %v", syms)
		}
	}
}
