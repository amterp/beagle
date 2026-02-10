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
	HomeDir   string
	Runner    CommandRunner
	Namespace string
}

func RunNow(f config.File, jobID string, opts OpsOptions) error {
	uc, label, _, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	return runner.Run("launchctl", "kickstart", "-k", "gui/"+uc.UID+"/"+label)
}

func Enable(f config.File, jobID string, opts OpsOptions) error {
	uc, label, plistPath, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	_ = runner.Run("launchctl", "bootout", "gui/"+uc.UID+"/"+label)
	return runner.Run("launchctl", "bootstrap", "gui/"+uc.UID, plistPath)
}

func Disable(f config.File, jobID string, opts OpsOptions) error {
	uc, label, _, runner, err := jobRuntimeContext(f, jobID, opts)
	if err != nil {
		return err
	}
	return runner.Run("launchctl", "bootout", "gui/"+uc.UID+"/"+label)
}

func ReadLogs(f config.File, jobID string, stderr bool, tailLines int, opts OpsOptions) (string, error) {
	_, _, _, _, err := jobRuntimeContext(f, jobID, opts)
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
	namespace := core.NormalizeNamespace(opts.Namespace)
	path := core.LogFilePath(uc.HomeDir, namespace, jobID, stream)
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

func jobRuntimeContext(f config.File, jobID string, opts OpsOptions) (uc core.UserContext, label string, plistPath string, runner CommandRunner, err error) {
	uc, err = core.CurrentUserWithHome(opts.HomeDir)
	if err != nil {
		return core.UserContext{}, "", "", nil, err
	}
	_, ok := f.Jobs[jobID]
	if !ok {
		return core.UserContext{}, "", "", nil, fmt.Errorf("job not found: %s", jobID)
	}
	namespace := core.NormalizeNamespace(opts.Namespace)
	label = core.BuildLabel(uc.Username, namespace, jobID)
	plistPath = core.PlistPath(uc.HomeDir, label)
	runner = opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return uc, label, plistPath, runner, nil
}
