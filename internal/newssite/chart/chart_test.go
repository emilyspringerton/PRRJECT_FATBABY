package chart

import (
	"strings"
	"testing"
	"time"
)

func TestParseYahooResponse(t *testing.T) {
	body := `{"chart":{"result":[{"timestamp":[1700000000,1700086400,1700172800],"indicators":{"quote":[{"close":[150.0,152.5,0]}]}}],"error":null}}`
	pts, err := parseYahooResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// close=0 is filtered out
	if len(pts) != 2 {
		t.Fatalf("want 2 price points, got %d", len(pts))
	}
	if pts[0].Close != 150.0 || pts[1].Close != 152.5 {
		t.Errorf("closes = %v,%v, want 150.0,152.5", pts[0].Close, pts[1].Close)
	}
	if pts[0].Time.IsZero() {
		t.Error("first time should not be zero")
	}
}

func TestParseYahooResponseError(t *testing.T) {
	body := `{"chart":{"result":null,"error":{"code":"Not Found","description":"No data"}}}`
	_, err := parseYahooResponse(strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error from yahoo error payload, got nil")
	}
	if !strings.Contains(err.Error(), "Not Found") {
		t.Errorf("error should mention Not Found: %v", err)
	}
}

func TestParseYahooResponseEmpty(t *testing.T) {
	body := `{"chart":{"result":[],"error":null}}`
	pts, err := parseYahooResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pts != nil {
		t.Errorf("want nil for empty result, got %v", pts)
	}
}

func TestRenderSVGUptrend(t *testing.T) {
	pts := []PricePoint{
		{Time: time.Unix(1700000000, 0), Close: 100.0},
		{Time: time.Unix(1700086400, 0), Close: 110.0},
	}
	svg := RenderSVG(pts)
	if !strings.HasPrefix(svg, "<svg") {
		t.Errorf("want <svg prefix, got: %s", svg[:20])
	}
	// Uptrend → green line
	if !strings.Contains(svg, "#16a34a") {
		t.Errorf("uptrend SVG should use green (#16a34a)")
	}
	if !strings.Contains(svg, "$100.00") {
		t.Errorf("SVG should contain open price label $100.00")
	}
	if !strings.Contains(svg, "$110.00") {
		t.Errorf("SVG should contain close price label $110.00")
	}
}

func TestRenderSVGDowntrend(t *testing.T) {
	pts := []PricePoint{
		{Time: time.Unix(1700000000, 0), Close: 110.0},
		{Time: time.Unix(1700086400, 0), Close: 100.0},
	}
	svg := RenderSVG(pts)
	// Downtrend → red line
	if !strings.Contains(svg, "#dc2626") {
		t.Errorf("downtrend SVG should use red (#dc2626)")
	}
}

func TestRenderSVGTooFewPoints(t *testing.T) {
	if got := RenderSVG(nil); got != "" {
		t.Errorf("nil pts: want empty, got %q", got)
	}
	if got := RenderSVG([]PricePoint{{Close: 100.0}}); got != "" {
		t.Errorf("single point: want empty, got %q", got)
	}
}

func TestRenderSVGFlatLine(t *testing.T) {
	pts := []PricePoint{
		{Time: time.Unix(1700000000, 0), Close: 50.0},
		{Time: time.Unix(1700086400, 0), Close: 50.0},
	}
	svg := RenderSVG(pts)
	// Flat → treated as up (close >= open) → green
	if !strings.Contains(svg, "#16a34a") {
		t.Errorf("flat line should use green: %s", svg)
	}
}
