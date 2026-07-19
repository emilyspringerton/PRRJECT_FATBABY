package marketcal

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestIsMarketDay_Weekends(t *testing.T) {
	if IsMarketDay(date(2026, time.July, 18)) { // Saturday
		t.Error("Saturday should not be a market day")
	}
	if IsMarketDay(date(2026, time.July, 19)) { // Sunday
		t.Error("Sunday should not be a market day")
	}
	if !IsMarketDay(date(2026, time.July, 20)) { // Monday
		t.Error("Monday should be a market day")
	}
}

func TestIsMarketDay_2026Holidays(t *testing.T) {
	// 2026 NYSE holiday dates, cross-checked against the published schedule.
	holidays := []struct {
		date time.Time
		name string
	}{
		{date(2026, time.January, 1), NewYearsDay},
		{date(2026, time.January, 19), MLKDay},
		{date(2026, time.February, 16), PresidentsDay},
		{date(2026, time.April, 3), GoodFriday},
		{date(2026, time.May, 25), MemorialDay},
		{date(2026, time.June, 19), Juneteenth},
		{date(2026, time.July, 3), IndependenceDay}, // July 4 2026 is a Saturday -> observed Friday July 3
		{date(2026, time.September, 7), LaborDay},
		{date(2026, time.November, 26), Thanksgiving},
		{date(2026, time.December, 25), ChristmasDay},
	}
	for _, h := range holidays {
		if IsMarketDay(h.date) {
			t.Errorf("%s (%s) should not be a market day", h.date.Format("2006-01-02"), h.name)
		}
		if got := HolidayName(h.date); got != h.name {
			t.Errorf("HolidayName(%s) = %q, want %q", h.date.Format("2006-01-02"), got, h.name)
		}
	}
}

func TestIsMarketDay_2025SaturdayHolidayObservance(t *testing.T) {
	// Juneteenth 2025-06-19 is a Thursday -- not a weekend-adjustment case,
	// included as a sanity check that the fixed-date holiday still fires.
	if IsMarketDay(date(2025, time.June, 19)) {
		t.Error("Juneteenth 2025 should not be a market day")
	}
	// Christmas 2027-12-25 falls on a Saturday -> observed Friday Dec 24.
	if IsMarketDay(date(2027, time.December, 24)) {
		t.Error("Dec 24 2027 (observed Christmas) should not be a market day")
	}
	if !IsMarketDay(date(2027, time.December, 27)) {
		t.Error("Dec 27 2027 (Monday after observed Christmas) should be a market day")
	}
}

func TestIsMarketDay_RegularTradingDay(t *testing.T) {
	if !IsMarketDay(date(2026, time.July, 21)) {
		t.Error("an ordinary Tuesday should be a market day")
	}
	if HolidayName(date(2026, time.July, 21)) != "" {
		t.Error("an ordinary Tuesday should not report a holiday name")
	}
}

func TestIsEarlyClose(t *testing.T) {
	cases := []struct {
		date time.Time
		want bool
		name string
	}{
		{date(2026, time.November, 27), true, "day after Thanksgiving 2026"},
		{date(2026, time.December, 24), true, "Christmas Eve 2026 (Thursday)"},
		{date(2026, time.July, 21), false, "ordinary trading day"},
	}
	for _, tc := range cases {
		if got := IsEarlyClose(tc.date); got != tc.want {
			t.Errorf("IsEarlyClose(%s) [%s] = %v, want %v", tc.date.Format("2006-01-02"), tc.name, got, tc.want)
		}
	}
}

func TestIsEarlyClose_NotOnAHoliday(t *testing.T) {
	// July 3 2026 is itself the observed Independence Day holiday (market
	// closed), so it must not also report as an early-close trading day.
	if IsEarlyClose(date(2026, time.July, 3)) {
		t.Error("a full holiday closure must not also report as an early close")
	}
}

func TestEasterSunday_KnownDates(t *testing.T) {
	cases := map[int]time.Time{
		2026: date(2026, time.April, 5),
		2025: date(2025, time.April, 20),
		2024: date(2024, time.March, 31),
	}
	for y, want := range cases {
		if got := easterSunday(y); !got.Equal(want) {
			t.Errorf("easterSunday(%d) = %s, want %s", y, got.Format("2006-01-02"), want.Format("2006-01-02"))
		}
	}
}
