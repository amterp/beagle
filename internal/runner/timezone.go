package runner

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/amterp/beagle/internal/launchd"
)

// ShouldSkipForTimezone checks whether the current time in the configured
// timezone matches any of the cron calendar entries. Returns true if the job
// should be skipped (i.e., no calendar entry matches the current time).
//
// This is necessary because launchd fires based on the system timezone, but
// the user may want the schedule interpreted in a different timezone.
//
// NOTE: This does not account for DST transitions. During the ~1 hour
// window around a DST change, a job might fire or be skipped unexpectedly.
// This is a known limitation - implementing DST tolerance would add
// significant complexity for a rare edge case.
func ShouldSkipForTimezone(stderr io.Writer) bool {
	if os.Getenv("BEAGLE_JOB_TYPE") != "schedule" {
		return false
	}
	if os.Getenv("BEAGLE_SCHEDULE_STRICT_TZ") != "1" {
		return false
	}
	cron := strings.TrimSpace(os.Getenv("BEAGLE_SCHEDULE_CRON"))
	tz := strings.TrimSpace(os.Getenv("BEAGLE_SCHEDULE_TIMEZONE"))
	if cron == "" || tz == "" {
		return false
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false
	}
	cals, err := launchd.ParseCron(cron)
	if err != nil {
		return false
	}
	now := time.Now().In(loc)
	if !anyCalendarMatches(cals, now) {
		fmt.Fprintf(stderr, "beagle-run: skipping - cron does not match %s in %s\n",
			now.Format("15:04 Mon"), tz)
		return true
	}
	return false
}

// anyCalendarMatches returns true if any calendar entry matches the given time.
func anyCalendarMatches(cals []launchd.Calendar, now time.Time) bool {
	for _, cal := range cals {
		if calendarMatches(cal, now) {
			return true
		}
	}
	return false
}

// calendarMatches checks if the given time matches the calendar entry.
// A nil field means "any value" (wildcard).
func calendarMatches(cal launchd.Calendar, now time.Time) bool {
	if cal.Minute != nil && *cal.Minute != now.Minute() {
		return false
	}
	if cal.Hour != nil && *cal.Hour != now.Hour() {
		return false
	}
	if cal.Day != nil && *cal.Day != now.Day() {
		return false
	}
	if cal.Month != nil && *cal.Month != int(now.Month()) {
		return false
	}
	if cal.Weekday != nil && *cal.Weekday != int(now.Weekday()) {
		return false
	}
	return true
}
