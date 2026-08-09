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
	return c.dateMatches(t) && c.timeMatches(t)
}

// dateMatches is the day-granular half of Matches: month plus the dom/dow rule.
// Every instant whose wall clock falls on a given local date shares these
// fields, so when this is false no instant on that date can match - which is
// what lets PrevFire skip a whole day in one step instead of 1440.
func (c CronSpec) dateMatches(t time.Time) bool {
	if !c.monthWild {
		if _, ok := c.months[int(t.Month())]; !ok {
			return false
		}
	}
	return c.dayMatches(t)
}

// timeMatches is the minute-granular half of Matches.
func (c CronSpec) timeMatches(t time.Time) bool {
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
	return true
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
//
// Local dates whose month/dom/dow cannot match are skipped whole rather than a
// minute at a time, which is what keeps a year-long lookback affordable at one
// supervisor tick per minute: a few hundred steps instead of ~525,600, measured
// at 24us against the old scan's 19.2ms. The remaining minute-by-minute work is
// bounded by one local day per date that matches at day level but yields no
// fire, which only happens when every scheduled (hour, minute) on that date
// falls in a DST gap.
func (c CronSpec) PrevFire(now time.Time, loc *time.Location, maxLookback time.Duration) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	cand := now.Truncate(time.Minute)
	limit := cand.Add(-maxLookback)
	for !cand.Before(limit) {
		wall := cand.In(loc)
		if !c.dateMatches(wall) {
			cand = prevDateEnd(cand, loc)
			continue
		}
		if c.timeMatches(wall) {
			return cand, true
		}
		cand = cand.Add(-time.Minute)
	}
	return time.Time{}, false
}

// NextFireHorizon bounds NextFire's forward search.
//
// A year is not enough, which is easy to get wrong: `0 5 29 2 *` is a legal
// expression whose occurrences are up to eight years apart, because century
// years divisible by 100 but not 400 are not leap years, so February 29th skips
// from 2096 to 2104. A one-year bound would report that job as never firing.
// Ten years clears that gap with room to spare.
//
// Nothing beyond it can fire at all, so a miss is a config mistake worth
// reporting rather than a search worth extending: `0 0 30 2 *` asks for
// February 30th. The day-skip keeps the miss cheap - a decade is a few thousand
// date steps, not five million minutes.
const NextFireHorizon = 10 * 366 * 24 * time.Hour

// NextFire returns the first minute strictly after now whose wall-clock in loc
// matches the expression, searching forward at most maxLookahead. It is the
// mirror of PrevFire and shares its reasoning: it steps over absolute instants
// and reads wall clock via In(loc), so the zone database handles DST rather
// than field arithmetic.
//
// It starts at the minute after now rather than at now, so a job whose
// occurrence is firing this very minute reports its following one. "Next" that
// could mean "now" would make a schedule column read as permanently due.
//
// Whole non-matching local dates are skipped via nextDateStart, which is what
// keeps a year-long horizon affordable on a sparse expression - the same
// bound that makes PrevFire's year-long lookback cheap.
func (c CronSpec) NextFire(now time.Time, loc *time.Location, maxLookahead time.Duration) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	base := now.Truncate(time.Minute)
	cand := base.Add(time.Minute)
	limit := base.Add(maxLookahead)
	for !cand.After(limit) {
		wall := cand.In(loc)
		if !c.dateMatches(wall) {
			cand = nextDateStart(cand, loc)
			continue
		}
		if c.timeMatches(wall) {
			return cand, true
		}
		cand = cand.Add(time.Minute)
	}
	return time.Time{}, false
}

// nextDateStart returns the first minute-aligned instant on the local date
// after cand's. Jumping there can only skip instants sharing cand's local
// date, which the caller has already ruled out.
//
// Like prevDateEnd it avoids time.Date for the boundary, for the same reason:
// local midnight does not exist in every zone, and time.Date's result for a
// nonexistent wall clock is explicitly not guaranteed.
//
// The addition is only a starting guess; the two loops make it exact for any
// transition size. The first handles a date that grew (clocks went back), where
// the guess is still inside it. The second handles a date that shrank, or was
// skipped outright as Pacific/Apia's 2011-12-30 was, where the guess overshot
// past the following date's start. The second loop cannot walk back past
// cand+1m, because cand is itself on the date, so this always advances at least
// a minute and NextFire cannot spin.
func nextDateStart(cand time.Time, loc *time.Location) time.Time {
	wall := cand.In(loc)
	y, m, d := wall.Date()

	t := cand.Add(time.Duration((23-wall.Hour())*60+(60-wall.Minute())) * time.Minute)
	for onDate(t, loc, y, m, d) {
		t = t.Add(time.Minute)
	}
	for !onDate(t.Add(-time.Minute), loc, y, m, d) {
		t = t.Add(-time.Minute)
	}
	return t
}

// prevDateEnd returns the last minute-aligned instant strictly before the start
// of cand's local date. Jumping there can only skip instants sharing cand's
// local date, which the caller has already ruled out.
//
// It deliberately does not build the boundary with time.Date. Local midnight
// does not exist in every zone - America/Havana, Asia/Beirut, Africa/Cairo and
// Antarctica/Casey among others still transition at midnight - and time.Date's
// result for a nonexistent wall clock is explicitly not guaranteed. For Havana
// on 2026-03-08 (00:00 -> 01:00) time.Date resolves to 2026-03-07 23:00, so
// subtracting a minute from it would skip 23:00-23:59 on the 7th unseen.
//
// The subtraction below is only a starting guess; correctness comes from the
// two boundary loops, which hold for any transition size - including
// Pacific/Apia's 2011 skip of an entire local day. The guess is at least one
// minute and the loops never carry the result to or past cand, so PrevFire
// always makes backward progress and cannot spin.
//
// This assumes a local date is a contiguous run of instants. That holds for
// every transition in tzdata since 2011; the counterexamples (America/Goose_Bay
// under Newfoundland's old 00:01 DST rule) are older than any catch-up window
// can reach.
func prevDateEnd(cand time.Time, loc *time.Location) time.Time {
	wall := cand.In(loc)
	y, m, d := wall.Date()

	t := cand.Add(-time.Duration(wall.Hour()*60+wall.Minute()+1) * time.Minute)
	// Clocks moved forward inside the date, so the guess overshot: walk up until
	// the next minute is back inside it. Bounded by cand, which is on the date.
	for !onDate(t.Add(time.Minute), loc, y, m, d) {
		t = t.Add(time.Minute)
	}
	// Clocks moved back inside the date, so the guess is still inside it: walk
	// out. Bounded by the length of the local date.
	for onDate(t, loc, y, m, d) {
		t = t.Add(-time.Minute)
	}
	return t
}

func onDate(t time.Time, loc *time.Location, y int, m time.Month, d int) bool {
	ty, tm, td := t.In(loc).Date()
	return ty == y && tm == m && td == d
}
