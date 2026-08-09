package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/launchd"
	"github.com/amterp/beagle/internal/runlog"
)

func scheduleStatus(id, cron, tz string) launchd.JobStatus {
	return launchd.JobStatus{
		ResolvedJob: config.ResolvedJob{
			ID:       id,
			Type:     "schedule",
			Enabled:  true,
			Schedule: config.Schedule{Cron: cron, Timezone: tz},
		},
		Loaded: true,
	}
}

func serviceStatus(id string, pid int) launchd.JobStatus {
	return launchd.JobStatus{
		ResolvedJob: config.ResolvedJob{ID: id, Type: "service", Enabled: true},
		Loaded:      true,
		PID:         pid,
	}
}

func TestCompactDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0ms"},
		{20 * time.Millisecond, "20ms"},
		{669 * time.Millisecond, "669ms"},
		{6 * time.Second, "6s"},
		{74 * time.Second, "1m 14s"},
		{20 * time.Minute, "20m"},
		{4*time.Hour + 5*time.Minute, "4h 5m"},
		{6 * time.Hour, "6h"},
		{25 * time.Hour, "1d 1h"},
		{24 * time.Hour, "1d"},
		{22*24*time.Hour + 17*time.Hour, "22d 17h"},
		{-time.Hour, "0ms"},
	}
	for _, tc := range cases {
		if got := compactDuration(tc.in); got != tc.want {
			t.Errorf("compactDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestHumanizeCronLiveShapes covers every cron expression in the real
// ~/.beagle/jobs.yaml, so the column is known to render the config it exists to
// describe rather than only the shapes that were convenient to implement.
func TestHumanizeCronLiveShapes(t *testing.T) {
	cases := map[string]string{
		"0 18 * * *":   "18:00 daily",
		"0 6 * * *":    "06:00 daily",
		"0 3 * * *":    "03:00 daily",
		"0 4 * * *":    "04:00 daily",
		"0 5 * * *":    "05:00 daily",
		"0 7 * * *":    "07:00 daily",
		"15 5 * * *":   "05:15 daily",
		"0 12 * * 1":   "12:00 Mon",
		"30 7 * * 5":   "07:30 Fri",
		"*/15 * * * *": "every 15m",
		"30 4 1 * *":   "04:30 on the 1st",
		"45 4 1 * *":   "04:45 on the 1st",
		"0 5 1 * *":    "05:00 on the 1st",
		"* * * * *":    "every minute",
		"0 0 2 * *":    "00:00 on the 2nd",
		"0 0 3 * *":    "00:00 on the 3rd",
		"0 0 11 * *":   "00:00 on the 11th",
		"0 0 21 * *":   "00:00 on the 21st",
		"30 * * * *":   "hourly at :30",
	}
	for cron, want := range cases {
		if got := humanizeCron(cron); got != want {
			t.Errorf("humanizeCron(%q) = %q, want %q", cron, got, want)
		}
	}
}

// TestHumanizeCronFallsBack is the other half of the contract. A column that
// guesses wrong about when a job runs is worse than one that shows cron, so
// anything not recognised exactly must come back verbatim.
func TestHumanizeCronFallsBack(t *testing.T) {
	// "the 13th OR any Friday" under the Vixie dom/dow rule, not "Friday the
	// 13th" - the mistake most cron humanizers make. Phrasing it safely is not
	// worth the words, so it is not phrased at all.
	raw := []string{
		"0 0 13 * 5",
		"0 9-17 * * *",
		"0,15,30,45 * * * *",
		"0 0 1 1 *",
		"0 0 * * 1-5",
		"*/15 */2 * * *",
		"0 99 * * *",
		"99 0 * * *",
		"0 0 32 * *",
		"0 0 * * 9",
		"nonsense",
		"* * * *",
		"* * * * * *",
		"",
	}
	for _, cron := range raw {
		if got := humanizeCron(cron); got != cron {
			t.Errorf("humanizeCron(%q) = %q, want it returned verbatim", cron, got)
		}
	}
}

// TestHumanizeCronNeverLiesAboutUnparseable ties "never lie" to a property a
// machine can check, rather than to a hand-maintained list: if the scheduler
// itself cannot parse the expression, prose about it would be invention.
func TestHumanizeCronNeverLiesAboutUnparseable(t *testing.T) {
	candidates := []string{
		"0 99 * * *", "99 0 * * *", "0 0 32 * *", "0 0 0 * *", "0 0 * 13 *",
		"0 0 * * 9", "*/0 * * * *", "a b c d e", "", "* * * *", "0 -1 * * *",
	}
	for _, cron := range candidates {
		if _, err := launchd.ParseSpec(cron); err == nil {
			continue
		}
		if got := humanizeCron(cron); got != cron {
			t.Errorf("humanizeCron(%q) = %q, but the scheduler rejects that expression", cron, got)
		}
	}
}

func TestZoneCell(t *testing.T) {
	const machine = "Europe/Lisbon"
	cases := map[string]string{
		"America/Chicago":  "Chicago",
		"America/New_York": "New_York",
		"Europe/Lisbon":    "", // matches the machine, so saying so is noise
		"local":            "local",
		"Local":            "local",
		"":                 "UTC", // unset means UTC, which differs from the machine
		"UTC":              "UTC",
	}
	for tz, want := range cases {
		if got := plain(zoneCell(tz, machine)); got != want {
			t.Errorf("zoneCell(%q, %q) = %q, want %q", tz, machine, got, want)
		}
	}
}

// TestUptimeCellRejectsStaleRunRow is the case that motivates checking the PID
// rather than trusting status alone. A run killed before it could write its
// finish leaves the row saying "running" forever; without the PID check a dead
// service reports a confidently growing uptime.
func TestUptimeCellRejectsStaleRunRow(t *testing.T) {
	now := time.Date(2026, 8, 9, 17, 0, 0, 0, time.UTC)
	started := now.Add(-4 * time.Hour)
	live := runlog.RunSummary{Status: "running", StartedAt: started, PID: 724}

	if got := plain(uptimeCell(serviceStatus("kan", 724), live, true, now)); got != "4h" {
		t.Errorf("a live service should report uptime, got %q", got)
	}
	if got := plain(uptimeCell(serviceStatus("kan", 0), live, true, now)); got != "-" {
		t.Errorf("launchd running nothing must not report uptime, got %q", got)
	}
	if got := plain(uptimeCell(serviceStatus("kan", 999), live, true, now)); got != "-" {
		t.Errorf("a stale row from a different process must not report uptime, got %q", got)
	}
	done := runlog.RunSummary{Status: "succeeded", StartedAt: started, PID: 724}
	if got := plain(uptimeCell(serviceStatus("kan", 724), done, true, now)); got != "-" {
		t.Errorf("a finished run is not an uptime, got %q", got)
	}
	if got := plain(uptimeCell(serviceStatus("kan", 724), runlog.RunSummary{}, false, now)); got != "-" {
		t.Errorf("no run record means no uptime, got %q", got)
	}
}

func TestServiceStateCell(t *testing.T) {
	running := runlog.RunSummary{Status: "running"}
	failed := runlog.RunSummary{Status: "failed", ExitCode: 3}

	if got := plain(serviceStateCell(serviceStatus("a", 724), running, true)); got != "● running" {
		t.Errorf("got %q, want ● running", got)
	}
	// Loaded with no process and a failed last run: the exit code is the whole
	// point of splitting this from stateCell, which would only say "loaded".
	if got := plain(serviceStateCell(serviceStatus("a", 0), failed, true)); got != "✗ exit 3" {
		t.Errorf("got %q, want ✗ exit 3", got)
	}
	if got := plain(serviceStateCell(serviceStatus("a", 0), runlog.RunSummary{}, false)); got != "○ not running" {
		t.Errorf("got %q, want ○ not running", got)
	}
	paused := serviceStatus("a", 0)
	paused.Disabled = true
	if got := plain(serviceStateCell(paused, running, true)); got != "⏸ paused" {
		t.Errorf("got %q, want ⏸ paused", got)
	}
	unloaded := serviceStatus("a", 0)
	unloaded.Loaded = false
	if got := plain(serviceStateCell(unloaded, running, true)); got != "○ stopped" {
		t.Errorf("got %q, want ○ stopped", got)
	}
}

func TestNextCell(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	if got := plain(nextCell(scheduleStatus("a", "0 18 * * *", "UTC"), now)); got != "in 6h" {
		t.Errorf("nextCell = %q, want in 6h", got)
	}
	// February 30th cannot happen. Reporting that is the point of the horizon.
	if got := plain(nextCell(scheduleStatus("a", "0 0 30 2 *", "UTC"), now)); got != "· never" {
		t.Errorf("nextCell = %q, want · never", got)
	}
	// A cron that passes validation's five-field shape check but that the
	// scheduler rejects surfaces here rather than only in the supervisor log.
	if got := plain(nextCell(scheduleStatus("a", "0 99 * * *", "UTC"), now)); got != "✗ bad cron" {
		t.Errorf("nextCell = %q, want ✗ bad cron", got)
	}
	// A paused job keeps its schedule but must not read as a live countdown.
	paused := scheduleStatus("a", "0 18 * * *", "UTC")
	paused.Disabled = true
	if got := plain(nextCell(paused, now)); got != "in 6h" {
		t.Errorf("nextCell = %q, want the dimmed countdown text", got)
	}
}

func TestListSectionsSplitsByType(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	items := []launchd.JobStatus{
		serviceStatus("kan_serve", 724),
		scheduleStatus("mail_sync", "*/15 * * * *", "America/Chicago"),
	}
	health := map[string]runlog.RunSummary{
		"kan_serve": {Status: "running", StartedAt: now.Add(-2 * time.Hour), PID: 724},
		"mail_sync": {Status: "succeeded", StartedAt: now.Add(-5 * time.Minute), Duration: 6 * time.Second},
	}

	out := plain(listSections(items, health, now, "Europe/Lisbon"))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	if lines[0] != "SERVICES" {
		t.Fatalf("expected a SERVICES heading first, got %q", lines[0])
	}
	svcHeaders := columns(lines[1])
	wantSvc := []string{"JOB", "ENABLED", "STATE", "UPTIME", "PID"}
	if strings.Join(svcHeaders, "|") != strings.Join(wantSvc, "|") {
		t.Fatalf("service headers = %v, want %v", svcHeaders, wantSvc)
	}

	schedIdx := -1
	for i, l := range lines {
		if l == "SCHEDULES" {
			schedIdx = i
		}
	}
	if schedIdx < 0 {
		t.Fatal("expected a SCHEDULES heading")
	}
	if lines[schedIdx-1] != "" {
		t.Error("expected a blank line between the two sections")
	}

	// TYPE is what the headings replaced; if it came back the split bought
	// nothing.
	if strings.Contains(out, "TYPE") {
		t.Error("TYPE should be gone - the section heading carries it")
	}
	// The zone tag appears for the Chicago job because the machine is elsewhere.
	if !strings.Contains(out, "Chicago") {
		t.Error("expected the foreign zone tagged on the schedule row")
	}
	if !strings.Contains(out, "every 15m") {
		t.Error("expected the humanized schedule")
	}
	if !strings.Contains(out, "724") {
		t.Error("expected the service PID")
	}
}

// TestListSectionsIndependentWidths is the reason for two tables rather than
// one grouped one: a very long service name must not pad the schedules table.
func TestListSectionsIndependentWidths(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	items := []launchd.JobStatus{
		serviceStatus("a_very_long_service_name_indeed", 1),
		scheduleStatus("s", "0 7 * * *", "UTC"),
	}
	out := plain(listSections(items, map[string]runlog.RunSummary{}, now, "UTC"))

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "s ") {
			if got := columns(line)[0]; got != "s" {
				t.Fatalf("schedule row padded to the service table's width: %q", line)
			}
			return
		}
	}
	t.Fatal("did not find the schedule row")
}

func TestListSectionsSkipsEmptySection(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	out := plain(listSections(
		[]launchd.JobStatus{scheduleStatus("only", "0 7 * * *", "UTC")},
		map[string]runlog.RunSummary{}, now, "UTC"))

	if strings.Contains(out, "SERVICES") {
		t.Error("a config with no services should not print an empty SERVICES section")
	}
	if !strings.Contains(out, "SCHEDULES") {
		t.Error("expected the SCHEDULES section")
	}
}
