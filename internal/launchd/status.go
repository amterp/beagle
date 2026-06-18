package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
)

type OutputRunner func(name string, args ...string) (string, error)

type StatusOptions struct {
	HomeDir string
	RunOut  OutputRunner
}

type JobStatus struct {
	ID       string
	Type     string
	Enabled  bool
	Label    string
	Plist    string
	Loaded   bool
	Disabled bool
	Raw      string
}

type DoctorReport struct {
	HomeDirOK        bool
	LaunchAgentsOK   bool
	LaunchctlOK      bool
	RunnerOK         bool
	SupervisorLoaded bool
	Issues           []string
}

func List(f config.File, opts StatusOptions) ([]JobStatus, error) {
	resolved, err := config.Resolve(f)
	if err != nil {
		return nil, err
	}

	uc, outRunner, err := statusContext(opts)
	if err != nil {
		return nil, err
	}

	items := make([]JobStatus, 0, len(resolved))
	for _, j := range resolved {
		label := core.BuildLabel(uc.Username, j.ID)
		plist := core.PlistPath(uc.HomeDir, label)
		raw, loaded, disabled := inspectLabel(outRunner, uc.UID, label)
		items = append(items, JobStatus{
			ID:       j.ID,
			Type:     j.Type,
			Enabled:  j.Enabled,
			Label:    label,
			Plist:    plist,
			Loaded:   loaded,
			Disabled: disabled,
			Raw:      raw,
		})
	}
	sort.Slice(items, func(i, k int) bool { return items[i].ID < items[k].ID })
	return items, nil
}

func GetStatus(f config.File, jobID string, opts StatusOptions) (JobStatus, error) {
	items, err := List(f, opts)
	if err != nil {
		return JobStatus{}, err
	}
	for _, item := range items {
		if item.ID == jobID {
			return item, nil
		}
	}
	return JobStatus{}, fmt.Errorf("job not found: %s", jobID)
}

func Doctor(opts StatusOptions) (DoctorReport, error) {
	home := opts.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return DoctorReport{}, err
		}
	}

	report := DoctorReport{}
	if st, err := os.Stat(home); err == nil && st.IsDir() {
		report.HomeDirOK = true
	} else {
		report.Issues = append(report.Issues, "home directory is missing or inaccessible")
	}

	launchDir := core.LaunchAgentsDir(home)
	if st, err := os.Stat(launchDir); err == nil && st.IsDir() {
		report.LaunchAgentsOK = true
	} else {
		report.Issues = append(report.Issues, "scheduler backend directory is missing")
	}

	runner := opts.RunOut
	if runner == nil {
		runner = execOutput
	}
	if _, err := runner("launchctl", "help"); err == nil {
		report.LaunchctlOK = true
	} else {
		report.Issues = append(report.Issues, "scheduler backend command is unavailable")
	}

	// The plist hard-codes an absolute path to beagle-run; if it can't be
	// resolved, every job fails the moment launchd invokes it.
	if _, err := resolveRunnerPath(strings.TrimSpace(os.Getenv("BEAGLE_RUNNER_PATH"))); err == nil {
		report.RunnerOK = true
	} else {
		report.Issues = append(report.Issues, "beagle-run not found (set BEAGLE_RUNNER_PATH or `go install ./cmd/beagle-run`)")
	}

	// The supervisor is the single agent that drives every schedule job; if it
	// isn't loaded, nothing scheduled will ever fire.
	if uc, err := core.CurrentUserWithHome(home); err == nil {
		if _, loaded, _ := inspectLabel(runner, uc.UID, core.SupervisorLabel(uc.Username)); loaded {
			report.SupervisorLoaded = true
		} else {
			report.Issues = append(report.Issues, "scheduler supervisor is not loaded - run `beagle apply`")
		}
	}

	return report, nil
}

func statusContext(opts StatusOptions) (uc core.UserContext, outRunner OutputRunner, err error) {
	uc, err = core.CurrentUserWithHome(opts.HomeDir)
	if err != nil {
		return core.UserContext{}, nil, err
	}
	outRunner = opts.RunOut
	if outRunner == nil {
		outRunner = execOutput
	}
	return uc, outRunner, nil
}

func inspectLabel(runOut OutputRunner, uid string, label string) (raw string, loaded bool, disabled bool) {
	out, err := runOut("launchctl", "print", "gui/"+uid+"/"+label)
	if err != nil {
		return strings.TrimSpace(out), false, false
	}
	raw = strings.TrimSpace(out)
	disabled = strings.Contains(raw, `"Disabled" => true`) || strings.Contains(raw, "disabled = true")
	return raw, true, disabled
}

func execOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	b, err := cmd.CombinedOutput()
	return string(b), err
}
