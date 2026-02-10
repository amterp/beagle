package launchd

import (
	"strings"
	"testing"
)

func TestParseCronSimple(t *testing.T) {
	cals, err := ParseCron("0 5 1 * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(cals))
	}
	c := cals[0]
	if c.Minute == nil || *c.Minute != 0 {
		t.Fatalf("expected minute 0, got %v", c.Minute)
	}
	if c.Hour == nil || *c.Hour != 5 {
		t.Fatalf("expected hour 5, got %v", c.Hour)
	}
	if c.Day == nil || *c.Day != 1 {
		t.Fatalf("expected day 1, got %v", c.Day)
	}
	if c.Month != nil {
		t.Fatalf("expected nil month, got %v", c.Month)
	}
	if c.Weekday != nil {
		t.Fatalf("expected nil weekday, got %v", c.Weekday)
	}
}

func TestParseCronAllWildcard(t *testing.T) {
	cals, err := ParseCron("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 1 {
		t.Fatalf("expected 1 calendar (all nil), got %d", len(cals))
	}
	c := cals[0]
	if c.Minute != nil || c.Hour != nil || c.Day != nil || c.Month != nil || c.Weekday != nil {
		t.Fatalf("all-wildcard should have all nil fields: %+v", c)
	}
}

func TestParseCronStep(t *testing.T) {
	// */15 in minute field: 0,15,30,45
	cals, err := ParseCron("*/15 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 4 {
		t.Fatalf("expected 4 calendars for */15, got %d", len(cals))
	}
	minutes := make([]int, len(cals))
	for i, c := range cals {
		if c.Minute == nil {
			t.Fatal("expected non-nil minute")
		}
		minutes[i] = *c.Minute
	}
	expected := []int{0, 15, 30, 45}
	for i, want := range expected {
		if minutes[i] != want {
			t.Fatalf("entry %d: expected minute %d, got %d", i, want, minutes[i])
		}
	}
}

func TestParseCronRange(t *testing.T) {
	// 1-5 in weekday field
	cals, err := ParseCron("0 9 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 5 {
		t.Fatalf("expected 5 calendars for 1-5 weekday, got %d", len(cals))
	}
}

func TestParseCronList(t *testing.T) {
	// 1,3,5 in weekday field
	cals, err := ParseCron("0 9 * * 1,3,5")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 3 {
		t.Fatalf("expected 3 calendars for 1,3,5, got %d", len(cals))
	}
}

func TestParseCronMixed(t *testing.T) {
	// */15 minutes, 9-17 hours, Mon-Fri
	cals, err := ParseCron("*/15 9-17 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	// 4 minutes * 9 hours * 5 weekdays = 180
	if len(cals) != 180 {
		t.Fatalf("expected 180 calendars, got %d", len(cals))
	}
}

func TestParseCronSteppedRange(t *testing.T) {
	// 0-30/10 in minute
	cals, err := ParseCron("0-30/10 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	// 0, 10, 20, 30
	if len(cals) != 4 {
		t.Fatalf("expected 4 calendars for 0-30/10, got %d", len(cals))
	}
}

func TestParseCronOverflowRejected(t *testing.T) {
	// This would generate too many entries
	_, err := ParseCron("* * 1-31 1-12 0-6")
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !strings.Contains(err.Error(), "max") {
		t.Fatalf("expected max entries error, got: %v", err)
	}
}

func TestParseCronInvalidFieldCount(t *testing.T) {
	_, err := ParseCron("* * *")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseCronOutOfRange(t *testing.T) {
	_, err := ParseCron("60 * * * *")
	if err == nil {
		t.Fatal("expected error for minute 60")
	}
}

func TestParseCronStepSingleBase(t *testing.T) {
	// 5/10 should generate: 5, 15, 25, 35, 45, 55
	cals, err := ParseCron("5/10 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 6 {
		t.Fatalf("expected 6 calendars for 5/10, got %d", len(cals))
	}
}

func TestParseCronInvalidMonthDay(t *testing.T) {
	// Month 0 is invalid (months are 1-12)
	_, err := ParseCron("* * * 0 *")
	if err == nil {
		t.Fatal("expected error for month 0")
	}
	// Day 0 is invalid (days are 1-31)
	_, err = ParseCron("* * 0 * *")
	if err == nil {
		t.Fatal("expected error for day 0")
	}
}

func TestParseCronStepZeroNegative(t *testing.T) {
	_, err := ParseCron("*/0 * * * *")
	if err == nil {
		t.Fatal("expected error for step 0")
	}
	_, err = ParseCron("*/-1 * * * *")
	if err == nil {
		t.Fatal("expected error for negative step")
	}
}
