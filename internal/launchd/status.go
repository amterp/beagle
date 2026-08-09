package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
)

type OutputRunner func(name string, args ...string) (string, error)

type StatusOptions struct {
	HomeDir string
	RunOut  OutputRunner
}

// JobStatus is a job's configuration alongside what launchd currently thinks
// of it. The resolved config is embedded rather than copied field by field, so
// callers reach ID/Type/Enabled exactly as before while also getting the
// schedule, timezone and catch-up window that ls needs to describe a job.
type JobStatus struct {
	config.ResolvedJob
	Label    string
	Plist    string
	Loaded   bool
	Disabled bool
	// PID is the process launchd is supervising, or 0 when it is running
	// nothing. For a service that is the live truth about whether it is up,
	// which a run-log row cannot tell you: the row still says "running" after
	// the process has died.
	PID int
	Raw string
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
			ResolvedJob: j,
			Label:       label,
			Plist:       plist,
			Loaded:      loaded,
			Disabled:    disabled,
			PID:         parsePID(raw),
			Raw:         raw,
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

// pidPattern matches the pid line in `launchctl print` output, which reads
// "\tpid = 759". Anchoring to a line start keeps it off the other keys that
// end in "pid" (there is no such key today, but the dump is not our format to
// rely on).
var pidPattern = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)`)

// parsePID pulls the supervised process ID out of a launchctl print dump.
// Absent (a loaded agent running nothing) or unparseable both yield 0, which
// callers render as "not running" rather than guessing.
func parsePID(raw string) int {
	m := pidPattern.FindStringSubmatch(raw)
	if m == nil {
		return 0
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return pid
}

func execOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	b, err := cmd.CombinedOutput()
	return string(b), err
}
