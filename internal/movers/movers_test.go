package movers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// screenerBaseURLForTest points screenerBaseURL at a test server for the
// duration of one test, returning a restore func.
func screenerBaseURLForTest(url string) func() {
	orig := screenerBaseURL
	screenerBaseURL = url
	return func() { screenerBaseURL = orig }
}

func fakeScreenerServer(t *testing.T, quotes []screenerQuote) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("request missing User-Agent header")
		}
		resp := screenerResponse{}
		resp.Finance.Result = []struct {
			Quotes []screenerQuote `json:"quotes"`
		}{{Quotes: quotes}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestFetchScreener_ParsesQuotes(t *testing.T) {
	ts := fakeScreenerServer(t, []screenerQuote{
		{
			Symbol: "lcid", ShortName: "Lucid Group", LongName: "Lucid Group, Inc.",
			FullExchangeName: "NasdaqGS", RegularMarketPrice: 7.36, RegularMarketChange: 0.9,
			RegularMarketChangePct: 13.93, RegularMarketVolume: 47786233,
			RegularMarketPrevClose: 6.46, MarketCap: 2872290048,
		},
	})
	defer ts.Close()

	orig := screenerBaseURLForTest(ts.URL)
	defer orig()

	quotes, err := FetchScreener(context.Background(), ts.Client(), "day_gainers", 10)
	if err != nil {
		t.Fatalf("FetchScreener: %v", err)
	}
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(quotes))
	}
	q := quotes[0]
	if q.Symbol != "LCID" {
		t.Errorf("Symbol = %q, want LCID (uppercased)", q.Symbol)
	}
	if q.Name != "Lucid Group, Inc." {
		t.Errorf("Name = %q, want longName preferred over shortName", q.Name)
	}
	if q.ChangePercent != 13.93 {
		t.Errorf("ChangePercent = %v, want 13.93", q.ChangePercent)
	}
	if q.Volume != 47786233 {
		t.Errorf("Volume = %v, want 47786233", q.Volume)
	}
}

func TestFetchScreener_NameFallsBackToShortNameThenSymbol(t *testing.T) {
	ts := fakeScreenerServer(t, []screenerQuote{
		{Symbol: "AAA", ShortName: "AAA Corp"},
		{Symbol: "BBB"},
	})
	defer ts.Close()
	defer screenerBaseURLForTest(ts.URL)()

	quotes, err := FetchScreener(context.Background(), ts.Client(), "day_losers", 10)
	if err != nil {
		t.Fatalf("FetchScreener: %v", err)
	}
	if quotes[0].Name != "AAA Corp" {
		t.Errorf("Name = %q, want fallback to shortName", quotes[0].Name)
	}
	if quotes[1].Name != "BBB" {
		t.Errorf("Name = %q, want fallback to symbol", quotes[1].Name)
	}
}

func TestFetchScreener_RetriesOn429(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		resp := screenerResponse{}
		resp.Finance.Result = []struct {
			Quotes []screenerQuote `json:"quotes"`
		}{{Quotes: []screenerQuote{{Symbol: "OK"}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	defer screenerBaseURLForTest(ts.URL)()

	quotes, err := FetchScreener(context.Background(), ts.Client(), "day_gainers", 5)
	if err != nil {
		t.Fatalf("FetchScreener: %v", err)
	}
	if attempts < 2 {
		t.Errorf("attempts = %d, want at least 2 (should have retried the 429)", attempts)
	}
	if len(quotes) != 1 || quotes[0].Symbol != "OK" {
		t.Errorf("unexpected quotes after retry: %+v", quotes)
	}
}

func TestFetchScreener_YahooErrorField(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io := `{"finance":{"result":[],"error":{"code":"bad","description":"nope"}}}`
		w.Write([]byte(io))
	}))
	defer ts.Close()
	defer screenerBaseURLForTest(ts.URL)()

	_, err := FetchScreener(context.Background(), ts.Client(), "day_gainers", 5)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected error mentioning yahoo error description, got %v", err)
	}
}

func TestFetchSnapshot_FetchesBothGainersAndLosers(t *testing.T) {
	var gotIDs []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIDs = append(gotIDs, r.URL.Query().Get("scrIds"))
		resp := screenerResponse{}
		resp.Finance.Result = []struct {
			Quotes []screenerQuote `json:"quotes"`
		}{{Quotes: []screenerQuote{{Symbol: r.URL.Query().Get("scrIds")}}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()
	defer screenerBaseURLForTest(ts.URL)()

	snap, err := FetchSnapshot(context.Background(), ts.Client(), 10)
	if err != nil {
		t.Fatalf("FetchSnapshot: %v", err)
	}
	if len(snap.Gainers) != 1 || len(snap.Losers) != 1 {
		t.Fatalf("expected 1 gainer + 1 loser, got %d/%d", len(snap.Gainers), len(snap.Losers))
	}
	if gotIDs[0] != "day_gainers" || gotIDs[1] != "day_losers" {
		t.Errorf("scrIds fetched in unexpected order: %v", gotIDs)
	}
	if snap.FetchedAt.IsZero() {
		t.Error("FetchedAt should be set")
	}
}
