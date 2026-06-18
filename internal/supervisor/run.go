package supervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
	"github.com/amterp/beagle/internal/launchd"
	"github.com/amterp/beagle/internal/runlog"
)

// Supervise runs a single supervisor tick end to end: take the single-flight
// lock, load config, evaluate schedules, and stamp a heartbeat. It is what the
// launchd supervisor agent invokes (`beagle supervise`) on boot, on wake, and
// every minute. Returns a process exit code.
func Supervise(stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	lockPath := filepath.Join(core.BeagleDir(home), "supervisor.lock")
	if err := os.MkdirAll(core.BeagleDir(home), 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	lock, ok, err := AcquireLock(lockPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !ok {
		// Another tick is already running; nothing to do.
		return 0
	}
	defer lock.Release()

	configPath := core.ConfigPath(home)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// No jobs configured yet - a no-op tick, not an error.
		return 0
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	store, err := runlog.Open(core.RunlogDBPath(home))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer store.Close()

	uc, err := core.CurrentUser()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	now := time.Now()
	res, tickErr := Tick(cfg, Deps{
		Store:    store,
		Runner:   launchd.ExecRunner{},
		Username: uc.Username,
		UID:      uc.UID,
		Now:      now,
		Log:      stderr,
	})

	// Heartbeat regardless of per-job errors, so doctor can distinguish "the
	// scheduler is dead" from "a job failed".
	if err := store.SetMeta(context.Background(), TickHeartbeatKey, "ok", now); err != nil {
		fmt.Fprintf(stderr, "supervisor: heartbeat: %v\n", err)
	}

	if tickErr != nil {
		fmt.Fprintln(stderr, tickErr)
		return 1
	}
	for _, e := range res.Errors {
		fmt.Fprintln(stderr, "supervisor:", e)
	}
	if len(res.Errors) > 0 {
		return 1
	}
	return 0
}
