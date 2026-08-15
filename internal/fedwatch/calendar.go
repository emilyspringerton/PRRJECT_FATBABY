package fedwatch

import "time"

// FOMCMeeting is one scheduled Federal Open Market Committee meeting.
// Two-day meetings (the normal case) have Start != End; single-day
// meetings would have Start == End, though none exist in the tracked
// range below.
type FOMCMeeting struct {
	Start time.Time
	End   time.Time
}

// fomcDate builds a UTC midnight date -- meetings are tracked by calendar
// date, not a specific decision time (the decision/statement release time
// itself comes from the RSS feed in client.go, not this calendar).
func fomcDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// TrackedMeetings is the Fed's own published FOMC meeting schedule,
// 2021-2027 -- sourced directly from
// https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm
// (fetched and cross-checked against the live press feed 2026-08-15: the
// feed's own "Federal Reserve issues FOMC statement" items for
// 2026-06-17 and 2026-07-29 match this calendar's June 16-17 and July
// 28-29 meetings exactly). Not rule-computable like marketcal's NYSE
// holidays -- the Fed sets these by its own annual decision, so this is
// data to be refreshed from the published calendar as new years are
// announced, not a formula. 2021-2025 kept for historical backtesting
// value (same reasoning marketcal keeps past years); 2027 included since
// it was already published at the time this was written.
var TrackedMeetings = []FOMCMeeting{
	// 2021
	{fomcDate(2021, time.January, 26), fomcDate(2021, time.January, 27)},
	{fomcDate(2021, time.March, 16), fomcDate(2021, time.March, 17)},
	{fomcDate(2021, time.April, 27), fomcDate(2021, time.April, 28)},
	{fomcDate(2021, time.June, 15), fomcDate(2021, time.June, 16)},
	{fomcDate(2021, time.July, 27), fomcDate(2021, time.July, 28)},
	{fomcDate(2021, time.September, 21), fomcDate(2021, time.September, 22)},
	{fomcDate(2021, time.November, 2), fomcDate(2021, time.November, 3)},
	{fomcDate(2021, time.December, 14), fomcDate(2021, time.December, 15)},
	// 2022
	{fomcDate(2022, time.January, 25), fomcDate(2022, time.January, 26)},
	{fomcDate(2022, time.March, 15), fomcDate(2022, time.March, 16)},
	{fomcDate(2022, time.May, 3), fomcDate(2022, time.May, 4)},
	{fomcDate(2022, time.June, 14), fomcDate(2022, time.June, 15)},
	{fomcDate(2022, time.July, 26), fomcDate(2022, time.July, 27)},
	{fomcDate(2022, time.September, 20), fomcDate(2022, time.September, 21)},
	{fomcDate(2022, time.November, 1), fomcDate(2022, time.November, 2)},
	{fomcDate(2022, time.December, 13), fomcDate(2022, time.December, 14)},
	// 2023
	{fomcDate(2023, time.January, 31), fomcDate(2023, time.February, 1)},
	{fomcDate(2023, time.March, 21), fomcDate(2023, time.March, 22)},
	{fomcDate(2023, time.May, 2), fomcDate(2023, time.May, 3)},
	{fomcDate(2023, time.June, 13), fomcDate(2023, time.June, 14)},
	{fomcDate(2023, time.July, 25), fomcDate(2023, time.July, 26)},
	{fomcDate(2023, time.September, 19), fomcDate(2023, time.September, 20)},
	{fomcDate(2023, time.October, 31), fomcDate(2023, time.November, 1)},
	{fomcDate(2023, time.December, 12), fomcDate(2023, time.December, 13)},
	// 2024
	{fomcDate(2024, time.January, 30), fomcDate(2024, time.January, 31)},
	{fomcDate(2024, time.March, 19), fomcDate(2024, time.March, 20)},
	{fomcDate(2024, time.April, 30), fomcDate(2024, time.May, 1)},
	{fomcDate(2024, time.June, 11), fomcDate(2024, time.June, 12)},
	{fomcDate(2024, time.July, 30), fomcDate(2024, time.July, 31)},
	{fomcDate(2024, time.September, 17), fomcDate(2024, time.September, 18)},
	{fomcDate(2024, time.November, 6), fomcDate(2024, time.November, 7)},
	{fomcDate(2024, time.December, 17), fomcDate(2024, time.December, 18)},
	// 2025
	{fomcDate(2025, time.January, 28), fomcDate(2025, time.January, 29)},
	{fomcDate(2025, time.March, 18), fomcDate(2025, time.March, 19)},
	{fomcDate(2025, time.May, 6), fomcDate(2025, time.May, 7)},
	{fomcDate(2025, time.June, 17), fomcDate(2025, time.June, 18)},
	{fomcDate(2025, time.July, 29), fomcDate(2025, time.July, 30)},
	{fomcDate(2025, time.September, 16), fomcDate(2025, time.September, 17)},
	{fomcDate(2025, time.October, 28), fomcDate(2025, time.October, 29)},
	{fomcDate(2025, time.December, 9), fomcDate(2025, time.December, 10)},
	// 2026
	{fomcDate(2026, time.January, 27), fomcDate(2026, time.January, 28)},
	{fomcDate(2026, time.March, 17), fomcDate(2026, time.March, 18)},
	{fomcDate(2026, time.April, 28), fomcDate(2026, time.April, 29)},
	{fomcDate(2026, time.June, 16), fomcDate(2026, time.June, 17)},
	{fomcDate(2026, time.July, 28), fomcDate(2026, time.July, 29)},
	{fomcDate(2026, time.September, 15), fomcDate(2026, time.September, 16)},
	{fomcDate(2026, time.October, 27), fomcDate(2026, time.October, 28)},
	{fomcDate(2026, time.December, 8), fomcDate(2026, time.December, 9)},
	// 2027
	{fomcDate(2027, time.January, 26), fomcDate(2027, time.January, 27)},
	{fomcDate(2027, time.March, 16), fomcDate(2027, time.March, 17)},
	{fomcDate(2027, time.April, 27), fomcDate(2027, time.April, 28)},
	{fomcDate(2027, time.June, 8), fomcDate(2027, time.June, 9)},
	{fomcDate(2027, time.July, 27), fomcDate(2027, time.July, 28)},
	{fomcDate(2027, time.September, 14), fomcDate(2027, time.September, 15)},
	{fomcDate(2027, time.October, 26), fomcDate(2027, time.October, 27)},
	{fomcDate(2027, time.December, 7), fomcDate(2027, time.December, 8)},
}

// IsFOMCMeetingDay reports whether t's calendar date (UTC) falls within
// any tracked meeting's [Start, End] range, inclusive.
func IsFOMCMeetingDay(t time.Time) bool {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	for _, m := range TrackedMeetings {
		if !d.Before(m.Start) && !d.After(m.End) {
			return true
		}
	}
	return false
}

// NextMeeting returns the earliest tracked meeting whose Start is on or
// after t's calendar date, and true. Returns (FOMCMeeting{}, false) when
// t is past every tracked meeting (i.e. the calendar needs refreshing
// against a newly published year) -- callers must check the bool, not
// assume a zero-value Start is meaningful.
func NextMeeting(t time.Time) (FOMCMeeting, bool) {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	for _, m := range TrackedMeetings {
		if !m.Start.Before(d) {
			return m, true
		}
	}
	return FOMCMeeting{}, false
}
