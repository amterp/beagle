package launchd

import (
	"fmt"
	"strings"
	"time"
)

// CronSpec is a parsed 5-field cron expression that can test whether a given
// instant matches and find the most recent matching minute at or before a
// given instant. Unlike the []Calendar expansion (which feeds launchd's
// StartCalendarInterval), CronSpec is what the beagle supervisor evaluates
// directly so it owns scheduling, including catch-up.
type CronSpec struct {
	minutes  map[int]struct{}
	hours    map[int]struct{}
	days     map[int]struct{} // day-of-month
	months   map[int]struct{}
	weekdays map[int]struct{} // 0=Sunday..6=Saturday

	minWild, hourWild, domWild, monthWild, dowWild bool
}

// ParseSpec parses a 5-field cron expression into a CronSpec. It reuses the
// same field grammar as ParseCron (*, N, N-M, */N, N-M/S, comma lists).
func ParseSpec(cron string) (CronSpec, error) {
	parts := strings.Fields(strings.TrimSpace(cron))
	if len(parts) != 5 {
		return CronSpec{}, fmt.Errorf("cron must have 5 fields")
	}
	var c CronSpec
	var err error
	if c.minutes, c.minWild, err = parseSet(parts[0], 0, 59); err != nil {
		return CronSpec{}, fmt.Errorf("minute: %w", err)
	}
	if c.hours, c.hourWild, err = parseSet(parts[1], 0, 23); err != nil {
		return CronSpec{}, fmt.Errorf("hour: %w", err)
	}
	if c.days, c.domWild, err = parseSet(parts[2], 1, 31); err != nil {
		return CronSpec{}, fmt.Errorf("day: %w", err)
	}
	if c.months, c.monthWild, err = parseSet(parts[3], 1, 12); err != nil {
		return CronSpec{}, fmt.Errorf("month: %w", err)
	}
	if c.weekdays, c.dowWild, err = parseSet(parts[4], 0, 6); err != nil {
		return CronSpec{}, fmt.Errorf("weekday: %w", err)
	}
	return c, nil
}

func parseSet(field string, min, max int) (map[int]struct{}, bool, error) {
	vals, err := parseField(field, min, max)
	if err != nil {
		return nil, false, err
	}
	set := make(map[int]struct{}, len(vals))
	wild := false
	for _, v := range vals {
		if v == nil {
			wild = true
			continue
		}
		set[*v] = struct{}{}
	}
	return set, wild, nil
}

// Matches reports whether t (interpreted in its own location) satisfies the
// expression. Day-of-month and day-of-week follow the classic Vixie cron rule:
// when both are restricted, a match on either suffices; otherwise each
// restricted field must match.
func (c CronSpec) Matches(t time.Time) bool {
	if !c.minWild {
		if _, ok := c.minutes[t.Minute()]; !ok {
			return false
		}
	}
	if !c.hourWild {
		if _, ok := c.hours[t.Hour()]; !ok {
			return false
		}
	}
	if !c.monthWild {
		if _, ok := c.months[int(t.Month())]; !ok {
			return false
		}
	}
	return c.dayMatches(t)
}

func (c CronSpec) dayMatches(t time.Time) bool {
	domOK := c.domWild
	if !c.domWild {
		_, domOK = c.days[t.Day()]
	}
	dowOK := c.dowWild
	if !c.dowWild {
		_, dowOK = c.weekdays[int(t.Weekday())]
	}
	if !c.domWild && !c.dowWild {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// PrevFire returns the most recent minute at or before now whose wall-clock in
// loc matches the expression, searching back at most maxLookback. It steps over
// absolute instants (reading wall-clock via In(loc)) so DST transitions are
// handled by the clock, not by field arithmetic:
//   - spring-forward: instants in the skipped hour don't exist, so a schedule
//     landing there simply never matches (standard cron behavior);
//   - fall-back: the repeated wall-clock minute matches at two instants; this
//     returns the later one. Callers that must fire an occurrence once should
//     dedup on the wall-clock identity, not the raw instant.
func (c CronSpec) PrevFire(now time.Time, loc *time.Location, maxLookback time.Duration) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	cand := now.Truncate(time.Minute)
	limit := cand.Add(-maxLookback)
	for !cand.Before(limit) {
		if c.Matches(cand.In(loc)) {
			return cand, true
		}
		cand = cand.Add(-time.Minute)
	}
	return time.Time{}, false
}
