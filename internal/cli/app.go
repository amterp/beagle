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
	runNowCmd := ra.NewCmd("run-now").SetDescription("Run a job now, outside its schedule")
	restartCmd := ra.NewCmd("restart").SetDescription("Restart a job: stop the running instance, start a fresh one")
	startCmd := ra.NewCmd("start").SetDescription("Start a stopped job")
	stopCmd := ra.NewCmd("stop").SetDescription("Stop a job until the next apply")
	doctorCmd := ra.NewCmd("doctor").SetDescription("Run environment diagnostics")

	// enable/disable predate start/stop and named the wrong thing: they load and
	// unload a launchd agent, which collides with the config's own `enabled:`
	// field (the durable switch) while being undone by the next apply. Kept
	// hidden so existing muscle memory and scripts still work.
	enableCmd := ra.NewCmd("enable").SetDescription("Deprecated alias for start").SetHidden(true)
	disableCmd := ra.NewCmd("disable").SetDescription("Deprecated alias for stop").SetHidden(true)
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
	runNowForce, _ := ra.NewBool("force").SetOptional(true).SetUsage("Clear an open circuit breaker first").Register(runNowCmd)
	restartJob, _ := ra.NewString("job").SetUsage("Job id, or `supervisor` to re-arm the scheduler").Register(restartCmd)
	restartForce, _ := ra.NewBool("force").SetOptional(true).SetUsage("Clear an open circuit breaker first").Register(restartCmd)
	startJob, _ := ra.NewString("job").SetUsage("Job id").Register(startCmd)
	stopJob, _ := ra.NewString("job").SetUsage("Job id").Register(stopCmd)
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
	restartUsed, err := root.RegisterCmd(restartCmd)
	if err != nil {
		return err
	}
	startUsed, err := root.RegisterCmd(startCmd)
	if err != nil {
		return err
	}
	stopUsed, err := root.RegisterCmd(stopCmd)
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
		return a.runRestart(*configPath, *runNowJob, "run-now", "triggered", *runNowForce)
	case *restartUsed:
		return a.runRestart(*configPath, *restartJob, "restart", "restarted", *restartForce)
	case *startUsed:
		return a.runStart(*configPath, *startJob)
	case *stopUsed:
		return a.runStop(*configPath, *stopJob)
	case *enableUsed:
		a.deprecated("enable", "start")
		return a.runStart(*configPath, *enableJob)
	case *disableUsed:
		a.deprecated("disable", "stop")
		return a.runStop(*configPath, *disableJob)
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

	fmt.Fprint(a.out, listSections(items, loadJobHealth(), time.Now(), config.MachineZone()))
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
	now := time.Now()
	summary, hasRun := loadJobHealth()[item.ID]
	isService := item.Type == "service"

	state := stateCell(item)
	if isService {
		state = serviceStateCell(item, summary, hasRun)
	}
	pairs := [][2]string{
		{"job", item.ID},
		{"type", item.Type},
		{"enabled", yesNo(item.Enabled)},
		{"state", state},
	}
	if isService {
		pairs = append(pairs,
			[2]string{"uptime", uptimeCell(item, summary, hasRun, now)},
			[2]string{"pid", pidCell(item.PID)},
		)
	} else {
		pairs = append(pairs,
			[2]string{"schedule", scheduleDetail(item)},
			[2]string{"next run", nextDetail(item, now, config.MachineZone())},
		)
	}
	if hasRun {
		lastRun := runOutcome(summary, true)
		if when := runWhen(summary, true); when != "" {
			lastRun += "  " + when
		}
		if d := runDuration(summary, true); d != "" {
			lastRun += "  " + d
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
	// A heartbeat only says some tick once succeeded. If launchd can no longer
	// exec the supervisor, the last pre-breakage tick keeps the age looking
	// healthy for minutes, so believing it here is what turned a dead scheduler
	// into a green doctor. Positive evidence of a stale binary overrules it.
	if report.SupervisorProgramMissing {
		ticking = false
		tickDetail = warnStyle.Render("stale binary")
	}
	fmt.Fprintln(a.out, check(report.HomeDirOK, "home directory", ""))
	fmt.Fprintln(a.out, check(report.LaunchAgentsOK && report.LaunchctlOK, "scheduler backend", ""))
	fmt.Fprintln(a.out, check(report.RunnerOK, "runner found", ""))
	fmt.Fprintln(a.out, check(report.SupervisorLoaded && !report.SupervisorProgramMissing, "supervisor loaded", ""))
	fmt.Fprintln(a.out, check(ticking, "supervisor ticking", tickDetail))
	for _, issue := range report.Issues {
		fmt.Fprintf(a.out, "%s %s\n", failStyle.Render(glyphFail), issue)
	}
	// Loaded but not ticking is the silent killer: no scheduled job fires, and
	// apply cannot fix it - it sees a loaded agent with matching plist content and
	// reports "unchanged". Name the one command that does. A stale binary is the
	// exception: there the agent must be re-rendered, and the issue above already
	// says so, so pointing at restart would send the reader down a dead end.
	if report.SupervisorLoaded && !ticking && !report.SupervisorProgramMissing {
		fmt.Fprintf(a.out, "%s %s\n", failStyle.Render(glyphFail),
			"the supervisor is loaded but not ticking, so no scheduled job is firing - run `beagle restart supervisor`")
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

// runRestart backs both `restart` and `run-now`. They are one launchd operation -
// kill any in-flight instance, start a fresh one - reached by two intents:
// bouncing a service onto a rebuilt binary, and rerunning a scheduled job off
// its schedule. cmd names the invoked command for error messages; verb is the
// past-tense word to report on success.
func (a *App) runRestart(configPath string, jobID string, cmd string, verb string, force bool) error {
	fail := errStyle.Render(cmd + " failed:")
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", fail)
	}

	// The supervisor is not a configured job, so it needs no config at all - and
	// asking for it must not fail just because jobs.yaml is missing.
	if jobID == core.SupervisorName {
		if err := launchd.RestartSupervisor(launchd.OpsOptions{}); err != nil {
			return fmt.Errorf("%s %v", fail, err)
		}
		fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" restarted"), "supervisor")
		fmt.Fprintln(a.out, dimStyle.Render("  scheduler re-armed; check `beagle doctor` to confirm it is ticking"))
		return nil
	}

	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", fail, err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", fail, err)
	}
	if _, ok := cfg.Jobs[jobID]; !ok {
		return fmt.Errorf("%s job not found: %s", fail, jobID)
	}
	if err := a.gateBreaker(jobID, force); err != nil {
		return fmt.Errorf("%s %v", fail, err)
	}
	if err := launchd.Restart(cfg, jobID, launchd.OpsOptions{}); err != nil {
		return fmt.Errorf("%s %v", fail, err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" "+verb), jobID)
	return nil
}

func (a *App) runStart(configPath string, jobID string) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", errStyle.Render("start failed:"))
	}
	if jobID == core.SupervisorName {
		return fmt.Errorf("%s %s", errStyle.Render("start failed:"), supervisorNotAJob("start"))
	}
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("start failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("start failed:"), err)
	}
	if err := launchd.Start(cfg, jobID, launchd.OpsOptions{}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("start failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" started"), jobID)
	return nil
}

func (a *App) runStop(configPath string, jobID string) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("%s job id is required", errStyle.Render("stop failed:"))
	}
	if jobID == core.SupervisorName {
		return fmt.Errorf("%s %s", errStyle.Render("stop failed:"), supervisorNotAJob("stop"))
	}
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("stop failed:"), err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("stop failed:"), err)
	}
	job, ok := cfg.Jobs[jobID]
	if !ok {
		return fmt.Errorf("%s job not found: %s", errStyle.Render("stop failed:"), jobID)
	}
	if err := launchd.Stop(cfg, jobID, launchd.OpsOptions{}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("stop failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" stopped"), jobID)
	// A stop that silently un-does itself is the trap the old `disable` had, so
	// say plainly that it is not durable and where the durable switch lives.
	fmt.Fprintln(a.out, dimStyle.Render("  comes back on the next `beagle apply` or reboot - set `enabled: false` in jobs.yaml to keep it down"))
	if strings.EqualFold(job.Type, "schedule") {
		fmt.Fprintln(a.out, dimStyle.Render("  while stopped, the supervisor logs an error each time this job comes due"))
	}
	return nil
}

// supervisorNotAJob explains why stop/start reject the supervisor. Unloading the
// scheduler stops every schedule job at once, so beagle does not offer it as a
// one-word command; restart covers the case people actually want.
func supervisorNotAJob(verb string) string {
	return fmt.Sprintf("the supervisor is the scheduler, not a job, so `%s` does not apply to it - "+
		"use `beagle restart supervisor` to re-arm it, or `beagle apply` to reinstall it", verb)
}

// deprecated warns that an old command name was used and names its replacement.
// It goes to stderr so piping stdout stays clean.
func (a *App) deprecated(old, replacement string) {
	fmt.Fprintf(a.errOut, "%s `beagle %s` is now `beagle %s` - running %s\n",
		warnStyle.Render("note:"), old, replacement, replacement)
}

// gateBreaker stops a manual run the circuit breaker would silently swallow.
// beagle-run consults the breaker itself and records an open-circuit run as
// "skipped" with exit 0, so without this check restart/run-now report success for
// a command that never executed. force clears the breaker rather than refusing.
//
// Best-effort by design: an unreachable run-log must not block the user's actual
// request. The exception is --force, which is an explicit instruction to clear
// state - failing that silently would be its own lie.
func (a *App) gateBreaker(jobID string, force bool) error {
	dbPath, err := runlog.DefaultPath()
	if err == nil {
		if _, statErr := os.Stat(dbPath); statErr != nil {
			err = statErr
		}
	}
	var store *runlog.Store
	if err == nil {
		store, err = runlog.Open(dbPath)
	}
	if err != nil {
		if force {
			return fmt.Errorf("cannot reach the run log at %s to clear the circuit breaker: %w", dbPath, err)
		}
		return nil
	}
	defer store.Close()

	ctx := context.Background()
	st, found, err := store.GetBreakerState(ctx, jobID)
	if err != nil {
		if force {
			return fmt.Errorf("read circuit breaker state: %w", err)
		}
		return nil
	}

	clear, err := breakerGate(st, found, force, jobID, time.Now())
	if err != nil {
		return err
	}
	if !clear {
		return nil
	}
	if err := store.ClearBreaker(ctx, jobID); err != nil {
		return fmt.Errorf("clear circuit breaker: %w", err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render(glyphOK+" breaker cleared"), jobID)
	return nil
}

// breakerGate decides what a manual run should do about the breaker: proceed
// (clear=false, nil), clear it first (clear=true), or refuse with a message that
// explains why the run would have been a no-op. Kept free of I/O so the wording -
// the part that has to teach an operator what to do - stays under test.
func breakerGate(st runlog.BreakerState, found, force bool, jobID string, now time.Time) (clear bool, err error) {
	if !found || !st.IsOpen(now) {
		return false, nil
	}
	if force {
		return true, nil
	}
	return false, fmt.Errorf("circuit breaker is open until %s (%s from now).\n"+
		"  %d failures in the last %s tripped it, so this run would be recorded as skipped and the command would never execute.\n"+
		"  Fix the cause (`beagle logs %s --stderr`, `beagle failures --job %s`), or pass --force to clear the breaker and run anyway.",
		st.OpenUntil.Local().Format("2006-01-02 15:04:05"),
		st.OpenUntil.Sub(now).Round(time.Second),
		st.FailureCount,
		now.Sub(st.WindowFrom).Round(time.Second),
		jobID, jobID)
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
