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
