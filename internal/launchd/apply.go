package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
)

type CommandRunner interface {
	Run(name string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

type ApplyOptions struct {
	HomeDir        string
	RunnerPath     string
	SupervisorPath string
	Runner         CommandRunner
}

type Summary struct {
	Created   int
	Updated   int
	Removed   int
	Unchanged int
	Errors    []string
}

func Apply(f config.File, opts ApplyOptions) (Summary, error) {
	jobs, err := config.Resolve(f)
	if err != nil {
		return Summary{}, err
	}

	uc, err := core.CurrentUserWithHome(opts.HomeDir)
	if err != nil {
		return Summary{}, err
	}

	launchDir := core.LaunchAgentsDir(uc.HomeDir)
	logsRoot := core.LogsDir(uc.HomeDir)

	if err := os.MkdirAll(launchDir, 0o755); err != nil {
		return Summary{}, fmt.Errorf("create launch agents dir: %w", err)
	}
	if err := os.MkdirAll(logsRoot, 0o755); err != nil {
		return Summary{}, fmt.Errorf("create logs dir: %w", err)
	}

	runnerPath := opts.RunnerPath
	if runnerPath == "" {
		runnerPath = strings.TrimSpace(os.Getenv("BEAGLE_RUNNER_PATH"))
	}
	runnerPath, err = resolveRunnerPath(runnerPath)
	if err != nil {
		return Summary{}, err
	}

	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	desired := map[string]string{}
	desiredLabels := map[string]string{}
	for _, job := range jobs {
		label := core.BuildLabel(uc.Username, job.ID)
		plistPath := core.PlistPath(uc.HomeDir, label)
		stdoutPath := core.LogFilePath(uc.HomeDir, job.ID, "stdout")
		stderrPath := core.LogFilePath(uc.HomeDir, job.ID, "stderr")
		if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil {
			return Summary{}, fmt.Errorf("create logs for %s: %w", job.ID, err)
		}

		spec, err := BuildSpec(label, job, runnerPath, stdoutPath, stderrPath)
		if err != nil {
			return Summary{}, err
		}
		content, err := RenderPlist(spec)
		if err != nil {
			return Summary{}, err
		}
		desired[plistPath] = content
		desiredLabels[plistPath] = label
	}

	// The supervisor agent is always desired: it's the one job launchd keeps
	// ticking, and the scheduler that drives every schedule job. Adding it to
	// `desired` both bootstraps it and protects it from the global-glob GC below.
	supervisorPath, err := resolveSupervisorPath(opts.SupervisorPath)
	if err != nil {
		return Summary{}, err
	}
	{
		supLabel := core.SupervisorLabel(uc.Username)
		supPlistPath := core.PlistPath(uc.HomeDir, supLabel)
		supStdout := core.LogFilePath(uc.HomeDir, core.SupervisorName, "stdout")
		supStderr := core.LogFilePath(uc.HomeDir, core.SupervisorName, "stderr")
		if err := os.MkdirAll(filepath.Dir(supStdout), 0o755); err != nil {
			return Summary{}, fmt.Errorf("create supervisor logs: %w", err)
		}
		supContent, err := RenderPlist(BuildSupervisorSpec(supLabel, supervisorPath, supStdout, supStderr))
		if err != nil {
			return Summary{}, err
		}
		desired[supPlistPath] = supContent
		desiredLabels[supPlistPath] = supLabel
	}

	managedPattern := core.ManagedGlob(uc.HomeDir, uc.Username)
	existing, err := filepath.Glob(managedPattern)
	if err != nil {
		return Summary{}, fmt.Errorf("glob managed plists: %w", err)
	}

	managed := map[string]struct{}{}
	for _, p := range existing {
		managed[p] = struct{}{}
	}

	summary := Summary{}

	for path := range managed {
		if _, ok := desired[path]; ok {
			continue
		}
		label := strings.TrimSuffix(filepath.Base(path), ".plist")
		_ = runner.Run("launchctl", "bootout", "gui/"+uc.UID+"/"+label)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			summary.Errors = append(summary.Errors, fmt.Sprintf("remove %s: %v", path, err))
			continue
		}
		summary.Removed++
	}

	paths := make([]string, 0, len(desired))
	for path := range desired {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		content := desired[path]
		label := desiredLabels[path]

		current, err := os.ReadFile(path)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			summary.Errors = append(summary.Errors, fmt.Sprintf("read %s: %v", path, err))
			continue
		}

		if exists && string(current) == content {
			// Plist content matches, but the job might not actually be loaded
			// (e.g. a previous bootstrap failed or was manually unloaded).
			// Check and re-bootstrap if needed.
			if isJobLoaded(runner, uc.UID, label) {
				summary.Unchanged++
				continue
			}
			if err := reload(runner, uc.UID, label, path); err != nil {
				summary.Errors = append(summary.Errors, fmt.Sprintf("re-bootstrap %s: %v", label, err))
			} else {
				summary.Updated++
			}
			continue
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("write %s: %v", path, err))
			continue
		}

		if err := reload(runner, uc.UID, label, path); err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("bootstrap %s: %v", label, err))
		} else if exists {
			summary.Updated++
		} else {
			summary.Created++
		}
	}

	if len(summary.Errors) > 0 {
		return summary, fmt.Errorf("apply completed with %d error(s)", len(summary.Errors))
	}
	return summary, nil
}

// isJobLoaded checks whether a launchd job is currently loaded by running
// "launchctl print". We use the CommandRunner interface so tests can stub this.
func isJobLoaded(runner CommandRunner, uid string, label string) bool {
	// "launchctl print" exits 0 if the service is loaded, non-zero otherwise.
	// Using the runner so tests with fakeRunner can control this behavior.
	err := runner.Run("launchctl", "print", "gui/"+uid+"/"+label)
	return err == nil
}

const (
	reloadPollInterval = 50 * time.Millisecond
	reloadPollAttempts = 120 // ~6s ceiling; returns the instant the label is gone
	bootstrapAttempts  = 3
)

// reloadSleep is a test seam; production always uses time.Sleep.
var reloadSleep = time.Sleep

// reload tears down a launchd label (if loaded) and bootstraps it from
// plistPath, tolerating launchd's asynchronous bootout. "launchctl bootout"
// returns before launchd has finished reaping the job, so a bootstrap issued
// right after races the in-flight teardown and fails with EIO
// ("5: Input/output error"), stranding a previously-running service. We make
// teardown synchronous: bootout, poll until the label is gone, then bootstrap,
// re-checking isJobLoaded after each attempt. Returns nil only once the label
// is confirmed loaded. The caller must write plistPath before calling reload.
func reload(runner CommandRunner, uid, label, plistPath string) error {
	target := "gui/" + uid + "/" + label
	domain := "gui/" + uid

	if isJobLoaded(runner, uid, label) {
		_ = runner.Run("launchctl", "bootout", target)
		for i := 0; i < reloadPollAttempts && isJobLoaded(runner, uid, label); i++ {
			reloadSleep(reloadPollInterval)
		}
	}

	var err error
	for i := 0; i < bootstrapAttempts; i++ {
		if err = runner.Run("launchctl", "bootstrap", domain, plistPath); err == nil {
			return nil
		}
		// bootstrap can report EIO yet still complete the load; trust launchd's
		// own view over the command's exit status.
		if isJobLoaded(runner, uid, label) {
			return nil
		}
		reloadSleep(reloadPollInterval)
	}
	return err
}

func resolveRunnerPath(path string) (string, error) {
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("runner path must be absolute: %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("runner path invalid: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("runner path is a directory: %s", path)
		}
		return path, nil
	}

	found, err := exec.LookPath("beagle-run")
	if err != nil {
		return "", fmt.Errorf("beagle-run not found; set BEAGLE_RUNNER_PATH to an absolute beagle-run binary")
	}
	abs, err := filepath.Abs(found)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// resolveSupervisorPath resolves the `beagle` binary the supervisor plist
// invokes. An explicit override wins; otherwise prefer the binary running this
// apply (os.Executable), falling back to PATH.
func resolveSupervisorPath(path string) (string, error) {
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("supervisor path must be absolute: %s", path)
		}
		if info, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("supervisor path invalid: %w", err)
		} else if info.IsDir() {
			return "", fmt.Errorf("supervisor path is a directory: %s", path)
		}
		return path, nil
	}

	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			return resolved, nil
		}
		return self, nil
	}

	found, err := exec.LookPath("beagle")
	if err != nil {
		return "", fmt.Errorf("beagle not found; set ApplyOptions.SupervisorPath to an absolute beagle binary")
	}
	return filepath.Abs(found)
}
