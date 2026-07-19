// Package marketcal answers "is the US stock market open" questions — NYSE
// full-closure holidays, weekends, and early closes — so scheduled work
// (the daily movers article, and anything else that shouldn't fire on a
// Saturday or Memorial Day) can gate on real market state instead of just
// "did the cron tick."
//
// This is a best-effort calendar, computed from NYSE's published holiday
// rules, not a certified trading-holiday data feed. Weekend-adjustment uses
// the standard convention (holiday on Saturday -> observed Friday, holiday
// on Sunday -> observed Monday). Good Friday is computed via the
// Anonymous Gregorian (Meeus/Jones/Butcher) Easter algorithm since it's a
// moveable feast. All dates are evaluated in US/Eastern, since that's the
// market's own clock — pass time.Time values already in that location, or
// use the ForDate helpers which take a plain calendar date and don't care
// about timezone.
package marketcal

import "time"

// Holiday names NYSE fully closes for, in calendar order.
const (
	NewYearsDay     = "New Year's Day"
	MLKDay          = "Martin Luther King, Jr. Day"
	PresidentsDay   = "Washington's Birthday"
	GoodFriday      = "Good Friday"
	MemorialDay     = "Memorial Day"
	Juneteenth      = "Juneteenth National Independence Day"
	IndependenceDay = "Independence Day"
	LaborDay        = "Labor Day"
	Thanksgiving    = "Thanksgiving Day"
	ChristmasDay    = "Christmas Day"
)

// IsMarketDay reports whether the US stock market (NYSE/Nasdaq) is open for
// regular trading on the given calendar date — not a weekend, not a full
// NYSE closure holiday. Time-of-day and timezone on t are ignored; only the
// Y-M-D calendar date is considered.
func IsMarketDay(t time.Time) bool {
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	return HolidayName(t) == ""
}

// HolidayName returns the name of the NYSE full-closure holiday observed on
// this calendar date, or "" if it's a regular trading day (or a weekend —
// this function only reports holidays, callers should check weekends
// separately or just use IsMarketDay).
func HolidayName(t time.Time) string {
	y := t.Year()
	date := dateOnly(t)
	for _, h := range holidaysForYear(y) {
		if h.observed.Equal(date) {
			return h.name
		}
	}
	return ""
}

// IsEarlyClose reports whether NYSE closes early (1:00pm ET) on this date —
// the trading day after Thanksgiving, and Christmas Eve / July 3rd when
// those fall on a regular trading weekday. The market is still open on an
// early-close day; this is informational (e.g. for an article to note "half
// day today"), not a reason to skip anything IsMarketDay already allows.
func IsEarlyClose(t time.Time) bool {
	if !IsMarketDay(t) {
		return false
	}
	y := t.Year()
	date := dateOnly(t)

	thanksgiving := nthWeekday(y, time.November, time.Thursday, 4)
	dayAfterThanksgiving := thanksgiving.AddDate(0, 0, 1)
	if date.Equal(dayAfterThanksgiving) {
		return true
	}

	christmasEve := time.Date(y, time.December, 24, 0, 0, 0, 0, time.UTC)
	if date.Equal(christmasEve) {
		return true
	}

	julyThird := time.Date(y, time.July, 3, 0, 0, 0, 0, time.UTC)
	if date.Equal(julyThird) {
		return true
	}

	return false
}

type holiday struct {
	name     string
	observed time.Time // weekend-adjusted, UTC midnight, date-only
}

func holidaysForYear(y int) []holiday {
	return []holiday{
		{NewYearsDay, observedDate(y, time.January, 1)},
		{MLKDay, nthWeekday(y, time.January, time.Monday, 3)},
		{PresidentsDay, nthWeekday(y, time.February, time.Monday, 3)},
		{GoodFriday, easterSunday(y).AddDate(0, 0, -2)},
		{MemorialDay, lastWeekday(y, time.May, time.Monday)},
		{Juneteenth, observedDate(y, time.June, 19)},
		{IndependenceDay, observedDate(y, time.July, 4)},
		{LaborDay, nthWeekday(y, time.September, time.Monday, 1)},
		{Thanksgiving, nthWeekday(y, time.November, time.Thursday, 4)},
		{ChristmasDay, observedDate(y, time.December, 25)},
	}
}

// observedDate applies the standard weekend-adjustment convention to a
// fixed calendar date: Saturday -> observed the preceding Friday, Sunday ->
// observed the following Monday.
func observedDate(y int, month time.Month, day int) time.Time {
	d := time.Date(y, month, day, 0, 0, 0, 0, time.UTC)
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDate(0, 0, -1)
	case time.Sunday:
		return d.AddDate(0, 0, 1)
	default:
		return d
	}
}

// nthWeekday returns the date of the nth occurrence of weekday in month/year
// (n=1 for first, n=4 for fourth, etc).
func nthWeekday(y int, month time.Month, weekday time.Weekday, n int) time.Time {
	d := time.Date(y, month, 1, 0, 0, 0, 0, time.UTC)
	offset := (int(weekday) - int(d.Weekday()) + 7) % 7
	d = d.AddDate(0, 0, offset+7*(n-1))
	return d
}

// lastWeekday returns the date of the last occurrence of weekday in month/year.
func lastWeekday(y int, month time.Month, weekday time.Weekday) time.Time {
	// First day of next month, minus one day, is the last day of month.
	firstOfNext := time.Date(y, month+1, 1, 0, 0, 0, 0, time.UTC)
	last := firstOfNext.AddDate(0, 0, -1)
	offset := (int(last.Weekday()) - int(weekday) + 7) % 7
	return last.AddDate(0, 0, -offset)
}

// easterSunday computes the date of Easter Sunday for the given year via the
// Anonymous Gregorian (Meeus/Jones/Butcher) algorithm.
func easterSunday(y int) time.Time {
	a := y % 19
	b := y / 100
	c := y % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
