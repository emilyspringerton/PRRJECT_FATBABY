package prreaction

import (
	"testing"
	"time"
)

func mustLoadNY(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	return loc
}

func TestTargetTime_ShortOffsetsAreWallClock(t *testing.T) {
	ny := mustLoadNY(t)
	release := time.Date(2026, 8, 5, 10, 0, 0, 0, ny) // Wednesday, during market hours

	if got := TargetTime(release, OffsetT0); !got.Equal(release) {
		t.Errorf("t0 = %v, want %v (release itself)", got, release)
	}
	if got, want := TargetTime(release, OffsetT15m), release.Add(15*time.Minute); !got.Equal(want) {
		t.Errorf("t15m = %v, want %v", got, want)
	}
	if got, want := TargetTime(release, OffsetT1h), release.Add(time.Hour); !got.Equal(want) {
		t.Errorf("t1h = %v, want %v", got, want)
	}
}

func TestTargetTime_EOD_DuringMarketHours_SameDay(t *testing.T) {
	ny := mustLoadNY(t)
	release := time.Date(2026, 8, 5, 10, 0, 0, 0, ny) // Wednesday 10am ET
	got := TargetTime(release, OffsetEOD)
	want := time.Date(2026, 8, 5, 16, 0, 0, 0, ny)
	if !got.Equal(want) {
		t.Errorf("eod = %v, want %v (same trading day's 4pm close)", got, want)
	}
}

func TestTargetTime_EOD_AfterHours_RollsToNextTradingDay(t *testing.T) {
	ny := mustLoadNY(t)
	release := time.Date(2026, 8, 5, 18, 0, 0, 0, ny) // Wednesday 6pm ET, after close
	got := TargetTime(release, OffsetEOD)
	want := time.Date(2026, 8, 6, 16, 0, 0, 0, ny) // Thursday close
	if !got.Equal(want) {
		t.Errorf("eod (after-hours release) = %v, want %v (next trading day's close)", got, want)
	}
}

func TestTargetTime_EOD_WeekendRelease_RollsToMonday(t *testing.T) {
	ny := mustLoadNY(t)
	release := time.Date(2026, 8, 8, 12, 0, 0, 0, ny) // Saturday
	got := TargetTime(release, OffsetEOD)
	want := time.Date(2026, 8, 10, 16, 0, 0, 0, ny) // Monday close
	if !got.Equal(want) {
		t.Errorf("eod (weekend release) = %v, want %v (following Monday's close)", got, want)
	}
}

func TestTargetTime_T1d_T3d_AreTradingDaysAfterEOD(t *testing.T) {
	ny := mustLoadNY(t)
	release := time.Date(2026, 8, 5, 10, 0, 0, 0, ny) // Wednesday -> EOD Wed
	t1d := TargetTime(release, OffsetT1d)
	t3d := TargetTime(release, OffsetT3d)
	wantT1d := time.Date(2026, 8, 6, 16, 0, 0, 0, ny)  // Thursday close
	wantT3d := time.Date(2026, 8, 10, 16, 0, 0, 0, ny) // Monday close (Thu->Fri->Mon = 3 trading days after Wed)
	if !t1d.Equal(wantT1d) {
		t.Errorf("t1d = %v, want %v", t1d, wantT1d)
	}
	if !t3d.Equal(wantT3d) {
		t.Errorf("t3d = %v, want %v", t3d, wantT3d)
	}
}

func TestOffset_IsIntraday(t *testing.T) {
	intraday := map[Offset]bool{
		OffsetT0: true, OffsetT15m: true, OffsetT1h: true,
		OffsetEOD: false, OffsetT1d: false, OffsetT3d: false,
	}
	for o, want := range intraday {
		if got := o.IsIntraday(); got != want {
			t.Errorf("%s.IsIntraday() = %v, want %v", o, got, want)
		}
	}
}

func TestNearestBarAtOrBefore(t *testing.T) {
	base := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	bars := []Bar{
		{Time: base, Close: 100},
		{Time: base.Add(1 * time.Minute), Close: 101},
		{Time: base.Add(5 * time.Minute), Close: 105},
		{Time: base.Add(10 * time.Minute), Close: 110}, // after target below, must be excluded
	}

	got, ok := NearestBarAtOrBefore(bars, base.Add(7*time.Minute))
	if !ok {
		t.Fatal("expected a bar, got none")
	}
	if got.Close != 105 {
		t.Errorf("nearest bar close = %v, want 105 (the 5-min bar, latest one <= target)", got.Close)
	}

	if _, ok := NearestBarAtOrBefore(bars, base.Add(-time.Minute)); ok {
		t.Error("expected no bar before every bar's own timestamp, got one anyway")
	}
}
