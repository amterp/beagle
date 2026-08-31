package launchd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
)

func TestMain(m *testing.M) {
	// Never actually sleep during reload's bootout-settle / bootstrap-retry
	// loops, so the suite stays fast regardless of the configured intervals.
	reloadSleep = func(time.Duration) {}
	os.Exit(m.Run())
}

type fakeRunner struct {
	calls []string
	err   error
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.err
}

// scriptRunner is a programmable CommandRunner: fn decides each call's result,
// letting a test model stateful launchd behavior - e.g. "loaded until bootout,
// then gone" or "bootstrap fails once then succeeds". calls records every
// invocation as "name arg arg ...".
type scriptRunner struct {
	calls []string
	fn    func(call string, calls []string) error
}

func (s *scriptRunner) Run(name string, args ...string) error {
	call := name + " " + strings.Join(args, " ")
	s.calls = append(s.calls, call)
	if s.fn == nil {
		return nil
	}
	return s.fn(call, s.calls)
}

func TestApplyWritesPlistAndBootstraps(t *testing.T) {
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "beagle-run")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo", "hello"},
				Restart: "always",
			},
		},
	}

	runner := &fakeRunner{}
	summary, err := Apply(f, ApplyOptions{
		HomeDir:        dir,
		RunnerPath:     runnerPath,
		SupervisorPath: runnerPath,
		Runner:         runner,
	})
	if err != nil {
		t.Fatalf("unexpected apply error: %v", err)
	}
	// The job plus the always-present supervisor agent.
	if summary.Created != 2 {
		t.Fatalf("expected job + supervisor created, got %+v", summary)
	}
	if len(runner.calls) == 0 {
		t.Fatal("expected launchctl calls")
	}

	paths, _ := filepath.Glob(filepath.Join(dir, "Library", "LaunchAgents", "com.beagle.*.plist"))
	if len(paths) != 2 {
		t.Fatalf("expected job + supervisor plists, got %d", len(paths))
	}
}

func TestApplyReportsRunnerErrors(t *testing.T) {
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "beagle-run")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo", "hello"},
				Restart: "always",
			},
		},
	}

	runner := &fakeRunner{err: errors.New("launchctl failed")}
	summary, err := Apply(f, ApplyOptions{
		HomeDir:        dir,
		RunnerPath:     runnerPath,
		SupervisorPath: runnerPath,
		Runner:         runner,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(summary.Errors) == 0 {
		t.Fatalf("expected summary errors, got %+v", summary)
	}
}

func TestApplyRebootstrapsUnloadedJob(t *testing.T) {
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "beagle-run")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo", "hello"},
				Restart: "always",
			},
		},
	}

	// First apply to create the plist
	runner := &fakeRunner{}
	_, err := Apply(f, ApplyOptions{
		HomeDir:        dir,
		RunnerPath:     runnerPath,
		SupervisorPath: runnerPath,
		Runner:         runner,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second apply with a runner that reports the job as NOT loaded (launchctl
	// print fails) but lets bootstrap succeed. The plist is identical, so the
	// content-match path runs; since the job isn't loaded, it re-bootstraps.
	printFailRunner := &selectiveRunner{
		failOn: "launchctl print",
	}

	summary, err := Apply(f, ApplyOptions{
		HomeDir:        dir,
		RunnerPath:     runnerPath,
		SupervisorPath: runnerPath,
		Runner:         printFailRunner,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both the job and the supervisor are present-but-not-loaded, so both
	// re-bootstrap.
	if summary.Updated != 2 {
		t.Fatalf("expected 2 updated (job + supervisor re-bootstrapped), got %+v", summary)
	}
}

type selectiveRunner struct {
	calls  []string
	failOn string
}

func (s *selectiveRunner) Run(name string, args ...string) error {
	call := name + " " + strings.Join(args, " ")
	s.calls = append(s.calls, call)
	if s.failOn != "" && strings.HasPrefix(call, s.failOn) {
		return errors.New("not loaded")
	}
	return nil
}

// TestApplyGarbageCollectsStrayManagedPlists verifies the global reconciliation:
// any plist beagle manages for this user (matching com.beagle.<user>.*) that is
// no longer backed by config - e.g. a pre-refactor orphan - is booted out and
// removed. This is the behavior that the old per-namespace glob could not reach.
func TestApplyGarbageCollectsStrayManagedPlists(t *testing.T) {
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "beagle-run")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	uc, err := core.CurrentUserWithHome(dir)
	if err != nil {
		t.Fatal(err)
	}

	launchDir := filepath.Join(dir, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanLabel := core.BuildLabel(uc.Username, "old_orphan")
	orphan := core.PlistPath(dir, orphanLabel)
	if err := os.WriteFile(orphan, []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A plist for a different user must NOT be touched.
	foreign := filepath.Join(launchDir, "com.beagle.someoneelse.worker_b.plist")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo", "hello"},
				Restart: "always",
			},
		},
	}
	runner := &fakeRunner{}
	if _, err := Apply(f, ApplyOptions{
		HomeDir:        dir,
		RunnerPath:     runnerPath,
		SupervisorPath: runnerPath,
		Runner:         runner,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected stray managed plist to be reconciled away, stat err = %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("expected another user's plist to remain untouched: %v", err)
	}

	var sawBootout bool
	for _, c := range runner.calls {
		if strings.Contains(c, "bootout") && strings.Contains(c, orphanLabel) {
			sawBootout = true
		}
	}
	if !sawBootout {
		t.Fatalf("expected a bootout for the orphan label %q, calls: %v", orphanLabel, runner.calls)
	}
}

// firstIndexWithPrefix returns the index of the first recorded call starting
// with prefix, or -1.
func firstIndexWithPrefix(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

// TestReloadWaitsForBootoutThenBootstraps is the core of the fix: a loaded job
// must be booted out and confirmed gone before we bootstrap, so the bootstrap
// never races launchd's asynchronous teardown.
func TestReloadWaitsForBootoutThenBootstraps(t *testing.T) {
	bootedOut := false
	pollsAfterBootout := 0
	r := &scriptRunner{}
	r.fn = func(call string, _ []string) error {
		switch {
		case strings.HasPrefix(call, "launchctl bootout"):
			bootedOut = true
			return nil
		case strings.HasPrefix(call, "launchctl print"):
			if !bootedOut {
				return nil // loaded
			}
			// Teardown takes a couple of polls to settle, then the label is gone.
			pollsAfterBootout++
			if pollsAfterBootout < 3 {
				return nil // still loaded
			}
			return errors.New("not loaded")
		default: // bootstrap
			return nil
		}
	}

	if err := reload(r, "501", "com.beagle.test.job", "/tmp/job.plist"); err != nil {
		t.Fatalf("reload returned error: %v", err)
	}

	bootoutIdx := firstIndexWithPrefix(r.calls, "launchctl bootout")
	bootstrapIdx := firstIndexWithPrefix(r.calls, "launchctl bootstrap")
	if bootoutIdx == -1 {
		t.Fatalf("expected a bootout, calls: %v", r.calls)
	}
	if bootstrapIdx == -1 {
		t.Fatalf("expected a bootstrap, calls: %v", r.calls)
	}
	if bootstrapIdx < bootoutIdx {
		t.Fatalf("bootstrap ran before bootout, calls: %v", r.calls)
	}
	// There must be polling prints between bootout and bootstrap - that's the
	// wait-until-gone that closes the race.
	if bootstrapIdx-bootoutIdx < 2 {
		t.Fatalf("expected polling prints between bootout and bootstrap, calls: %v", r.calls)
	}
}

// TestReloadRetriesBootstrapOnTransientError covers a residual EIO that clears
// on retry once the job is not loaded.
func TestReloadRetriesBootstrapOnTransientError(t *testing.T) {
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

	if err := reload(r, "501", "com.beagle.test.job", "/tmp/job.plist"); err != nil {
		t.Fatalf("reload returned error: %v", err)
	}
	if bootstraps != 2 {
		t.Fatalf("expected 2 bootstrap attempts, got %d", bootstraps)
	}
	if i := firstIndexWithPrefix(r.calls, "launchctl bootout"); i != -1 {
		t.Fatalf("did not expect a bootout for an unloaded job, calls: %v", r.calls)
	}
}

// TestReloadReturnsNilWhenBootstrapFailsButJobLoaded covers launchd reporting an
// error from bootstrap yet completing the load anyway - we trust launchd's own
// view and stop, rather than retrying into a duplicate.
func TestReloadReturnsNilWhenBootstrapFailsButJobLoaded(t *testing.T) {
	bootstrapped := false
	bootstraps := 0
	r := &scriptRunner{}
	r.fn = func(call string, _ []string) error {
		switch {
		case strings.HasPrefix(call, "launchctl print"):
			if bootstrapped {
				return nil // launchd actually completed the load
			}
			return errors.New("not loaded")
		case strings.HasPrefix(call, "launchctl bootstrap"):
			bootstrapped = true
			bootstraps++
			return errors.New("5: Input/output error")
		default:
			return nil
		}
	}

	if err := reload(r, "501", "com.beagle.test.job", "/tmp/job.plist"); err != nil {
		t.Fatalf("reload returned error: %v", err)
	}
	if bootstraps != 1 {
		t.Fatalf("expected exactly 1 bootstrap (fails-but-loaded short-circuits), got %d", bootstraps)
	}
}

// TestReloadReturnsErrorWhenBootstrapNeverSucceeds verifies the retry is bounded
// and a genuinely failing bootstrap is surfaced rather than hung on.
func TestReloadReturnsErrorWhenBootstrapNeverSucceeds(t *testing.T) {
	bootstraps := 0
	r := &scriptRunner{}
	r.fn = func(call string, _ []string) error {
		switch {
		case strings.HasPrefix(call, "launchctl print"):
			return errors.New("not loaded")
		case strings.HasPrefix(call, "launchctl bootstrap"):
			bootstraps++
			return errors.New("5: Input/output error")
		default:
			return nil
		}
	}

	if err := reload(r, "501", "com.beagle.test.job", "/tmp/job.plist"); err == nil {
		t.Fatal("expected reload to return an error when bootstrap never succeeds")
	}
	if bootstraps != bootstrapAttempts {
		t.Fatalf("expected %d bootstrap attempts, got %d", bootstrapAttempts, bootstraps)
	}
}

// TestReloadSkipsBootoutWhenNotLoaded confirms the bootout is gated on load
// state - a not-loaded label (new job / manually unloaded) bootstraps directly.
func TestReloadSkipsBootoutWhenNotLoaded(t *testing.T) {
	r := &scriptRunner{}
	r.fn = func(call string, _ []string) error {
		if strings.HasPrefix(call, "launchctl print") {
			return errors.New("not loaded")
		}
		return nil // bootstrap succeeds
	}

	if err := reload(r, "501", "com.beagle.test.job", "/tmp/job.plist"); err != nil {
		t.Fatalf("reload returned error: %v", err)
	}
	if i := firstIndexWithPrefix(r.calls, "launchctl bootout"); i != -1 {
		t.Fatalf("did not expect a bootout when the job is not loaded, calls: %v", r.calls)
	}
}

// TestResolveSupervisorPathKeepsSymlink pins the behavior that lets the
// supervisor plist survive a package upgrade. Homebrew points a stable symlink
// at a versioned directory; baking the resolved target into the plist means the
// next upgrade deletes the path launchd was told to exec, and the scheduler
// dies silently. Every other test injects SupervisorPath explicitly, so this is
// the only cover for the self-path branch.
func TestResolveSupervisorPathKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	versionedDir := filepath.Join(dir, "Cellar", "beagle", "0.6.0", "bin")
	if err := os.MkdirAll(versionedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	versioned := filepath.Join(versionedDir, "beagle")
	if err := os.WriteFile(versioned, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stableDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(stableDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(stableDir, "beagle")
	if err := os.Symlink(versioned, stable); err != nil {
		t.Fatal(err)
	}
	selfIsSymlink := func() (string, error) { return stable, nil }

	got, err := resolveSupervisorPath("", selfIsSymlink)
	if err != nil {
		t.Fatal(err)
	}
	if got != stable {
		t.Errorf("resolveSupervisorPath = %q, want the stable symlink %q - a versioned path stops working on upgrade", got, stable)
	}

	if got, err := resolveSupervisorPath(versioned, selfIsSymlink); err != nil || got != versioned {
		t.Errorf("explicit override = %q (err %v), want %q", got, err, versioned)
	}

	// A relative self path must never reach the plist - launchd cannot spawn it -
	// so resolution falls through to PATH instead.
	if got, _ := resolveSupervisorPath("", func() (string, error) { return "beagle", nil }); got == "beagle" {
		t.Error("resolveSupervisorPath returned a relative path; launchd cannot spawn it")
	}
}
