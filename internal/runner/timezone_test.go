package runner

import (
	"time"
	"testing"

	"github.com/amterp/beagle/internal/launchd"
)

func intPtr(v int) *int { return &v }

func TestCalendarMatchesAllWildcard(t *testing.T) {
	cal := launchd.Calendar{} // all nil = match everything
	now := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	if !calendarMatches(cal, now) {
		t.Fatal("all-wildcard calendar should match any time")
	}
}

func TestCalendarMatchesSpecificTime(t *testing.T) {
	cal := launchd.Calendar{
		Minute: intPtr(30),
		Hour:   intPtr(14),
	}
	matching := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	if !calendarMatches(cal, matching) {
		t.Fatal("should match at 14:30")
	}

	nonMatching := time.Date(2025, 6, 15, 15, 30, 0, 0, time.UTC)
	if calendarMatches(cal, nonMatching) {
		t.Fatal("should not match at 15:30")
	}
}

func TestCalendarMatchesWeekday(t *testing.T) {
	// Sunday = 0
	cal := launchd.Calendar{
		Weekday: intPtr(0),
	}
	sunday := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC) // June 15 2025 is a Sunday
	if !calendarMatches(cal, sunday) {
		t.Fatalf("expected Sunday match, day is %s", sunday.Weekday())
	}

	monday := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
	if calendarMatches(cal, monday) {
		t.Fatal("should not match Monday")
	}
}
