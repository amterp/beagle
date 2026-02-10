package launchd

import (
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/config"
)

func TestBuildAndRenderServicePlist(t *testing.T) {
	rj := config.ResolvedJob{
		ID:      "worker_a",
		Type:    "service",
		Command: []string{"/usr/local/bin/worker"},
		Restart: "on-failure",
		Enabled: true,
	}

	spec, err := BuildSpec("com.beagle.test.worker_a", rj, "beagle-run", "/tmp/stdout.log", "/tmp/stderr.log", "team-a")
	if err != nil {
		t.Fatal(err)
	}

	plist, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Fatalf("expected KeepAlive in plist: %s", plist)
	}
	if !strings.Contains(plist, "<key>ProgramArguments</key>") {
		t.Fatalf("expected ProgramArguments in plist: %s", plist)
	}
}

func TestBuildAndRenderSchedulePlist(t *testing.T) {
	rj := config.ResolvedJob{
		ID:      "monthly_report",
		Type:    "schedule",
		Command: []string{"/usr/local/bin/report"},
		Enabled: true,
		Schedule: config.Schedule{
			Cron: "0 5 1 * *",
		},
	}

	spec, err := BuildSpec("com.beagle.test.monthly_report", rj, "beagle-run", "/tmp/stdout.log", "/tmp/stderr.log", "team-a")
	if err != nil {
		t.Fatal(err)
	}

	plist, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plist, "<key>StartCalendarInterval</key>") {
		t.Fatalf("expected StartCalendarInterval in plist: %s", plist)
	}
	if !strings.Contains(plist, "<key>Hour</key>") || !strings.Contains(plist, "<integer>5</integer>") {
		t.Fatalf("expected hour value in schedule plist: %s", plist)
	}
}

func TestRenderPlistSingleCalendarUsesDict(t *testing.T) {
	// A single calendar entry should render as a bare <dict>, not wrapped in <array>.
	spec := JobSpec{
		Label:       "com.beagle.test.single",
		ProgramArgs: []string{"/bin/echo"},
		StdoutPath:  "/tmp/out.log",
		StderrPath:  "/tmp/err.log",
		Type:        "schedule",
		Enabled:     true,
		Calendars:   []Calendar{{Minute: intPtr(30), Hour: intPtr(9)}},
	}

	plist, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plist, "<key>StartCalendarInterval</key>") {
		t.Fatalf("missing StartCalendarInterval")
	}
	// Single entry: dict should follow immediately after the key, no <array> wrapper.
	calIdx := strings.Index(plist, "<key>StartCalendarInterval</key>")
	afterCal := plist[calIdx:]
	// The next element after the key line should be <dict>, not <array>.
	if strings.Contains(afterCal, "<array>") {
		t.Fatalf("single calendar should not use <array> after StartCalendarInterval:\n%s", afterCal)
	}
	if !strings.Contains(plist, "<key>Minute</key>") || !strings.Contains(plist, "<integer>30</integer>") {
		t.Fatalf("missing minute in plist:\n%s", plist)
	}
	if !strings.Contains(plist, "<key>Hour</key>") || !strings.Contains(plist, "<integer>9</integer>") {
		t.Fatalf("missing hour in plist:\n%s", plist)
	}
}

func TestRenderPlistMultipleCalendarsUsesArray(t *testing.T) {
	// Multiple calendar entries should be wrapped in <array>.
	spec := JobSpec{
		Label:       "com.beagle.test.multi",
		ProgramArgs: []string{"/bin/echo"},
		StdoutPath:  "/tmp/out.log",
		StderrPath:  "/tmp/err.log",
		Type:        "schedule",
		Enabled:     true,
		Calendars: []Calendar{
			{Minute: intPtr(0), Hour: intPtr(9), Weekday: intPtr(1)},
			{Minute: intPtr(0), Hour: intPtr(9), Weekday: intPtr(3)},
			{Minute: intPtr(0), Hour: intPtr(9), Weekday: intPtr(5)},
		},
	}

	plist, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plist, "<key>StartCalendarInterval</key>") {
		t.Fatalf("missing StartCalendarInterval")
	}
	if !strings.Contains(plist, "<array>") {
		t.Fatalf("multiple calendars should use <array>:\n%s", plist)
	}

	// Should have 3 <dict> blocks inside the array (one per weekday).
	count := strings.Count(plist, "<key>Weekday</key>")
	if count != 3 {
		t.Fatalf("expected 3 Weekday entries, got %d:\n%s", count, plist)
	}
}

func TestRenderPlistScheduleViaBuiltSpec(t *testing.T) {
	// End-to-end: cron expression with multiple values produces array rendering.
	rj := config.ResolvedJob{
		ID:      "frequent",
		Type:    "schedule",
		Command: []string{"/usr/local/bin/ping"},
		Enabled: true,
		Schedule: config.Schedule{
			Cron: "*/15 * * * *",
		},
	}

	spec, err := BuildSpec("com.beagle.test.frequent", rj, "beagle-run", "/tmp/out.log", "/tmp/err.log", "ns")
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Calendars) != 4 {
		t.Fatalf("expected 4 calendars for */15, got %d", len(spec.Calendars))
	}

	plist, err := RenderPlist(spec)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(plist, "<array>") {
		t.Fatalf("*/15 should produce array rendering:\n%s", plist)
	}

	// Verify all 4 minute values appear.
	for _, min := range []string{"0", "15", "30", "45"} {
		expected := "<integer>" + min + "</integer>"
		if !strings.Contains(plist, expected) {
			t.Fatalf("missing minute %s in plist:\n%s", min, plist)
		}
	}
}
