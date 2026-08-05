// Package prreaction tracks how a ticker's real stock price moves after a
// press release or SEC filing is published -- founder, live: "we need to
// start tracking price action/reaction when a pr is released and then at
// certain time intervals after the release so we can start tracking how
// certain companies respond to news."
//
// Pure scheduling/selection logic lives here (no network, no event store) so
// it's directly testable; cmd/pr-reaction-watcher wires this to the real
// event stores and Yahoo Finance quotes.
package prreaction

import (
	"time"

	"github.com/example/prrject-fatbaby/internal/marketcal"
)

// Offset names one of the six real sample points tracked after a release.
type Offset string

const (
	OffsetT0   Offset = "t0"   // price at release (baseline)
	OffsetT15m Offset = "t15m" // +15 minutes, wall clock
	OffsetT1h  Offset = "t1h"  // +1 hour, wall clock
	OffsetEOD  Offset = "eod"  // close of the release's own trading day (or the next one, if released after-hours/on a non-trading day)
	OffsetT1d  Offset = "t1d"  // close of the trading day after EOD
	OffsetT3d  Offset = "t3d"  // close of the 3rd trading day after EOD
)

// AllOffsets is every sample point tracked per release, in the order they
// become due.
var AllOffsets = []Offset{OffsetT0, OffsetT15m, OffsetT1h, OffsetEOD, OffsetT1d, OffsetT3d}

// IsIntraday reports whether this offset needs 1-minute-resolution quote
// data (t0/t15m/t1h) as opposed to a daily close (eod/t1d/t3d).
func (o Offset) IsIntraday() bool {
	return o == OffsetT0 || o == OffsetT15m || o == OffsetT1h
}

var easternLocation = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// Real, if unlikely, environment gap (no tzdata) -- UTC is wrong for
		// market-close math but keeps the service running rather than
		// panicking on every single target-time computation.
		return time.UTC
	}
	return loc
}()

// tradingDayClose returns the real market-close instant on a given calendar
// day (4:00pm ET, or 1:00pm ET on a real NYSE early-close day). day's
// Y-M-D is taken as-is; time-of-day/location on the input is ignored, same
// contract as marketcal.IsMarketDay.
func tradingDayClose(day time.Time) time.Time {
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, easternLocation)
	hour, min := 16, 0
	if marketcal.IsEarlyClose(d) {
		hour, min = 13, 0
	}
	return time.Date(d.Year(), d.Month(), d.Day(), hour, min, 0, 0, easternLocation)
}

// nextTradingDay returns the next real NYSE trading day strictly after day.
func nextTradingDay(day time.Time) time.Time {
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, easternLocation).AddDate(0, 0, 1)
	for !marketcal.IsMarketDay(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// eodTradingDay returns the calendar day whose close counts as "EOD" for a
// release at releaseTime: that same day, if the release happened during a
// real trading day before its close; otherwise the next real trading day
// (covers after-hours releases and releases on a weekend/holiday).
func eodTradingDay(releaseTime time.Time) time.Time {
	rel := releaseTime.In(easternLocation)
	day := time.Date(rel.Year(), rel.Month(), rel.Day(), 0, 0, 0, 0, easternLocation)
	if marketcal.IsMarketDay(day) && rel.Before(tradingDayClose(day)) {
		return day
	}
	return nextTradingDay(day)
}

// TargetTime returns the real wall-clock instant a given offset should be
// sampled at, for a release at releaseTime.
func TargetTime(releaseTime time.Time, offset Offset) time.Time {
	switch offset {
	case OffsetT0:
		return releaseTime
	case OffsetT15m:
		return releaseTime.Add(15 * time.Minute)
	case OffsetT1h:
		return releaseTime.Add(1 * time.Hour)
	case OffsetEOD:
		return tradingDayClose(eodTradingDay(releaseTime))
	case OffsetT1d:
		d1 := nextTradingDay(eodTradingDay(releaseTime))
		return tradingDayClose(d1)
	case OffsetT3d:
		d := eodTradingDay(releaseTime)
		d = nextTradingDay(d)
		d = nextTradingDay(d)
		d = nextTradingDay(d)
		return tradingDayClose(d)
	default:
		return releaseTime
	}
}

// Bar is one price sample (a 1-minute or daily close) from Yahoo's chart API.
type Bar struct {
	Time  time.Time
	Close float64
}

// NearestBarAtOrBefore returns the most recent bar whose time is at or
// before target -- "price as of this moment," which can only ever look at
// the past, never assume a future bar. Returns ok=false if every bar in
// the slice is after target (nothing usable yet).
func NearestBarAtOrBefore(bars []Bar, target time.Time) (bar Bar, ok bool) {
	for _, b := range bars {
		if b.Time.After(target) {
			continue
		}
		if !ok || b.Time.After(bar.Time) {
			bar = b
			ok = true
		}
	}
	return bar, ok
}
