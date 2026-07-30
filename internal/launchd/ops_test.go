package launchd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
)

// opsFixture builds a single-job config and writes the launchd agent that
// ops-layer commands expect to find on disk, returning the home dir to pass as
// OpsOptions.HomeDir. Without the agent file, ensureLoaded refuses to bootstrap.
func opsFixture(t *testing.T, jobID string, job config.Job) (config.File, string) {
	t.Helper()
	home := t.TempDir()
	f := config.File{
		Version: config.CurrentVersion,
		Jobs:    config.Jobs{jobID: job},
	}

	uc, err := core.CurrentUserWithHome(home)
	if err != nil {
		t.Fatal(err)
	}
	plistPath := core.PlistPath(home, core.BuildLabel(uc.Username, jobID))
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return f, home
}

func serviceJob() config.Job {
	return config.Job{Type: "service", Command: []string{"/bin/echo", "hello"}, Restart: "always"}
}

func scheduleJob() config.Job {
	return config.Job{
		Type:     "schedule",
		Command:  []string{"/bin/echo", "hello"},
		Schedule: config.Schedule{Cron: "0 5 * * *"},
	}
}

// notLoaded scripts a runner where the label is absent until bootstrapped.
func notLoaded() *scriptRunner {
	r := &scriptRunner{}
	bootstrapped := false
	r.fn = func(call string, _ []string) error {
		switch {
		case strings.HasPrefix(call, "launchctl print"):
			if bootstrapped {
				return nil
			}
			return errors.New("not loaded")
		case strings.HasPrefix(call, "launchctl bootstrap"):
			bootstrapped = true
			return nil
		default:
			return nil
		}
	}
	return r
}

// loaded scripts a runner where the label is always present.
func loaded() *scriptRunner {
	return &scriptRunner{}
}

func joined(r *scriptRunner) string {
	return strings.Join(r.calls, "\n")
}

// TestStartToleratesBootstrapRace is the regression behind commit 93dc74c: like
// apply, start must retry a transient bootstrap failure rather than leaving the
// job down. Pre-fix the ops layer returned that first error.
func TestStartToleratesBootstrapRace(t *testing.T) {
	f, home := opsFixture(t, "worker_a", serviceJob())

	bootstraps := 0
	r := &scriptRunner{}
	r.fn = func(call string, _ []string) error {
		switch {
		case strings.HasPrefix(call, "launchctl print"):
			return errors.New("not loaded")
		case strings.HasPrefix(call, "launchctl bootstrap"):
			bootstraps++
			if bootstraps == 1 {
				return errors.New("5: Input/output error")
			}
			return nil
		default:
			return nil
		}
	}

	if err := Start(f, "worker_a", OpsOptions{HomeDir: home, Runner: r}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if bootstraps != 2 {
		t.Fatalf("expected Start to retry bootstrap (2 attempts), got %d", bootstraps)
	}
}

// TestRestartKicksLoadedJobWithoutReload is the core of `beagle restart`: a
// running service must be bounced with kickstart -k, not torn out of launchd and
// rebuilt. The bootout path was the old disable/enable workaround, which drops
// requests during launchd's asynchronous teardown.
func TestRestartKicksLoadedJobWithoutReload(t *testing.T) {
	f, home := opsFixture(t, "worker_a", serviceJob())
	r := loaded()

	if err := Restart(f, "worker_a", OpsOptions{HomeDir: home, Runner: r}); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}

	calls := joined(r)
	if !strings.Contains(calls, "launchctl kickstart -k gui/") {
		t.Fatalf("expected a forced kickstart, got:\n%s", calls)
	}
	if strings.Contains(calls, "launchctl bootout") || strings.Contains(calls, "launchctl bootstrap") {
		t.Fatalf("expected no reload of an already-loaded job, got:\n%s", calls)
	}
}

// TestRestartLoadsStoppedJob covers restarting something `beagle stop` unloaded.
// kickstart addresses launchd's registry, so without loading first it would fail
// outright and leave the job down.
func TestRestartLoadsStoppedJob(t *testing.T) {
	f, home := opsFixture(t, "worker_a", serviceJob())
	r := notLoaded()

	if err := Restart(f, "worker_a", OpsOptions{HomeDir: home, Runner: r}); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}

	calls := joined(r)
	if !strings.Contains(calls, "launchctl bootstrap") {
		t.Fatalf("expected the stopped job to be bootstrapped, got:\n%s", calls)
	}
	if !strings.Contains(calls, "launchctl kickstart -k gui/") {
		t.Fatalf("expected a forced kickstart after loading, got:\n%s", calls)
	}
}

// TestRestartWithoutInstalledAgent: a job in the config that was never applied
// has no agent, and the error must name the fix rather than surface a bare
// bootstrap failure.
func TestRestartWithoutInstalledAgent(t *testing.T) {
	home := t.TempDir()
	f := config.File{
		Version: config.CurrentVersion,
		Jobs:    config.Jobs{"worker_a": serviceJob()},
	}

	err := Restart(f, "worker_a", OpsOptions{HomeDir: home, Runner: notLoaded()})
	if err == nil {
		t.Fatal("expected Restart to fail when no agent is installed")
	}
	if !strings.Contains(err.Error(), "beagle apply") {
		t.Fatalf("error should point at `beagle apply`, got: %v", err)
	}
}

// TestStartKicksServiceUnforced: launchd starts a job on load only when
// KeepAlive is set, so `restart: never` services would load and sit dead. The
// unforced kick starts them without disturbing an already-running instance.
func TestStartKicksServiceUnforced(t *testing.T) {
	f, home := opsFixture(t, "worker_a", serviceJob())
	r := loaded()

	if err := Start(f, "worker_a", OpsOptions{HomeDir: home, Runner: r}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	calls := joined(r)
	if !strings.Contains(calls, "launchctl kickstart gui/") {
		t.Fatalf("expected an unforced kickstart for a service, got:\n%s", calls)
	}
	if strings.Contains(calls, "kickstart -k") {
		t.Fatalf("start must not kill a running instance, got:\n%s", calls)
	}
}

// TestStartDoesNotRunScheduleJob: starting a scheduled job means making it
// eligible to fire, not firing it. The supervisor owns its timing.
func TestStartDoesNotRunScheduleJob(t *testing.T) {
	f, home := opsFixture(t, "nightly", scheduleJob())
	r := loaded()

	if err := Start(f, "nightly", OpsOptions{HomeDir: home, Runner: r}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if calls := joined(r); strings.Contains(calls, "kickstart") {
		t.Fatalf("start must not run a schedule job, got:\n%s", calls)
	}
}

// TestStopBootsOut pins the contract behind `beagle stop`: unload the agent and
// nothing else. It is intentionally not durable - apply restores the job - which
// is why the CLI says so.
func TestStopBootsOut(t *testing.T) {
	f, home := opsFixture(t, "worker_a", serviceJob())
	r := loaded()

	if err := Stop(f, "worker_a", OpsOptions{HomeDir: home, Runner: r}); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	calls := joined(r)
	if !strings.Contains(calls, "launchctl bootout gui/") {
		t.Fatalf("expected a bootout, got:\n%s", calls)
	}
	if strings.Contains(calls, "kickstart") || strings.Contains(calls, "bootstrap") {
		t.Fatalf("stop must only unload, got:\n%s", calls)
	}
}

// TestRestartSupervisorAlwaysReloads: the failure this fixes is a supervisor
// launchd still lists as loaded but no longer invokes, so a kickstart alone would
// not re-arm the calendar interval. It must bootout and bootstrap regardless.
func TestRestartSupervisorAlwaysReloads(t *testing.T) {
	home := t.TempDir()
	uc, err := core.CurrentUserWithHome(home)
	if err != nil {
		t.Fatal(err)
	}
	plistPath := core.PlistPath(home, core.SupervisorLabel(uc.Username))
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plistPath, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &scriptRunner{}
	booted := false
	r.fn = func(call string, _ []string) error {
		switch {
		case strings.HasPrefix(call, "launchctl print"):
			if booted {
				return errors.New("not loaded")
			}
			return nil
		case strings.HasPrefix(call, "launchctl bootout"):
			booted = true
			return nil
		default:
			return nil
		}
	}

	if err := RestartSupervisor(OpsOptions{HomeDir: home, Runner: r}); err != nil {
		t.Fatalf("RestartSupervisor returned error: %v", err)
	}

	calls := joined(r)
	if !strings.Contains(calls, "launchctl bootout") || !strings.Contains(calls, "launchctl bootstrap") {
		t.Fatalf("expected a full reload of the supervisor, got:\n%s", calls)
	}
}

// TestRestartSupervisorWithoutAgent: with no supervisor agent nothing is
// scheduling anything, so the error must say that and name apply.
func TestRestartSupervisorWithoutAgent(t *testing.T) {
	err := RestartSupervisor(OpsOptions{HomeDir: t.TempDir(), Runner: loaded()})
	if err == nil {
		t.Fatal("expected RestartSupervisor to fail with no agent installed")
	}
	if !strings.Contains(err.Error(), "beagle apply") {
		t.Fatalf("error should point at `beagle apply`, got: %v", err)
	}
}
