package launchd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/config"
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
		HomeDir:    dir,
		RunnerPath: runnerPath,
		Runner:     runner,
		Namespace:  "team_a",
	})
	if err != nil {
		t.Fatalf("unexpected apply error: %v", err)
	}
	if summary.Created != 1 {
		t.Fatalf("expected one created job, got %+v", summary)
	}
	if len(runner.calls) == 0 {
		t.Fatal("expected launchctl calls")
	}

	paths, _ := filepath.Glob(filepath.Join(dir, "Library", "LaunchAgents", "com.beagle.*.plist"))
	if len(paths) != 1 {
		t.Fatalf("expected one plist, got %d", len(paths))
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
		HomeDir:    dir,
		RunnerPath: runnerPath,
		Runner:     runner,
		Namespace:  "team_a",
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
		HomeDir:    dir,
		RunnerPath: runnerPath,
		Runner:     runner,
		Namespace:  "team_a",
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
		HomeDir:    dir,
		RunnerPath: runnerPath,
		Runner:     printFailRunner,
		Namespace:  "team_a",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = callCount
	if summary.Updated != 1 {
		t.Fatalf("expected 1 updated (re-bootstrapped), got %+v", summary)
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

func TestApplyRemovesOnlyNamespaceManagedPlists(t *testing.T) {
	dir := t.TempDir()
	runnerPath := filepath.Join(dir, "beagle-run")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	launchDir := filepath.Join(dir, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(launchDir, "com.beagle.tester.team_b.worker_b.plist")
	if err := os.WriteFile(other, []byte("other"), 0o644); err != nil {
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
		HomeDir:    dir,
		RunnerPath: runnerPath,
		Runner:     runner,
		Namespace:  "team_a",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("expected other namespace plist to remain: %v", err)
	}
}
