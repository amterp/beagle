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

type fakeRunner struct {
	calls []string
	err   error
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.err
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

	// Second apply with a runner that reports the job as NOT loaded
	// (launchctl print returns error). The plist is identical, so normally
	// it would be "unchanged", but since it's not loaded, we re-bootstrap.
	callCount := 0
	unloadedRunner := &fakeRunner{err: nil}
	// Override to make "launchctl print" fail (job not loaded) but bootstrap succeed
	unloadedRunner.err = nil
	origRun := unloadedRunner.Run
	_ = origRun // suppress unused

	// Use a custom runner that fails on "print" but succeeds on "bootstrap"
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
	_ = callCount
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
