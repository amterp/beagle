package launchd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/config"
)

func TestListUsesLaunchctlInspection(t *testing.T) {
	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo", "hello"},
			},
		},
	}

	runOut := func(name string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "print" {
			return "state = running", nil
		}
		return "", nil
	}

	items, err := List(f, StatusOptions{HomeDir: t.TempDir(), RunOut: runOut})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}
	if !items[0].Loaded {
		t.Fatalf("expected loaded item: %+v", items[0])
	}
}

// TestParsePID pins the shape of the launchctl dump we read the live PID from.
// The dump is Apple's format, not ours, so the anchoring matters: the fixture
// below is trimmed from real output for a running service.
func TestParsePID(t *testing.T) {
	running := `gui/501/com.beagle.amterp.kan_serve = {
	active count = 1
	state = running
	program = /Users/amterp/src/beagle/bin/beagle-run
	arguments = {
		/Users/amterp/src/beagle/bin/beagle-run
		--job
		kan_serve
	}
	runs = 1
	pid = 724
	immediate reason = speculative
	last exit code = (never exited)
}`
	if got := parsePID(running); got != 724 {
		t.Errorf("parsePID = %d, want 724", got)
	}

	// A schedule job's agent is loaded but running nothing, so there is no pid
	// line at all. That must read as "not running", not as a parse failure.
	idle := `gui/501/com.beagle.amterp.mail_sync = {
	active count = 0
	state = not running
	runs = 42
	last exit code = 0
}`
	if got := parsePID(idle); got != 0 {
		t.Errorf("parsePID = %d, want 0 for an idle agent", got)
	}

	if got := parsePID(""); got != 0 {
		t.Errorf("parsePID = %d, want 0 for empty output", got)
	}

	// A job whose own arguments mention a pid must not be mistaken for one.
	// The real anchor is the line start, so this only passes while that holds.
	arged := `gui/501/x = {
	state = not running
	arguments = {
		/bin/mytool
		--pid = 999
	}
}`
	if got := parsePID(arged); got != 0 {
		t.Errorf("parsePID = %d, want 0 - an argument is not the process pid", got)
	}
}

func TestListReportsPID(t *testing.T) {
	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {Type: "service", Command: []string{"/bin/echo", "hello"}},
		},
	}
	runOut := func(name string, args ...string) (string, error) {
		return "\tstate = running\n\tpid = 4242\n", nil
	}
	items, err := List(f, StatusOptions{HomeDir: t.TempDir(), RunOut: runOut})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].PID != 4242 {
		t.Errorf("PID = %d, want 4242", items[0].PID)
	}
	// The embedded config must come through too - it is what ls describes a
	// schedule from.
	if items[0].Type != "service" || items[0].ID != "worker_a" {
		t.Errorf("expected the resolved job embedded, got %+v", items[0].ResolvedJob)
	}
}

func TestDoctorDetectsMissingLaunchctl(t *testing.T) {
	runOut := func(name string, args ...string) (string, error) {
		return "", errors.New("missing")
	}

	report, err := Doctor(StatusOptions{HomeDir: t.TempDir(), RunOut: runOut})
	if err != nil {
		t.Fatal(err)
	}
	if report.LaunchctlOK {
		t.Fatalf("expected launchctl to fail: %+v", report)
	}
	if !strings.Contains(strings.Join(report.Issues, "\n"), "scheduler backend command") {
		t.Fatalf("expected scheduler backend issue, got %+v", report)
	}
}

// doctorWithDump runs Doctor against a canned `launchctl print` dump for the
// supervisor, leaving the other probes to behave normally.
func doctorWithDump(t *testing.T, dump string) DoctorReport {
	t.Helper()
	runOut := func(name string, args ...string) (string, error) {
		if len(args) >= 1 && args[0] == "print" {
			return dump, nil
		}
		return "", nil
	}
	report, err := Doctor(StatusOptions{HomeDir: t.TempDir(), RunOut: runOut})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// TestDoctorFlagsSupervisorLaunchdFailures covers the false green that let a
// dead scheduler pass a health check: the heartbeat in the run DB stays fresh
// for minutes after launchd loses the ability to spawn the supervisor, so
// doctor has to read launchd's own view of the agent. The broken fixture is
// trimmed from real output for a supervisor whose binary a package upgrade
// removed.
func TestDoctorFlagsSupervisorLaunchdFailures(t *testing.T) {
	broken := `gui/501/com.beagle.amterp.supervisor = {
	state = spawn scheduled
	program = /opt/homebrew/Cellar/beagle/0.0.0-removed/bin/beagle
	runs = 226
	last exit code = 78: EX_CONFIG
	properties = runatload | penalty box | inferred program | managed LWCR | has LWCR
}`

	report := doctorWithDump(t, broken)
	if !report.SupervisorLoaded {
		t.Fatalf("a loaded-but-broken agent must still read as loaded: %+v", report)
	}
	if !report.SupervisorProgramMissing || !report.SupervisorThrottled || report.SupervisorLastExit != 78 {
		t.Fatalf("expected a missing program, throttling and exit 78, got %+v", report)
	}
	issues := strings.Join(report.Issues, "\n")
	if !strings.Contains(issues, "beagle apply") {
		t.Errorf("issues must name the command that re-points the agent, got: %s", issues)
	}
	if !strings.Contains(issues, "0.0.0-removed") {
		t.Errorf("issues must name the path that no longer exists, got: %s", issues)
	}

	// The healthy case must stay silent, including launchd's non-numeric
	// "(never exited)" - reading that as a failed tick would nag on every run.
	present := filepath.Join(t.TempDir(), "beagle")
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	healthy := `gui/501/com.beagle.amterp.supervisor = {
	state = not running
	program = ` + present + `
	runs = 5
	last exit code = (never exited)
	properties = runatload | inferred program
}`

	report = doctorWithDump(t, healthy)
	if report.SupervisorProgramMissing || report.SupervisorThrottled || report.SupervisorLastExit != -1 {
		t.Fatalf("expected a clean supervisor, got %+v", report)
	}
	for _, issue := range report.Issues {
		if strings.Contains(issue, "supervisor") {
			t.Errorf("healthy supervisor must raise no supervisor issue, got: %s", issue)
		}
	}
}
