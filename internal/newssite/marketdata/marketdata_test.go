package marketdata

import (
	"context"
	"encoding/json"
	"log"
	"testing"
	"time"

	"github.com/example/prrject-fatbaby/eventstore"
)

func tickEvent(t *testing.T, ticker, date string, timestamp time.Time, closeVal float64) eventstore.Event {
	t.Helper()
	bar := Bar{
		Ticker: ticker, Date: date, Timestamp: timestamp,
		Open: closeVal, High: closeVal, Low: closeVal, Close: closeVal, AdjClose: closeVal,
		Volume: 1000,
	}
	b, err := json.Marshal(bar)
	if err != nil {
		t.Fatalf("marshal bar: %v", err)
	}
	return eventstore.Event{
		ID:   "market_data_tick:" + ticker + ":" + date,
		Type: "market_data_tick",
		Data: b,
	}
}

func TestBuild_LoadsAndOrdersByDate(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.Append(ctx,
		tickEvent(t, "AAPL", "2026-01-03", base.AddDate(0, 0, 2), 103),
		tickEvent(t, "AAPL", "2026-01-01", base, 100),
		tickEvent(t, "AAPL", "2026-01-02", base.AddDate(0, 0, 1), 101),
	); err != nil {
		t.Fatalf("append: %v", err)
	}

	idx := NewStore()
	if err := Build(ctx, store, idx, log.Default()); err != nil {
		t.Fatalf("build: %v", err)
	}

	series := idx.SeriesFor("AAPL", 0)
	if len(series) != 3 {
		t.Fatalf("want 3 bars, got %d", len(series))
	}
	wantDates := []string{"2026-01-01", "2026-01-02", "2026-01-03"}
	for i, want := range wantDates {
		if series[i].Date != want {
			t.Errorf("index %d: date = %q, want %q", i, series[i].Date, want)
		}
	}
}

func TestSeriesFor_LimitsToMostRecentN(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var events []eventstore.Event
	for i := 0; i < 5; i++ {
		d := base.AddDate(0, 0, i)
		events = append(events, tickEvent(t, "MSFT", d.Format("2006-01-02"), d, float64(100+i)))
	}
	if _, err := store.Append(ctx, events...); err != nil {
		t.Fatalf("append: %v", err)
	}

	idx := NewStore()
	if err := Build(ctx, store, idx, log.Default()); err != nil {
		t.Fatalf("build: %v", err)
	}

	series := idx.SeriesFor("MSFT", 2)
	if len(series) != 2 {
		t.Fatalf("want 2 bars, got %d", len(series))
	}
	if series[0].Date != "2026-01-04" || series[1].Date != "2026-01-05" {
		t.Errorf("want the 2 most recent dates, got %s, %s", series[0].Date, series[1].Date)
	}
}

func TestIngest_LastWriteWinsPerTickerDate(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := store.Append(ctx,
		tickEvent(t, "AAPL", "2026-01-01", d, 100),
		tickEvent(t, "AAPL", "2026-01-01", d, 999), // corrected re-ingest, same day
	); err != nil {
		t.Fatalf("append: %v", err)
	}

	idx := NewStore()
	if err := Build(ctx, store, idx, log.Default()); err != nil {
		t.Fatalf("build: %v", err)
	}

	series := idx.SeriesFor("AAPL", 0)
	if len(series) != 1 {
		t.Fatalf("want 1 bar (deduped), got %d", len(series))
	}
	if series[0].Close != 999 {
		t.Errorf("want last-write-wins close=999, got %v", series[0].Close)
	}
}

func TestHasData(t *testing.T) {
	idx := NewStore()
	if idx.HasData("AAPL") {
		t.Error("empty store should report no data")
	}
}

func TestLatestMarketCap(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	mkEvent := func(date string, ts time.Time, mc int64) eventstore.Event {
		bar := Bar{Ticker: "AAPL", Date: date, Timestamp: ts, Close: 100, MarketCap: mc}
		b, err := json.Marshal(bar)
		if err != nil {
			t.Fatalf("marshal bar: %v", err)
		}
		return eventstore.Event{ID: "market_data_tick:AAPL:" + date, Type: "market_data_tick", Data: b}
	}

	if _, err := store.Append(ctx,
		mkEvent("2026-01-01", base, 1_000_000_000),
		mkEvent("2026-01-02", base.AddDate(0, 0, 1), 1_050_000_000), // latest bar, latest market cap
	); err != nil {
		t.Fatalf("append: %v", err)
	}

	idx := NewStore()
	if err := Build(ctx, store, idx, log.Default()); err != nil {
		t.Fatalf("build: %v", err)
	}

	mc, ok := idx.LatestMarketCap("AAPL")
	if !ok {
		t.Fatal("expected LatestMarketCap ok=true for AAPL")
	}
	if mc != 1_050_000_000 {
		t.Errorf("LatestMarketCap = %d, want 1050000000 (the latest bar's, not the first)", mc)
	}

	if _, ok := idx.LatestMarketCap("UNKNOWN"); ok {
		t.Error("LatestMarketCap for unknown ticker should be ok=false")
	}
}

func TestLatestMarketCap_ZeroWhenBarPredatesEnrichment(t *testing.T) {
	dir := t.TempDir()
	store, err := eventstore.NewFileStore(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// A bar with no market_cap field at all (as emitted before this field
	// existed) unmarshals to MarketCap == 0, which LatestMarketCap must
	// treat as "unknown", not "zero market cap".
	if _, err := store.Append(context.Background(), tickEvent(t, "OLD", "2026-01-01", time.Now(), 50)); err != nil {
		t.Fatalf("append: %v", err)
	}

	idx := NewStore()
	if err := Build(context.Background(), store, idx, log.Default()); err != nil {
		t.Fatalf("build: %v", err)
	}

	if _, ok := idx.LatestMarketCap("OLD"); ok {
		t.Error("expected ok=false for a bar with no market cap data")
	}
}
