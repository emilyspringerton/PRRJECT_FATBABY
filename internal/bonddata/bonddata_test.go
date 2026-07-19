package bonddata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fredBaseURLForTest(url string) func() {
	orig := fredBaseURL
	fredBaseURL = url
	return func() { fredBaseURL = orig }
}

func TestFetchSeries_ParsesCSV(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("observation_date,DGS10\n2026-07-14,4.58\n2026-07-15,4.55\n2026-07-16,4.57\n"))
	}))
	defer ts.Close()
	defer fredBaseURLForTest(ts.URL)()

	series := Series{ID: "DGS10", Label: "10-Year Treasury"}
	obs, err := FetchSeries(context.Background(), ts.Client(), series)
	if err != nil {
		t.Fatalf("FetchSeries: %v", err)
	}
	if len(obs) != 3 {
		t.Fatalf("got %d observations, want 3", len(obs))
	}
	if obs[0].Value != 4.58 || obs[0].Date.Format("2006-01-02") != "2026-07-14" {
		t.Errorf("first observation wrong: %+v", obs[0])
	}
	if obs[2].Value != 4.57 {
		t.Errorf("last observation wrong: %+v", obs[2])
	}
	if obs[0].Label != "10-Year Treasury" || obs[0].SeriesID != "DGS10" {
		t.Errorf("label/series ID not propagated: %+v", obs[0])
	}
}

func TestFetchSeries_SkipsMissingObservations(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// FRED marks holidays/no-data as "." -- must not become a zero value.
		w.Write([]byte("observation_date,DGS10\n2026-07-03,4.60\n2026-07-04,.\n2026-07-05,.\n2026-07-06,4.59\n"))
	}))
	defer ts.Close()
	defer fredBaseURLForTest(ts.URL)()

	obs, err := FetchSeries(context.Background(), ts.Client(), Series{ID: "DGS10", Label: "10-Year Treasury"})
	if err != nil {
		t.Fatalf("FetchSeries: %v", err)
	}
	if len(obs) != 2 {
		t.Fatalf("got %d observations, want 2 (missing rows skipped)", len(obs))
	}
	if obs[0].Value != 4.60 || obs[1].Value != 4.59 {
		t.Errorf("unexpected values after skipping missing rows: %+v", obs)
	}
}

func TestFetchLatest_ReturnsLastObservation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("observation_date,DGS2\n2026-07-14,4.18\n2026-07-15,4.13\n2026-07-16,4.16\n"))
	}))
	defer ts.Close()
	defer fredBaseURLForTest(ts.URL)()

	latest, err := FetchLatest(context.Background(), ts.Client(), Series{ID: "DGS2", Label: "2-Year Treasury"})
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if latest.Value != 4.16 || latest.Date.Format("2006-01-02") != "2026-07-16" {
		t.Errorf("FetchLatest = %+v, want the last row (2026-07-16, 4.16)", latest)
	}
}

func TestFetchLatest_ErrorsOnEmptySeries(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("observation_date,DGS2\n"))
	}))
	defer ts.Close()
	defer fredBaseURLForTest(ts.URL)()

	_, err := FetchLatest(context.Background(), ts.Client(), Series{ID: "DGS2", Label: "2-Year Treasury"})
	if err == nil {
		t.Fatal("expected an error for a series with no observations")
	}
}

func TestFetchSeries_RetriesOnServerError(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte("observation_date,DGS10\n2026-07-16,4.57\n"))
	}))
	defer ts.Close()
	defer fredBaseURLForTest(ts.URL)()

	obs, err := FetchSeries(context.Background(), ts.Client(), Series{ID: "DGS10", Label: "10-Year Treasury"})
	if err != nil {
		t.Fatalf("FetchSeries: %v", err)
	}
	if attempts < 2 {
		t.Errorf("attempts = %d, want at least 2 (should have retried the 500)", attempts)
	}
	if len(obs) != 1 || obs[0].Value != 4.57 {
		t.Errorf("unexpected result after retry: %+v", obs)
	}
}

func TestTrackedSeries_HasExpectedIDs(t *testing.T) {
	want := map[string]bool{"DGS2": false, "DGS10": false, "DGS30": false, "BAMLH0A0HYM2": false}
	for _, s := range TrackedSeries {
		if _, ok := want[s.ID]; !ok {
			t.Errorf("unexpected series in TrackedSeries: %s", s.ID)
		}
		want[s.ID] = true
		if s.Label == "" {
			t.Errorf("series %s has no label", s.ID)
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("expected series %s missing from TrackedSeries", id)
		}
	}
}
