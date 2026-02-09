package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amterp/beagle/internal/launchd"
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
	jobKey, err := ra.NewString("job-key").SetOptional(true).SetUsage("Namespaced job key").Register(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	namespace, err := ra.NewString("namespace").SetOptional(true).SetUsage("Job namespace").Register(cmd)
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
	ns := strings.TrimSpace(*namespace)
	if ns == "" {
		ns = strings.TrimSpace(os.Getenv("BEAGLE_NAMESPACE"))
	}
	key := strings.TrimSpace(*jobKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("BEAGLE_JOB_KEY"))
	}
	if key == "" {
		key = job
		if ns != "" {
			key = ns + ":" + job
		}
	}
	runID, err := store.StartRun(context.Background(), runlog.RunStart{
		JobID:     job,
		JobKey:    key,
		Namespace: ns,
		Command:   strings.Join(*command, " "),
		PID:       os.Getpid(),
		Started:   started,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	now := time.Now().UTC()
	policy := breakerPolicyFromEnv()
	if open, until, err := store.IsBreakerOpen(context.Background(), key, now); err == nil && open {
		_ = store.FinishRun(context.Background(), runlog.RunFinish{
			ID:         runID,
			Finished:   now,
			ExitCode:   0,
			Status:     "skipped",
			FailureCls: "circuit_open",
			Notes:      "circuit breaker open until " + until.Format(time.RFC3339),
		})
		return 0
	}

	if shouldSkipForTimezone(*command) {
		_ = store.FinishRun(context.Background(), runlog.RunFinish{
			ID:         runID,
			Finished:   now,
			ExitCode:   0,
			Status:     "skipped",
			FailureCls: "tz_gate_skip",
			Notes:      "schedule does not match configured timezone gate",
		})
		return 0
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
		_ = store.RecordOutcome(context.Background(), key, time.Now().UTC(), true, policy)
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
	_ = store.RecordOutcome(context.Background(), key, time.Now().UTC(), finish.Status == "failed", policy)

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

func breakerPolicyFromEnv() runlog.BreakerPolicy {
	return runlog.BreakerPolicy{
		MaxFailures:     envInt("BEAGLE_BREAKER_MAX_FAILURES"),
		WindowSeconds:   envInt("BEAGLE_BREAKER_WINDOW_SECONDS"),
		CooldownSeconds: envInt("BEAGLE_BREAKER_COOLDOWN_SECONDS"),
	}
}

func envInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func shouldSkipForTimezone(command []string) bool {
	if os.Getenv("BEAGLE_JOB_TYPE") != "schedule" {
		return false
	}
	if os.Getenv("BEAGLE_SCHEDULE_STRICT_TZ") != "1" {
		return false
	}
	cron := strings.TrimSpace(os.Getenv("BEAGLE_SCHEDULE_CRON"))
	tz := strings.TrimSpace(os.Getenv("BEAGLE_SCHEDULE_TIMEZONE"))
	if cron == "" || tz == "" || len(command) == 0 {
		return false
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return false
	}
	cal, err := launchd.ParseCron(cron)
	if err != nil {
		return false
	}
	now := time.Now().In(loc)
	if cal.Minute != nil && *cal.Minute != now.Minute() {
		return true
	}
	if cal.Hour != nil && *cal.Hour != now.Hour() {
		return true
	}
	if cal.Day != nil && *cal.Day != now.Day() {
		return true
	}
	if cal.Month != nil && *cal.Month != int(now.Month()) {
		return true
	}
	weekday := int(now.Weekday())
	if cal.Weekday != nil && *cal.Weekday != weekday {
		return true
	}
	return false
}
