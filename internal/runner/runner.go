package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/amterp/beagle/internal/core"
	"github.com/amterp/beagle/internal/runlog"
)

// RunConfig holds the parameters for a beagle-run execution.
type RunConfig struct {
	JobID     string
	JobKey    string
	Namespace string
	Command   []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

// Run executes the job command, recording telemetry to the store.
// Returns the process exit code.
func Run(cfg RunConfig, store *runlog.Store, stderr io.Writer) int {
	started := time.Now().UTC()
	job := strings.TrimSpace(cfg.JobID)
	ns := strings.TrimSpace(cfg.Namespace)
	key := strings.TrimSpace(cfg.JobKey)
	if key == "" {
		key = core.BuildJobKey(ns, job)
	}

	runID, err := store.StartRun(context.Background(), runlog.RunStart{
		JobID:     job,
		JobKey:    key,
		Namespace: ns,
		Command:   strings.Join(cfg.Command, " "),
		PID:       os.Getpid(),
		Started:   started,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	now := time.Now().UTC()
	policy := BreakerPolicyFromEnv(stderr)
	if open, until, err := store.IsBreakerOpen(context.Background(), key, now); err == nil && open {
		if err := store.FinishRun(context.Background(), runlog.RunFinish{
			ID:         runID,
			Finished:   now,
			ExitCode:   0,
			Status:     "skipped",
			FailureCls: "circuit_open",
			Notes:      "circuit breaker open until " + until.Format(time.RFC3339),
		}); err != nil {
			fmt.Fprintf(stderr, "beagle-run: warning: %v\n", err)
		}
		return 0
	}

	if ShouldSkipForTimezone(stderr) {
		if err := store.FinishRun(context.Background(), runlog.RunFinish{
			ID:         runID,
			Finished:   now,
			ExitCode:   0,
			Status:     "skipped",
			FailureCls: "tz_gate_skip",
			Notes:      "schedule does not match configured timezone gate",
		}); err != nil {
			fmt.Fprintf(stderr, "beagle-run: warning: %v\n", err)
		}
		return 0
	}

	realCmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	realCmd.Stdout = cfg.Stdout
	realCmd.Stderr = cfg.Stderr
	realCmd.Stdin = cfg.Stdin

	if err := realCmd.Start(); err != nil {
		if finErr := store.FinishRun(context.Background(), runlog.RunFinish{
			ID:         runID,
			Finished:   time.Now().UTC(),
			ExitCode:   127,
			Status:     "failed",
			FailureCls: "exec_error",
			Notes:      err.Error(),
		}); finErr != nil {
			fmt.Fprintf(stderr, "beagle-run: warning: %v\n", finErr)
		}
		fmt.Fprintln(stderr, err)
		// Don't trip circuit breaker for exec errors - these are config
		// issues (bad command path), not transient runtime failures. Tripping
		// the breaker would hide the root cause from the user on subsequent runs.
		return 127
	}

	stopSignals := ForwardSignals(realCmd)
	defer stopSignals()
	waitErr := realCmd.Wait()

	finish := runlog.RunFinish{
		ID:       runID,
		Finished: time.Now().UTC(),
		Status:   "succeeded",
	}
	exitCode := 0

	if waitErr != nil {
		finish.Status = "failed"
		finish.FailureCls = "exit_nonzero"
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					exitCode = 128 + int(status.Signal())
					finish.TermSignal = status.Signal().String()
					finish.FailureCls = "signal"
				} else {
					exitCode = status.ExitStatus()
				}
			} else {
				exitCode = exitErr.ExitCode()
			}
		} else {
			exitCode = 1
			finish.Notes = waitErr.Error()
		}
	}
	finish.ExitCode = exitCode
	if err := store.FinishRun(context.Background(), finish); err != nil {
		fmt.Fprintf(stderr, "beagle-run: warning: %v\n", err)
	}
	if err := store.RecordOutcome(context.Background(), key, time.Now().UTC(), finish.Status == "failed", policy); err != nil {
		fmt.Fprintf(stderr, "beagle-run: warning: %v\n", err)
	}

	return exitCode
}
