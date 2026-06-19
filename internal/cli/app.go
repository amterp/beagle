package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
	"github.com/amterp/beagle/internal/launchd"
	"github.com/amterp/beagle/internal/runlog"
	"github.com/amterp/beagle/internal/supervisor"
	"github.com/amterp/ra"
)

type App struct {
	out    io.Writer
	errOut io.Writer
}

func New(out, errOut io.Writer) *App {
	return &App{out: out, errOut: errOut}
}

func (a *App) Run(args []string) error {
	root := ra.NewCmd("beagle").
		SetDescription("Beagle orchestrates scheduled and always-on jobs from ~/.beagle/jobs.yaml").
		SetAutoHelpOnNoArgs(true)

	configPath, err := ra.NewString("config").
		SetShort("c").
		SetOptional(true).
		SetUsage("Path to a jobs config (defaults to ~/.beagle/jobs.yaml)").
		Register(root, ra.WithGlobal(true))
	if err != nil {
		return err
	}

	validateCmd := ra.NewCmd("validate").SetDescription("Validate Beagle configuration")
	applyCmd := ra.NewCmd("apply").SetDescription("Reconcile Beagle-managed jobs")
	lsCmd := ra.NewCmd("ls").SetDescription("List configured jobs")
	statusCmd := ra.NewCmd("status").SetDescription("Show detailed job status")
	logsCmd := ra.NewCmd("logs").SetDescription("Show job logs")
	failuresCmd := ra.NewCmd("failures").SetDescription("Show recent failures")
	runNowCmd := ra.NewCmd("run-now").SetDescription("Trigger immediate run")
	enableCmd := ra.NewCmd("enable").SetDescription("Enable a job")
	disableCmd := ra.NewCmd("disable").SetDescription("Disable a job")
	doctorCmd := ra.NewCmd("doctor").SetDescription("Run environment diagnostics")
	superviseCmd := ra.NewCmd("supervise").
		SetDescription("Run one scheduler tick; invoked by launchd").
		SetHidden(true)

	statusJob, _ := ra.NewString("job").SetUsage("Job id").Register(statusCmd)
	logsJob, _ := ra.NewString("job").SetUsage("Job id").Register(logsCmd)
	logsStderr, _ := ra.NewBool("stderr").SetOptional(true).SetUsage("Show stderr log").Register(logsCmd)
	logsTail, _ := ra.NewInt("tail").SetDefault(100).SetOptional(true).SetUsage("Tail line count").Register(logsCmd)
	failuresJob, _ := ra.NewString("job").SetUsage("Job id").SetOptional(true).Register(failuresCmd)
	failuresLimit, _ := ra.NewInt("limit").SetDefault(20).SetOptional(true).SetUsage("Number of failures").Register(failuresCmd)
	runNowJob, _ := ra.NewString("job").SetUsage("Job id").Register(runNowCmd)
	enableJob, _ := ra.NewString("job").SetUsage("Job id").Register(enableCmd)
	disableJob, _ := ra.NewString("job").SetUsage("Job id").Register(disableCmd)

	validateUsed, err := root.RegisterCmd(validateCmd)
	if err != nil {
		return err
	}
	applyUsed, err := root.RegisterCmd(applyCmd)
	if err != nil {
		return err
	}
	lsUsed, err := root.RegisterCmd(lsCmd)
	if err != nil {
		return err
	}
	statusUsed, err := root.RegisterCmd(statusCmd)
	if err != nil {
		return err
	}
	logsUsed, err := root.RegisterCmd(logsCmd)
	if err != nil {
		return err
	}
	failuresUsed, err := root.RegisterCmd(failuresCmd)
	if err != nil {
		return err
	}
	runNowUsed, err := root.RegisterCmd(runNowCmd)
	if err != nil {
		return err
	}
	enableUsed, err := root.RegisterCmd(enableCmd)
	if err != nil {
		return err
	}
	disableUsed, err := root.RegisterCmd(disableCmd)
	if err != nil {
		return err
	}
	doctorUsed, err := root.RegisterCmd(doctorCmd)
	if err != nil {
		return err
	}
	superviseUsed, err := root.RegisterCmd(superviseCmd)
	if err != nil {
		return err
	}

	if err := root.ParseOrError(args); err != nil {
		if err == ra.HelpInvokedErr {
			fmt.Fprint(a.out, root.GenerateLongUsage())
			return nil
		}
		return fmt.Errorf("%v\n\n%s", err, root.GenerateLongUsage())
	}

	switch {
	case *validateUsed:
		return a.runValidate(*configPath)
	case *applyUsed:
		return a.runApply(*configPath)
	case *lsUsed:
		return a.runList(*configPath)
	case *statusUsed:
		return a.runStatus(*configPath, *statusJob)
	case *logsUsed:
		return a.runLogs(*configPath, *logsJob, *logsStderr, *logsTail)
	case *failuresUsed:
		return a.runFailures(*failuresJob, *failuresLimit)
	case *runNowUsed:
		return a.runNow(*configPath, *runNowJob)
	case *enableUsed:
		return a.runEnable(*configPath, *enableJob)
	case *disableUsed:
		return a.runDisable(*configPath, *disableJob)
	case *doctorUsed:
		return a.runDoctor()
	case *superviseUsed:
		if code := supervisor.Supervise(a.errOut); code != 0 {
			return fmt.Errorf("supervisor tick reported errors")
		}
		return nil
	default:
		return nil
	}
}

func (a *App) runValidate(configPath string) error {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("validation failed:"), err)
	}
	if _, err := config.Load(path); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("validation failed:"), err)
	}

	fmt.Fprintf(a.out, "%s  %s\n", okStyle.Render(glyphOK+" config valid"), dimStyle.Render(path))
	return nil
}

func (a *App) runApply(configPath string) error {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("apply failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("apply failed:"), err)
	}

	summary, err := launchd.Apply(cfg, launchd.ApplyOptions{})
	fmt.Fprintln(a.out, applyLine(summary))
	for _, e := range summary.Errors {
		fmt.Fprintf(a.out, "%s %s\n", failStyle.Render(glyphFail), e)
	}
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("apply failed:"), err)
	}
	return nil
}

func (a *App) runList(configPath string) error {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("list failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("list failed:"), err)
	}
	items, err := launchd.List(cfg, launchd.StatusOptions{})
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("list failed:"), err)
	}

	if len(items) == 0 {
		fmt.Fprintln(a.out, infoStyle.Render("no jobs configured"))
		return nil
	}

	health := loadJobHealth()

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		summary, ok := health[item.ID]
		rows = append(rows, []string{
			item.ID,
			item.Type,
			yesNo(item.Enabled),
			stateCell(item),
			runOutcome(summary, ok),
			runWhen(summary, ok),
		})
	}
	fmt.Fprint(a.out, table([]string{"JOB", "TYPE", "ENABLED", "STATE", "LAST RUN", ""}, rows))
	return nil
}

func (a *App) runStatus(configPath string, jobID string) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", errStyle.Render("status failed:"))
	}
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("status failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("status failed:"), err)
	}
	item, err := launchd.GetStatus(cfg, jobID, launchd.StatusOptions{})
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("status failed:"), err)
	}
	pairs := [][2]string{
		{"job", item.ID},
		{"type", item.Type},
		{"enabled", yesNo(item.Enabled)},
		{"state", stateCell(item)},
	}
	if summary, ok := loadJobHealth()[item.ID]; ok {
		lastRun := runOutcome(summary, true)
		if when := runWhen(summary, true); when != "" {
			lastRun += "  " + when
		}
		pairs = append(pairs, [2]string{"last run", lastRun})
	}
	fmt.Fprint(a.out, kv(pairs))
	return nil
}

func (a *App) runDoctor() error {
	report, err := launchd.Doctor(launchd.StatusOptions{})
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("doctor failed:"), err)
	}

	ticking, tickDetail := supervisorTickStatus()
	fmt.Fprintln(a.out, check(report.HomeDirOK, "home directory", ""))
	fmt.Fprintln(a.out, check(report.LaunchAgentsOK && report.LaunchctlOK, "scheduler backend", ""))
	fmt.Fprintln(a.out, check(report.RunnerOK, "runner found", ""))
	fmt.Fprintln(a.out, check(report.SupervisorLoaded, "supervisor loaded", ""))
	fmt.Fprintln(a.out, check(ticking, "supervisor ticking", tickDetail))
	for _, issue := range report.Issues {
		fmt.Fprintf(a.out, "%s %s\n", failStyle.Render(glyphFail), issue)
	}
	return nil
}

// supervisorTickStatus reports whether the supervisor is ticking and a styled
// detail. A loaded-but-not-ticking supervisor means scheduled jobs are silently
// not firing - the exact failure this surfaces. Stale/never/unknown all read as
// not-ticking so doctor flags them with a red ✗.
func supervisorTickStatus() (ok bool, detail string) {
	dbPath, err := runlog.DefaultPath()
	if err != nil {
		return false, dimStyle.Render("unknown")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return false, dimStyle.Render("never")
	}
	store, err := runlog.Open(dbPath)
	if err != nil {
		return false, dimStyle.Render("unknown")
	}
	defer store.Close()
	_, ts, found, err := store.GetMeta(context.Background(), supervisor.TickHeartbeatKey)
	if err != nil || !found {
		return false, dimStyle.Render("never")
	}
	age := time.Since(ts).Round(time.Second)
	if age <= 3*time.Minute {
		return true, dimStyle.Render(fmt.Sprintf("%s ago", age))
	}
	return false, warnStyle.Render(fmt.Sprintf("stale %s ago", age))
}

func (a *App) runLogs(configPath string, jobID string, stderr bool, tail int) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", errStyle.Render("logs failed:"))
	}
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("logs failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("logs failed:"), err)
	}
	out, err := launchd.ReadLogs(cfg, jobID, stderr, tail, launchd.OpsOptions{})
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("logs failed:"), err)
	}
	if strings.TrimSpace(out) == "" {
		fmt.Fprintln(a.out, infoStyle.Render("no log output"))
		return nil
	}
	fmt.Fprint(a.out, out)
	return nil
}

func (a *App) runFailures(jobID string, limit int) error {
	dbPath, err := runlog.DefaultPath()
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("failures failed:"), err)
	}
	store, err := runlog.Open(dbPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("failures failed:"), err)
	}
	defer store.Close()

	failures, err := store.RecentFailures(context.Background(), jobID, limit)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("failures failed:"), err)
	}
	if len(failures) == 0 {
		fmt.Fprintln(a.out, infoStyle.Render("no failures recorded"))
		return nil
	}
	rows := make([][]string, 0, len(failures))
	for _, f := range failures {
		rows = append(rows, []string{
			dimStyle.Render(f.StartedAt.Local().Format("01-02 15:04:05")),
			f.JobID,
			failStyle.Render(fmt.Sprintf("exit %d", f.ExitCode)),
			dimStyle.Render(f.FailureCls),
		})
	}
	fmt.Fprint(a.out, table([]string{"TIME", "JOB", "EXIT", "CLASS"}, rows))
	return nil
}

func (a *App) runNow(configPath string, jobID string) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", errStyle.Render("run-now failed:"))
	}
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("run-now failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("run-now failed:"), err)
	}
	if err := launchd.RunNow(cfg, jobID, launchd.OpsOptions{}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("run-now failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" triggered"), jobID)
	return nil
}

func (a *App) runEnable(configPath string, jobID string) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", errStyle.Render("enable failed:"))
	}
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("enable failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("enable failed:"), err)
	}
	if err := launchd.Enable(cfg, jobID, launchd.OpsOptions{}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("enable failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" enabled"), jobID)
	return nil
}

func (a *App) runDisable(configPath string, jobID string) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", errStyle.Render("disable failed:"))
	}
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("disable failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("disable failed:"), err)
	}
	if err := launchd.Disable(cfg, jobID, launchd.OpsOptions{}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("disable failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" disabled"), jobID)
	return nil
}

// resolveConfigPath returns the config file to operate on: an explicit --config
// override if given, otherwise the single global ~/.beagle/jobs.yaml. When the
// default is missing, it returns a hint rather than a bare open error.
func resolveConfigPath(configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		abs, err := filepath.Abs(configPath)
		if err != nil {
			return "", fmt.Errorf("resolve config path: %w", err)
		}
		return abs, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := core.ConfigPath(home)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("no config at %s - create one or pass --config <path>", path)
	}
	return path, nil
}

// loadJobHealth reads the most recent run per job from the run-log DB, keyed by
// job id, so ls/status can surface "loaded but failing" rather than just
// "loaded". The render layer owns all formatting. Best-effort: any error yields
// an empty map and callers degrade gracefully.
func loadJobHealth() map[string]runlog.RunSummary {
	dbPath, err := runlog.DefaultPath()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	store, err := runlog.Open(dbPath)
	if err != nil {
		return nil
	}
	defer store.Close()
	summaries, err := store.LastRunSummaries(context.Background())
	if err != nil {
		return nil
	}
	out := make(map[string]runlog.RunSummary, len(summaries))
	for _, s := range summaries {
		out[s.JobID] = s
	}
	return out
}
