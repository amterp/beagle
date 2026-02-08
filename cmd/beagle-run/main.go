package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/amterp/beagle/internal/runlog"
	"github.com/amterp/ra"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cmd := ra.NewCmd("beagle-run").SetDescription("Internal Beagle runner")
	jobID, err := ra.NewString("job").SetUsage("Job id").Register(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	command, err := ra.NewStringSlice("command").SetVariadic(true).SetUsage("Command and args").Register(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := cmd.ParseOrError(args); err != nil {
		if err == ra.HelpInvokedErr {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if len(*command) == 0 {
		fmt.Fprintln(os.Stderr, "missing command arguments")
		return 2
	}

	dbPath, err := runlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	store, err := runlog.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer store.Close()

	started := time.Now().UTC()
	job := strings.TrimSpace(*jobID)
	runID, err := store.StartRun(context.Background(), runlog.RunStart{
		JobID:   job,
		Command: strings.Join(*command, " "),
		PID:     os.Getpid(),
		Started: started,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	realCmd := exec.Command((*command)[0], (*command)[1:]...)
	realCmd.Stdout = os.Stdout
	realCmd.Stderr = os.Stderr
	realCmd.Stdin = os.Stdin

	if err := realCmd.Start(); err != nil {
		_ = store.FinishRun(context.Background(), runlog.RunFinish{
			ID:         runID,
			Finished:   time.Now().UTC(),
			ExitCode:   127,
			Status:     "failed",
			FailureCls: "exec_error",
			Notes:      err.Error(),
		})
		fmt.Fprintln(os.Stderr, err)
		return 127
	}

	forwardSignals(realCmd)

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
		fmt.Fprintln(os.Stderr, err)
	}

	return exitCode
}

func forwardSignals(child *exec.Cmd) {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for s := range sigs {
			if child.Process != nil {
				_ = child.Process.Signal(s)
			}
		}
	}()
}
