package runner

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/amterp/beagle/internal/runlog"
)

func TestRunSuccessfulCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := runlog.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var stdout, stderr bytes.Buffer
	exitCode := Run(RunConfig{
		JobID:     "test-job",
		Namespace: "test",
		Command:   []string{"/bin/echo", "hello"},
		Stdout:    &stdout,
		Stderr:    &stderr,
	}, store, &stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", exitCode, stderr.String())
	}

	// Verify the run was recorded
	failures, err := store.RecentFailures(context.Background(), "test", "test-job", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("expected no failures, got %d", len(failures))
	}
}

func TestRunFailedCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := runlog.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var stdout, stderr bytes.Buffer
	exitCode := Run(RunConfig{
		JobID:     "test-job",
		Namespace: "test",
		Command:   []string{"/bin/sh", "-c", "exit 42"},
		Stdout:    &stdout,
		Stderr:    &stderr,
	}, store, &stderr)

	if exitCode != 42 {
		t.Fatalf("expected exit 42, got %d", exitCode)
	}
}

func TestRunExecError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := runlog.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var stdout, stderr bytes.Buffer
	exitCode := Run(RunConfig{
		JobID:     "test-job",
		Namespace: "test",
		Command:   []string{"/nonexistent/binary"},
		Stdout:    &stdout,
		Stderr:    &stderr,
	}, store, &stderr)

	if exitCode != 127 {
		t.Fatalf("expected exit 127, got %d", exitCode)
	}
}
