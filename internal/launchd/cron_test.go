package launchd

import (
	"testing"
	"time"
)

// These exercise the shared cron field grammar (parseField/parseStep/
// parseRange) through ParseSpec, which the supervisor evaluates.

func TestParseSpecAcceptsValidForms(t *testing.T) {
	valid := []string{
		"0 5 1 * *",
		"* * * * *",
		"*/15 * * * *",
		"0 9 * * 1-5",
		"0 9 * * 1,3,5",
		"*/15 9-17 * * 1-5",
		"0-30/10 * * * *",
		"5/10 * * * *",
	}
	for _, cron := range valid {
		if _, err := ParseSpec(cron); err != nil {
			t.Errorf("ParseSpec(%q) unexpected error: %v", cron, err)
		}
	}
}

func TestParseSpecRejectsInvalidForms(t *testing.T) {
	invalid := []string{
		"* * *",        // too few fields
		"60 * * * *",   // minute out of range
		"* * * 0 *",    // month 0
		"* * 0 * *",    // day 0
		"*/0 * * * *",  // step zero
		"*/-1 * * * *", // negative step
	}
	for _, cron := range invalid {
		if _, err := ParseSpec(cron); err == nil {
			t.Errorf("ParseSpec(%q) expected error, got nil", cron)
		}
	}
}

// TestParseSpecStepAndListMatch confirms the parsed sets behave as expected
// against concrete instants (replacing the old expansion-count assertions).
func TestParseSpecStepAndListMatch(t *testing.T) {
	c := mustParseSpec(t, "*/15 * * * *") // minutes 0,15,30,45
	at := func(m int) time.Time { return time.Date(2026, 6, 18, 10, m, 0, 0, time.UTC) }
	for _, m := range []int{0, 15, 30, 45} {
		if !c.Matches(at(m)) {
			t.Errorf("expected match at minute %d", m)
		}
	}
	for _, m := range []int{1, 7, 16, 31} {
		if c.Matches(at(m)) {
			t.Errorf("did not expect match at minute %d", m)
		}
	}
}
