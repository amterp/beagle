package launchd

import (
	"errors"
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
		RunnerPath: "/usr/local/bin/beagle-run",
		Runner:     runner,
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
		RunnerPath: "/usr/local/bin/beagle-run",
		Runner:     runner,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(summary.Errors) == 0 {
		t.Fatalf("expected summary errors, got %+v", summary)
	}
}
