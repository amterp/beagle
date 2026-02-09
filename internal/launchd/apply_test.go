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
