package launchd

import (
	"testing"
	"time"
)

func mustParseSpec(t *testing.T, cron string) CronSpec {
	t.Helper()
	c, err := ParseSpec(cron)
	if err != nil {
		t.Fatalf("ParseSpec(%q): %v", cron, err)
	}
	return c
}

func TestCronSpecMatchesBasic(t *testing.T) {
	c := mustParseSpec(t, "30 9 * * *") // 09:30 daily
	utc := time.UTC
	if !c.Matches(time.Date(2026, 6, 18, 9, 30, 0, 0, utc)) {
		t.Fatal("expected match at 09:30")
	}
	if c.Matches(time.Date(2026, 6, 18, 9, 31, 0, 0, utc)) {
		t.Fatal("did not expect match at 09:31")
	}
	if c.Matches(time.Date(2026, 6, 18, 10, 30, 0, 0, utc)) {
		t.Fatal("did not expect match at 10:30")
	}
}

// TestCronSpecDayOfMonthOrWeekday checks the Vixie rule: when both day-of-month
// and day-of-week are restricted, a match on EITHER fires.
func TestCronSpecDayOfMonthOrWeekday(t *testing.T) {
	c := mustParseSpec(t, "0 0 13 * 5") // midnight on the 13th OR any Friday
	utc := time.UTC
	// 2026-02-13 is a Friday (matches both); 2026-03-13 is a Friday too.
	if !c.Matches(time.Date(2026, 11, 13, 0, 0, 0, 0, utc)) { // Nov 13 2026 is a Friday
		t.Fatal("expected match on the 13th (also a Friday)")
	}
	// A Friday that is not the 13th should still match (weekday arm).
	fri := time.Date(2026, 6, 19, 0, 0, 0, 0, utc) // 2026-06-19 is a Friday
	if fri.Weekday() != time.Friday {
		t.Fatalf("test fixture wrong: %v is %v", fri, fri.Weekday())
	}
	if !c.Matches(fri) {
		t.Fatal("expected match on a Friday that is not the 13th")
	}
	// The 13th on a non-Friday should match (day-of-month arm).
	if !c.Matches(time.Date(2026, 5, 13, 0, 0, 0, 0, utc)) {
		t.Fatal("expected match on the 13th regardless of weekday")
	}
	// A non-13th non-Friday must not match.
	if c.Matches(time.Date(2026, 6, 18, 0, 0, 0, 0, utc)) {
		t.Fatal("did not expect match on a non-13th, non-Friday")
	}
}

func TestPrevFireBasic(t *testing.T) {
	c := mustParseSpec(t, "0 7 * * *") // 07:00 daily
	utc := time.UTC
	now := time.Date(2026, 6, 18, 9, 15, 0, 0, utc)
	got, ok := c.PrevFire(now, utc, 24*time.Hour)
	if !ok {
		t.Fatal("expected a previous fire")
	}
	want := time.Date(2026, 6, 18, 7, 0, 0, 0, utc)
	if !got.Equal(want) {
		t.Fatalf("PrevFire = %v, want %v", got, want)
	}
}

func TestPrevFireRespectsLookback(t *testing.T) {
	c := mustParseSpec(t, "0 5 1 * *") // 05:00 on the 1st (monthly)
	utc := time.UTC
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, utc) // ~17 days after the 1st
	if _, ok := c.PrevFire(now, utc, 6*time.Hour); ok {
		t.Fatal("monthly fire should be outside a 6h lookback")
	}
	if _, ok := c.PrevFire(now, utc, 30*24*time.Hour); !ok {
		t.Fatal("monthly fire should be found within a 30d lookback")
	}
}

// TestPrevFireSpringForward: on the spring-forward day the wall-clock 02:30
// does not exist, so a 02:30 schedule must skip that day rather than firing.
func TestPrevFireSpringForward(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	c := mustParseSpec(t, "30 2 * * *") // 02:30 daily

	// 2026-03-08 is the US spring-forward day: 02:00 -> 03:00, so 02:30 is skipped.
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, ny)
	got, ok := c.PrevFire(now, ny, 48*time.Hour)
	if !ok {
		t.Fatal("expected a previous fire within 48h")
	}
	wall := got.In(ny)
	if wall.Hour() != 2 || wall.Minute() != 30 {
		t.Fatalf("expected wall-clock 02:30, got %v", wall)
	}
	if wall.Day() != 7 {
		t.Fatalf("expected the fire to fall on 03-07 (02:30 skipped on 03-08), got day %d (%v)", wall.Day(), wall)
	}
}

// TestPrevFireFallBack: on the fall-back day the wall-clock 01:30 happens twice.
// Both instants match, and PrevFire returns the later (standard-time) one.
func TestPrevFireFallBack(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	c := mustParseSpec(t, "30 1 * * *") // 01:30 daily

	// 2026-11-01: 02:00 EDT -> 01:00 EST, so 01:30 occurs at 05:30 UTC (EDT)
	// and again at 06:30 UTC (EST).
	edt := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	est := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	if !c.Matches(edt.In(ny)) || !c.Matches(est.In(ny)) {
		t.Fatal("both 01:30 instants should match")
	}

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, ny)
	got, ok := c.PrevFire(now, ny, 24*time.Hour)
	if !ok {
		t.Fatal("expected a previous fire")
	}
	if got.In(ny).Hour() != 1 || got.In(ny).Minute() != 30 {
		t.Fatalf("expected wall-clock 01:30, got %v", got.In(ny))
	}
	if !got.Equal(est) {
		t.Fatalf("expected the later (EST) 01:30 instant %v, got %v", est, got)
	}
}

// prevFireNaive is the minute-by-minute scan PrevFire used before it learned to
// skip non-matching dates. It is kept as the reference implementation for
// TestPrevFireMatchesNaive: the skip is meant to be a pure speedup, so any
// disagreement between the two is a bug in the skip.
func prevFireNaive(c CronSpec, now time.Time, loc *time.Location, maxLookback time.Duration) (time.Time, bool) {
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

// TestPrevFireMatchesNaive is the backbone guarding the day-skip. It sweeps
// zones chosen for awkward transitions - midnight gaps (Havana, Beirut, Cairo,
// Santiago), a three-hour gap (Casey), a half-hour DST (Lord Howe) and a whole
// skipped local day (Apia) - against the pre-optimization scan.
func TestPrevFireMatchesNaive(t *testing.T) {
	zones := []string{
		"UTC",
		"America/New_York",
		"America/Havana",
		"Asia/Beirut",
		"Africa/Cairo",
		"America/Santiago",
		"Antarctica/Casey",
		"Pacific/Apia",
		"Australia/Lord_Howe",
	}
	crons := []string{
		"* * * * *",
		"0 7 * * *",
		"30 2 * * *",
		"30 1 * * *",
		"0 0 * * *",
		"59 23 * * *",
		"0 5 1 * *",
		"0 0 13 * 5",
		"*/17 * * * *",
		"0 5 29 2 *",
	}
	// Anchored on real transitions: US spring-forward and fall-back, Havana's
	// midnight gap, Casey's three-hour jump, Apia's skipped day, plus a neutral
	// date and a leap day.
	starts := []time.Time{
		time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 9, 3, 0, 0, 0, time.UTC),
		time.Date(2016, 10, 23, 6, 0, 0, 0, time.UTC),
		time.Date(2012, 1, 1, 6, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 18, 9, 15, 0, 0, time.UTC),
		time.Date(2024, 3, 1, 0, 30, 0, 0, time.UTC),
	}
	lookbacks := []time.Duration{6 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour}

	for _, zone := range zones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("tzdata unavailable: %v", err)
		}
		for _, cron := range crons {
			c := mustParseSpec(t, cron)
			for _, start := range starts {
				for _, lb := range lookbacks {
					wantT, wantOK := prevFireNaive(c, start, loc, lb)
					gotT, gotOK := c.PrevFire(start, loc, lb)
					if gotOK != wantOK || (wantOK && !gotT.Equal(wantT)) {
						t.Fatalf("PrevFire(%s, %s, %v, %v) = (%v, %v), naive scan = (%v, %v)",
							cron, zone, start, lb, gotT, gotOK, wantT, wantOK)
					}
				}
			}
		}
	}
}

// TestPrevFireMidnightGapZone pins the reason prevDateEnd does not build the day
// boundary with time.Date. Havana's clocks jump 00:00 -> 01:00 on 2026-03-08, so
// time.Date(2026, 3, 8, 0, 0, 0, 0, havana) resolves backwards to 2026-03-07
// 23:00; stepping a minute off that would skip 23:00-23:59 on the 7th and miss
// this schedule entirely.
func TestPrevFireMidnightGapZone(t *testing.T) {
	havana, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	c := mustParseSpec(t, "59 23 7 3 *") // 23:59 on March 7th

	now := time.Date(2026, 3, 8, 12, 0, 0, 0, havana)
	got, ok := c.PrevFire(now, havana, 48*time.Hour)
	if !ok {
		t.Fatal("expected to find 03-07 23:59 within 48h")
	}
	wall := got.In(havana)
	if wall.Day() != 7 || wall.Hour() != 23 || wall.Minute() != 59 {
		t.Fatalf("expected wall-clock 03-07 23:59, got %v", wall)
	}
}

// TestPrevFireSkippedLocalDay covers the largest transition in tzdata: Apia
// crossed the date line at the end of 2011, so 2011-12-30 never happened
// locally. A schedule on that date can never fire; the day before still can.
func TestPrevFireSkippedLocalDay(t *testing.T) {
	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	now := time.Date(2012, 1, 5, 12, 0, 0, 0, apia)

	missing := mustParseSpec(t, "0 12 30 12 *")
	if got, ok := missing.PrevFire(now, apia, 30*24*time.Hour); ok {
		t.Fatalf("2011-12-30 never existed in Apia, got %v", got.In(apia))
	}
	real := mustParseSpec(t, "0 12 29 12 *")
	got, ok := real.PrevFire(now, apia, 30*24*time.Hour)
	if !ok {
		t.Fatal("expected 2011-12-29 12:00 within 30d")
	}
	if wall := got.In(apia); wall.Day() != 29 || wall.Hour() != 12 {
		t.Fatalf("expected wall-clock 12-29 12:00, got %v", wall)
	}
}

// TestPrevFireYearLookback exercises the window the raised catch_up ceiling
// allows, including the worst case: a spec that can never match must terminate
// rather than grind through half a million minutes.
func TestPrevFireYearLookback(t *testing.T) {
	utc := time.UTC
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, utc)
	year := 366 * 24 * time.Hour

	monthly := mustParseSpec(t, "0 5 1 * *")
	got, ok := monthly.PrevFire(now, utc, year)
	if !ok {
		t.Fatal("expected the monthly occurrence within a year")
	}
	if want := time.Date(2026, 6, 1, 5, 0, 0, 0, utc); !got.Equal(want) {
		t.Fatalf("PrevFire = %v, want %v", got, want)
	}

	// February 30th never exists, so this scans the entire window and finds
	// nothing - the case that used to cost 525,600 iterations.
	never := mustParseSpec(t, "0 5 30 2 *")
	if _, ok := never.PrevFire(now, utc, year); ok {
		t.Fatal("February 30th should never match")
	}
}

// nextFireNaive is the forward counterpart of prevFireNaive: the reference
// minute-by-minute scan that nextDateStart's day-skip must agree with exactly.
func nextFireNaive(c CronSpec, now time.Time, loc *time.Location, maxLookahead time.Duration) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	base := now.Truncate(time.Minute)
	cand := base.Add(time.Minute)
	limit := base.Add(maxLookahead)
	for !cand.After(limit) {
		if c.Matches(cand.In(loc)) {
			return cand, true
		}
		cand = cand.Add(time.Minute)
	}
	return time.Time{}, false
}

// TestNextFireMatchesNaive mirrors TestPrevFireMatchesNaive over the same
// zones, expressions and anchors. nextDateStart is meant to be a pure speedup
// over the scan, so any disagreement is a bug in the skip.
func TestNextFireMatchesNaive(t *testing.T) {
	zones := []string{
		"UTC",
		"America/New_York",
		"America/Havana",
		"Asia/Beirut",
		"Africa/Cairo",
		"America/Santiago",
		"Antarctica/Casey",
		"Pacific/Apia",
		"Australia/Lord_Howe",
	}
	crons := []string{
		"* * * * *",
		"0 7 * * *",
		"30 2 * * *",
		"30 1 * * *",
		"0 0 * * *",
		"59 23 * * *",
		"0 5 1 * *",
		"0 0 13 * 5",
		"*/17 * * * *",
		"0 5 29 2 *",
	}
	starts := []time.Time{
		time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 7, 3, 0, 0, 0, time.UTC),
		time.Date(2016, 10, 21, 6, 0, 0, 0, time.UTC),
		time.Date(2011, 12, 28, 6, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 18, 9, 15, 0, 0, time.UTC),
		time.Date(2024, 2, 27, 0, 30, 0, 0, time.UTC),
	}
	lookaheads := []time.Duration{6 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour}

	for _, zone := range zones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("tzdata unavailable: %v", err)
		}
		for _, cron := range crons {
			c := mustParseSpec(t, cron)
			for _, start := range starts {
				for _, la := range lookaheads {
					wantT, wantOK := nextFireNaive(c, start, loc, la)
					gotT, gotOK := c.NextFire(start, loc, la)
					if gotOK != wantOK || (wantOK && !gotT.Equal(wantT)) {
						t.Fatalf("NextFire(%s, %s, %v, %v) = (%v, %v), naive scan = (%v, %v)",
							cron, zone, start, la, gotT, gotOK, wantT, wantOK)
					}
				}
			}
		}
	}
}

// TestNextFireSpringForward covers the wall-clock hour that does not exist:
// 02:30 never happens in New York on 2026-03-08, so the next fire is the 9th.
func TestNextFireSpringForward(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	c := mustParseSpec(t, "30 2 * * *")

	now := time.Date(2026, 3, 7, 12, 0, 0, 0, ny)
	got, ok := c.NextFire(now, ny, 7*24*time.Hour)
	if !ok {
		t.Fatal("expected a fire within a week")
	}
	if wall := got.In(ny); wall.Day() != 9 || wall.Hour() != 2 || wall.Minute() != 30 {
		t.Fatalf("expected wall-clock 03-09 02:30 (the 8th is skipped), got %v", wall)
	}
}

// TestNextFireFallBack covers the repeated hour. 01:30 happens twice on
// 2026-11-01 in New York; NextFire returns the earlier (EDT) instant, which is
// the mirror of PrevFire returning the later one.
func TestNextFireFallBack(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	c := mustParseSpec(t, "30 1 * * *")

	now := time.Date(2026, 11, 1, 0, 0, 0, 0, ny)
	got, ok := c.NextFire(now, ny, 24*time.Hour)
	if !ok {
		t.Fatal("expected a fire within a day")
	}
	if wall := got.In(ny); wall.Day() != 1 || wall.Hour() != 1 || wall.Minute() != 30 {
		t.Fatalf("expected wall-clock 11-01 01:30, got %v", wall)
	}
	if _, offset := got.In(ny).Zone(); offset != -4*3600 {
		t.Fatalf("expected the first (EDT, -0400) of the two 01:30s, got offset %ds", offset)
	}
}

// TestNextFireMidnightGapZone is the forward twin of
// TestPrevFireMidnightGapZone: Havana's clocks jump 00:00 -> 01:00 on
// 2026-03-08, so a boundary built with time.Date would land on the 7th and
// nextDateStart must not rely on it.
func TestNextFireMidnightGapZone(t *testing.T) {
	havana, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	c := mustParseSpec(t, "30 1 8 3 *") // 01:30 on March 8th, just past the gap

	now := time.Date(2026, 3, 6, 12, 0, 0, 0, havana)
	got, ok := c.NextFire(now, havana, 7*24*time.Hour)
	if !ok {
		t.Fatal("expected 03-08 01:30 within a week")
	}
	if wall := got.In(havana); wall.Day() != 8 || wall.Hour() != 1 || wall.Minute() != 30 {
		t.Fatalf("expected wall-clock 03-08 01:30, got %v", wall)
	}
}

// TestNextFireSkippedLocalDay is the forward twin of
// TestPrevFireSkippedLocalDay: Apia's 2011-12-30 never happened, so a schedule
// on it can never fire while the day after it can.
func TestNextFireSkippedLocalDay(t *testing.T) {
	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	now := time.Date(2011, 12, 28, 12, 0, 0, 0, apia)

	missing := mustParseSpec(t, "0 12 30 12 *")
	if got, ok := missing.NextFire(now, apia, 30*24*time.Hour); ok {
		t.Fatalf("2011-12-30 never existed in Apia, got %v", got.In(apia))
	}
	real := mustParseSpec(t, "0 12 31 12 *")
	got, ok := real.NextFire(now, apia, 30*24*time.Hour)
	if !ok {
		t.Fatal("expected 2011-12-31 12:00 within 30d")
	}
	if wall := got.In(apia); wall.Day() != 31 || wall.Hour() != 12 {
		t.Fatalf("expected wall-clock 12-31 12:00, got %v", wall)
	}
}

// TestNextFireStrictlyAfterNow pins that an occurrence firing this very minute
// reports the following one, so a schedule column never reads as permanently
// due.
func TestNextFireStrictlyAfterNow(t *testing.T) {
	utc := time.UTC
	c := mustParseSpec(t, "0 7 * * *")

	now := time.Date(2026, 6, 18, 7, 0, 30, 0, utc)
	got, ok := c.NextFire(now, utc, 48*time.Hour)
	if !ok {
		t.Fatal("expected tomorrow's occurrence")
	}
	if want := time.Date(2026, 6, 19, 7, 0, 0, 0, utc); !got.Equal(want) {
		t.Fatalf("NextFire = %v, want %v", got, want)
	}
}

// TestNextFireHorizon covers the bound: a reachable sparse expression is found,
// an unreachable one terminates instead of grinding the whole window.
func TestNextFireHorizon(t *testing.T) {
	utc := time.UTC
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, utc)

	monthly := mustParseSpec(t, "0 5 1 * *")
	got, ok := monthly.NextFire(now, utc, NextFireHorizon)
	if !ok {
		t.Fatal("expected the monthly occurrence within a year")
	}
	if want := time.Date(2026, 7, 1, 5, 0, 0, 0, utc); !got.Equal(want) {
		t.Fatalf("NextFire = %v, want %v", got, want)
	}

	never := mustParseSpec(t, "0 5 30 2 *")
	if _, ok := never.NextFire(now, utc, NextFireHorizon); ok {
		t.Fatal("February 30th should never match")
	}
}

// TestNextFireLeapDay is why the horizon is a decade and not a year. February
// 29th is a legal schedule whose occurrences sit up to eight years apart across
// a century non-leap year, and a one-year bound reports it as never firing.
func TestNextFireLeapDay(t *testing.T) {
	utc := time.UTC
	c := mustParseSpec(t, "0 5 29 2 *")

	// Two years out, already beyond a one-year horizon.
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, utc)
	got, ok := c.NextFire(now, utc, NextFireHorizon)
	if !ok {
		t.Fatal("expected 2028-02-29 to be reachable")
	}
	if want := time.Date(2028, 2, 29, 5, 0, 0, 0, utc); !got.Equal(want) {
		t.Fatalf("NextFire = %v, want %v", got, want)
	}

	// The century gap itself: 2100 is not a leap year, so 2096 jumps to 2104.
	now = time.Date(2096, 3, 1, 0, 0, 0, 0, utc)
	got, ok = c.NextFire(now, utc, NextFireHorizon)
	if !ok {
		t.Fatal("expected 2104-02-29 to be reachable across the century gap")
	}
	if want := time.Date(2104, 2, 29, 5, 0, 0, 0, utc); !got.Equal(want) {
		t.Fatalf("NextFire = %v, want %v", got, want)
	}
}

// TestNextDateStartMakesProgress pins the termination argument directly: the
// day-skip must always move forward, in every zone, including across the
// transitions that make a local date shorter or longer than 24 hours.
func TestNextDateStartMakesProgress(t *testing.T) {
	zones := []string{
		"UTC", "America/New_York", "America/Havana", "Asia/Beirut",
		"Africa/Cairo", "America/Santiago", "Antarctica/Casey",
		"Pacific/Apia", "Australia/Lord_Howe",
	}
	starts := []time.Time{
		time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2011, 12, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2016, 10, 22, 0, 0, 0, 0, time.UTC),
	}
	for _, zone := range zones {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("tzdata unavailable: %v", err)
		}
		for _, start := range starts {
			// Sweep two days a minute at a time around each transition.
			for i := 0; i < 2*24*60; i++ {
				cand := start.Add(time.Duration(i) * time.Minute)
				next := nextDateStart(cand, loc)
				if !next.After(cand) {
					t.Fatalf("nextDateStart(%v, %s) = %v, must move forward", cand, zone, next)
				}
				// It must land on a different local date, and the minute before
				// it must still be on cand's.
				cy, cm, cd := cand.In(loc).Date()
				if onDate(next, loc, cy, cm, cd) {
					t.Fatalf("nextDateStart(%v, %s) = %v is still on the same local date", cand, zone, next)
				}
				if !onDate(next.Add(-time.Minute), loc, cy, cm, cd) {
					t.Fatalf("nextDateStart(%v, %s) = %v overshot past the date boundary", cand, zone, next)
				}
			}
		}
	}
}

// BenchmarkNextFireSparseYear is the forward twin of
// BenchmarkPrevFireSparseYear: ls computes a next fire per schedule job on
// every invocation, so the day-skip has to hold here too.
func BenchmarkNextFireSparseYear(b *testing.B) {
	c, err := ParseSpec("0 5 30 2 *")
	if err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.NextFire(now, time.UTC, NextFireHorizon)
	}
}

// BenchmarkPrevFireSparseYear pins the day-skip's payoff, which is what makes a
// year-long catch_up affordable at one supervisor tick per minute. Measured on
// an M2 Pro: the naive minute-by-minute scan took 19.2ms for this case, the
// day-skip 24us.
func BenchmarkPrevFireSparseYear(b *testing.B) {
	c, err := ParseSpec("0 5 30 2 *")
	if err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	year := 366 * 24 * time.Hour
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.PrevFire(now, time.UTC, year)
	}
}
