package launchd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
)

type OpsOptions struct {
	HomeDir string
	Runner  CommandRunner
}

// Restart bounces a job: it kills any in-flight instance and starts a fresh one.
// For a service that is the "pick up the binary I just rebuilt" bounce; for a
// schedule job it is a rerun outside the schedule. Those are the same launchd
// operation, which is why `beagle restart` and `beagle run-now` share this - the
// two commands differ only in the intent a user brings, not in what happens.
func Restart(f config.File, jobID string, opts OpsOptions) error {
	uc, label, plistPath, _, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	if err := ensureLoaded(runner, uc.UID, label, plistPath); err != nil {
		return err
	}
	return Kick(uc.UID, label, true, runner)
}

// Start brings a job up, leaving it alone if it is already up. Loading is enough
// for a schedule job - the supervisor owns its timing - but a service also needs
// its process actually running, and launchd starts one on load only when
// KeepAlive is set, which `restart: never` services do not have. The unforced
// kick covers that case and is a no-op on a service that is already running.
func Start(f config.File, jobID string, opts OpsOptions) error {
	uc, label, plistPath, job, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	if err := ensureLoaded(runner, uc.UID, label, plistPath); err != nil {
		return err
	}
	if strings.EqualFold(job.Type, "service") {
		return Kick(uc.UID, label, false, runner)
	}
	return nil
}

// Stop unloads a job's launchd agent, stopping a running service and making a
// schedule job ineligible to fire. It is deliberately not persistent: jobs.yaml
// is the single source of truth, so the next `beagle apply` - or a reboot -
// brings the job back. `enabled: false` in the config is the durable off switch.
func Stop(f config.File, jobID string, opts OpsOptions) error {
	uc, label, _, _, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	return runner.Run("launchctl", "bootout", "gui/"+uc.UID+"/"+label)
}

// RestartSupervisor re-arms the scheduler agent. Unlike a job restart this always
// tears the agent down and bootstraps it again, because the failure it exists to
// fix is a supervisor launchd still reports as loaded but has stopped invoking
// (`beagle doctor` shows "supervisor ticking ✗"). Only a fresh bootstrap
// re-registers the every-minute calendar interval, and `beagle apply` cannot do
// it: apply sees a loaded agent whose plist content matches and calls it
// unchanged. The plist sets RunAtLoad, so bootstrapping also runs a tick
// immediately and the heartbeat updates without waiting out the minute.
func RestartSupervisor(opts OpsOptions) error {
	uc, err := core.CurrentUserWithHome(opts.HomeDir)
	if err != nil {
		return err
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	label := core.SupervisorLabel(uc.Username)
	plistPath := core.PlistPath(uc.HomeDir, label)
	if _, err := os.Stat(plistPath); err != nil {
		return fmt.Errorf("supervisor agent is not installed (no agent at %s), so nothing is scheduling jobs - run `beagle apply` to install it", plistPath)
	}
	return reload(runner, uc.UID, label, plistPath)
}

// ensureLoaded bootstraps a job's launchd agent if it is not already loaded.
// kickstart addresses a label in launchd's registry and fails outright on an
// unloaded one - exactly the state `beagle stop` leaves behind - so loading first
// is what lets restart and start work on a stopped job instead of erroring.
func ensureLoaded(runner CommandRunner, uid, label, plistPath string) error {
	if isJobLoaded(runner, uid, label) {
		return nil
	}
	if _, err := os.Stat(plistPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("job is configured but not installed in launchd (no agent at %s), "+
				"so there is nothing to run - run `beagle apply` first", plistPath)
		}
		return fmt.Errorf("read agent %s: %w", plistPath, err)
	}
	if err := reload(runner, uid, label, plistPath); err != nil {
		return fmt.Errorf("load %s: %w", label, err)
	}
	return nil
}

// Kick triggers an immediate run of a loaded job via launchctl kickstart.
//
// force adds -k, which kills any in-flight instance before starting a fresh
// one - what a user means by `run-now`. The supervisor passes force=false so a
// still-running job is left alone rather than being killed to start a duplicate
// (its own schedule_state dedup, not kickstart, is the real double-run guard).
func Kick(uid, label string, force bool, runner CommandRunner) error {
	if runner == nil {
		runner = ExecRunner{}
	}
	args := []string{"kickstart"}
	if force {
		args = append(args, "-k")
	}
	args = append(args, "gui/"+uid+"/"+label)
	return runner.Run("launchctl", args...)
}

func ReadLogs(f config.File, jobID string, stderr bool, tailLines int, opts OpsOptions) (string, error) {
	_, _, _, _, _, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return "", err
	}

	uc, err := core.CurrentUserWithHome(opts.HomeDir)
	if err != nil {
		return "", err
	}
	stream := "stdout"
	if stderr {
		stream = "stderr"
	}
	path := core.LogFilePath(uc.HomeDir, jobID, stream)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	text := string(b)
	if tailLines > 0 {
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		if len(lines) > tailLines {
			lines = lines[len(lines)-tailLines:]
		}
		text = strings.Join(lines, "\n")
	}
	return text, nil
}

func jobRuntimeContext(f config.File, jobID string, opts OpsOptions) (uc core.UserContext, label string, plistPath string, job config.Job, runner CommandRunner, err error) {
	uc, err = core.CurrentUserWithHome(opts.HomeDir)
	if err != nil {
		return core.UserContext{}, "", "", config.Job{}, nil, err
	}
	job, ok := f.Jobs[jobID]
	if !ok {
		return core.UserContext{}, "", "", config.Job{}, nil, fmt.Errorf("job not found: %s", jobID)
	}
	label = core.BuildLabel(uc.Username, jobID)
	plistPath = core.PlistPath(uc.HomeDir, label)
	runner = opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return uc, label, plistPath, job, runner, nil
}
