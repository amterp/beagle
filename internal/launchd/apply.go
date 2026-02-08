package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amterp/beagle/internal/config"
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
	HomeDir    string
	RunnerPath string
	Runner     CommandRunner
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

	home := opts.HomeDir
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return Summary{}, err
		}
	}
	launchDir := filepath.Join(home, "Library", "LaunchAgents")
	logsRoot := filepath.Join(home, ".local", "share", "beagle", "logs")

	if err := os.MkdirAll(launchDir, 0o755); err != nil {
		return Summary{}, fmt.Errorf("create launch agents dir: %w", err)
	}
	if err := os.MkdirAll(logsRoot, 0o755); err != nil {
		return Summary{}, fmt.Errorf("create logs dir: %w", err)
	}

	runnerPath := opts.RunnerPath
	if runnerPath == "" {
		runnerPath = "beagle-run"
	}

	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	u, err := user.Current()
	if err != nil {
		return Summary{}, fmt.Errorf("current user: %w", err)
	}
	uid := u.Uid
	username := sanitizeLabelPart(u.Username)

	desired := map[string]string{}
	desiredLabels := map[string]string{}
	for _, job := range jobs {
		label := fmt.Sprintf("com.beagle.%s.%s", username, job.ID)
		plistPath := filepath.Join(launchDir, label+".plist")
		stdoutPath := filepath.Join(logsRoot, job.ID, "stdout.log")
		stderrPath := filepath.Join(logsRoot, job.ID, "stderr.log")
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

	managedPattern := filepath.Join(launchDir, "com.beagle.*.plist")
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
		_ = runner.Run("launchctl", "bootout", "gui/"+uid+"/"+label)
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
			summary.Unchanged++
			continue
		}

		if exists {
			_ = runner.Run("launchctl", "bootout", "gui/"+uid+"/"+label)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("write %s: %v", path, err))
			continue
		}

		if err := runner.Run("launchctl", "bootstrap", "gui/"+uid, path); err != nil {
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

func sanitizeLabelPart(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return strings.ToLower(s)
}
