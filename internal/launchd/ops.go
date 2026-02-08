package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/amterp/beagle/internal/config"
)

type OpsOptions struct {
	HomeDir string
	Runner  CommandRunner
}

func RunNow(f config.File, jobID string, opts OpsOptions) error {
	uid, label, _, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	return runner.Run("launchctl", "kickstart", "-k", "gui/"+uid+"/"+label)
}

func Enable(f config.File, jobID string, opts OpsOptions) error {
	uid, label, plistPath, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	_ = runner.Run("launchctl", "bootout", "gui/"+uid+"/"+label)
	return runner.Run("launchctl", "bootstrap", "gui/"+uid, plistPath)
}

func Disable(f config.File, jobID string, opts OpsOptions) error {
	uid, label, _, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	return runner.Run("launchctl", "bootout", "gui/"+uid+"/"+label)
}

func ReadLogs(f config.File, jobID string, stderr bool, tailLines int, opts OpsOptions) (string, error) {
	_, _, _, _, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return "", err
	}

	home := opts.HomeDir
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	name := "stdout.log"
	if stderr {
		name = "stderr.log"
	}
	path := filepath.Join(home, ".local", "share", "beagle", "logs", jobID, name)
	b, err := os.ReadFile(path)
	if err != nil {
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

func jobRuntimeContext(f config.File, jobID string, opts OpsOptions) (uid string, label string, plistPath string, runner CommandRunner, err error) {
	u, err := user.Current()
	if err != nil {
		return "", "", "", nil, err
	}
	home := opts.HomeDir
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", "", "", nil, err
		}
	}
	_, ok := f.Jobs[jobID]
	if !ok {
		return "", "", "", nil, fmt.Errorf("job not found: %s", jobID)
	}
	label = fmt.Sprintf("com.beagle.%s.%s", sanitizeLabelPart(u.Username), jobID)
	plistPath = filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	runner = opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return u.Uid, label, plistPath, runner, nil
}

func TailFile(path string, tailLines int) (string, error) {
	cmd := exec.Command("tail", "-n", fmt.Sprintf("%d", tailLines), path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	return "", err
}
