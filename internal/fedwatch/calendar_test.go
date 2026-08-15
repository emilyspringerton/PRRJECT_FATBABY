package fedwatch

import (
	"testing"
	"time"
)

func TestIsFOMCMeetingDay_KnownMeetingDate(t *testing.T) {
	// 2026-07-29 is the second day of the Jul 28-29 2026 meeting, and is
	// also the date the sampleFeed fixture's real "Federal Reserve issues
	// FOMC statement" item was published on -- cross-checked, not
	// independently guessed.
	d := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	if !IsFOMCMeetingDay(d) {
		t.Error("expected 2026-07-29 to be a tracked FOMC meeting day")
	}
}

func TestIsFOMCMeetingDay_FirstDayOfTwoDayMeeting(t *testing.T) {
	d := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	if !IsFOMCMeetingDay(d) {
		t.Error("expected the first day of a two-day meeting to count")
	}
}

func TestIsFOMCMeetingDay_NonMeetingDate(t *testing.T) {
	d := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	if IsFOMCMeetingDay(d) {
		t.Error("expected an ordinary date to not be a meeting day")
	}
}

func TestIsFOMCMeetingDay_TimeOfDayIgnored(t *testing.T) {
	// A late-night timestamp on a meeting date should still count -- the
	// function should compare calendar dates, not exact instants.
	d := time.Date(2026, time.July, 29, 23, 59, 0, 0, time.UTC)
	if !IsFOMCMeetingDay(d) {
		t.Error("expected time-of-day to be ignored when checking the date")
	}
}

func TestNextMeeting_ReturnsEarliestUpcoming(t *testing.T) {
	d := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	m, ok := NextMeeting(d)
	if !ok {
		t.Fatal("expected a next meeting to be found")
	}
	want := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC)
	if !m.Start.Equal(want) {
		t.Errorf("NextMeeting Start = %v, want %v (Sep 15-16 2026)", m.Start, want)
	}
}

func TestNextMeeting_ReturnsCurrentMeetingIfInProgress(t *testing.T) {
	// Asking on the first day of a meeting should return that same
	// meeting, not skip ahead to the next one.
	d := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	m, ok := NextMeeting(d)
	if !ok {
		t.Fatal("expected a meeting to be found")
	}
	want := time.Date(2026, time.July, 28, 0, 0, 0, 0, time.UTC)
	if !m.Start.Equal(want) {
		t.Errorf("NextMeeting Start = %v, want %v", m.Start, want)
	}
}

func TestNextMeeting_PastAllTrackedMeetingsReturnsFalse(t *testing.T) {
	d := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	_, ok := NextMeeting(d)
	if ok {
		t.Error("expected ok=false when past every tracked meeting (calendar needs refreshing)")
	}
}

func TestTrackedMeetings_AllStartBeforeOrEqualEnd(t *testing.T) {
	for i, m := range TrackedMeetings {
		if m.End.Before(m.Start) {
			t.Errorf("meeting %d: End %v is before Start %v", i, m.End, m.Start)
		}
	}
}

func TestTrackedMeetings_ChronologicallySorted(t *testing.T) {
	for i := 1; i < len(TrackedMeetings); i++ {
		if TrackedMeetings[i].Start.Before(TrackedMeetings[i-1].Start) {
			t.Errorf("meeting %d (%v) is out of order relative to meeting %d (%v) -- "+
				"NextMeeting's linear scan assumes ascending order",
				i, TrackedMeetings[i].Start, i-1, TrackedMeetings[i-1].Start)
		}
	}
}

func TestTrackedMeetings_2026Count(t *testing.T) {
	count := 0
	for _, m := range TrackedMeetings {
		if m.Start.Year() == 2026 {
			count++
		}
	}
	// The Fed holds 8 scheduled FOMC meetings per year -- verified against
	// the live published calendar 2026-08-15.
	if count != 8 {
		t.Errorf("expected 8 tracked 2026 meetings, got %d", count)
	}
}
