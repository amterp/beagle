package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/core"
	"github.com/amterp/beagle/internal/launchd"
	"github.com/amterp/beagle/internal/profile"
	"github.com/amterp/beagle/internal/runlog"
	"github.com/amterp/ra"
)

type App struct {
	out    io.Writer
	errOut io.Writer
}

type commandContext struct {
	ConfigPath  string
	ProfileName string
	Namespace   string
}

func New(out, errOut io.Writer) *App {
	return &App{out: out, errOut: errOut}
}

func (a *App) Run(args []string) error {
	root := ra.NewCmd("beagle").
		SetDescription("Beagle orchestrates scheduled and always-on jobs from beagle.yaml").
		SetAutoHelpOnNoArgs(true)

	configPath, err := ra.NewString("config").
		SetShort("c").
		SetOptional(true).
		SetUsage("Path to beagle.yaml").
		Register(root, ra.WithGlobal(true))
	if err != nil {
		return err
	}

	profileName, err := ra.NewString("profile").
		SetOptional(true).
		SetUsage("Named Beagle profile").
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

	profileCmd := ra.NewCmd("profile").SetDescription("Manage Beagle config profiles")
	profileRegisterCmd := ra.NewCmd("register").SetDescription("Register a profile")
	profileListCmd := ra.NewCmd("ls").SetDescription("List profiles")
	profileRemoveCmd := ra.NewCmd("rm").SetDescription("Remove a profile")
	profileUseCmd := ra.NewCmd("use").SetDescription("Set active profile")

	statusJob, _ := ra.NewString("job").SetUsage("Job id or <profile>:<job>").Register(statusCmd)
	logsJob, _ := ra.NewString("job").SetUsage("Job id or <profile>:<job>").Register(logsCmd)
	logsStderr, _ := ra.NewBool("stderr").SetOptional(true).SetUsage("Show stderr log").Register(logsCmd)
	logsTail, _ := ra.NewInt("tail").SetDefault(100).SetOptional(true).SetUsage("Tail line count").Register(logsCmd)
	failuresJob, _ := ra.NewString("job").SetUsage("Job id or <profile>:<job>").SetOptional(true).Register(failuresCmd)
	failuresLimit, _ := ra.NewInt("limit").SetDefault(20).SetOptional(true).SetUsage("Number of failures").Register(failuresCmd)
	runNowJob, _ := ra.NewString("job").SetUsage("Job id or <profile>:<job>").Register(runNowCmd)
	enableJob, _ := ra.NewString("job").SetUsage("Job id or <profile>:<job>").Register(enableCmd)
	disableJob, _ := ra.NewString("job").SetUsage("Job id or <profile>:<job>").Register(disableCmd)

	profileRegisterName, _ := ra.NewString("name").SetUsage("Profile name").Register(profileRegisterCmd)
	profileRegisterConfig, _ := ra.NewString("config").SetUsage("Absolute path to beagle.yaml").Register(profileRegisterCmd)
	profileRemoveName, _ := ra.NewString("name").SetUsage("Profile name").Register(profileRemoveCmd)
	profileUseName, _ := ra.NewString("name").SetUsage("Profile name").Register(profileUseCmd)

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
	profileUsed, err := root.RegisterCmd(profileCmd)
	if err != nil {
		return err
	}

	profileRegisterUsed, err := profileCmd.RegisterCmd(profileRegisterCmd)
	if err != nil {
		return err
	}
	profileListUsed, err := profileCmd.RegisterCmd(profileListCmd)
	if err != nil {
		return err
	}
	profileRemoveUsed, err := profileCmd.RegisterCmd(profileRemoveCmd)
	if err != nil {
		return err
	}
	profileUseUsed, err := profileCmd.RegisterCmd(profileUseCmd)
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
	case *profileUsed && *profileRegisterUsed:
		return a.runProfileRegister(*profileRegisterName, *profileRegisterConfig)
	case *profileUsed && *profileListUsed:
		return a.runProfileList()
	case *profileUsed && *profileRemoveUsed:
		return a.runProfileRemove(*profileRemoveName)
	case *profileUsed && *profileUseUsed:
		return a.runProfileUse(*profileUseName)
	case *validateUsed:
		ctx, err := a.resolveContext(*configPath, *profileName)
		if err != nil {
			return err
		}
		return a.runValidate(ctx)
	case *applyUsed:
		ctx, err := a.resolveContext(*configPath, *profileName)
		if err != nil {
			return err
		}
		return a.runApply(ctx)
	case *lsUsed:
		ctx, err := a.resolveContext(*configPath, *profileName)
		if err != nil {
			return err
		}
		return a.runList(ctx)
	case *statusUsed:
		ctx, jobID, err := a.resolveJobContext(*configPath, *profileName, *statusJob)
		if err != nil {
			return err
		}
		return a.runStatus(ctx, jobID)
	case *logsUsed:
		ctx, jobID, err := a.resolveJobContext(*configPath, *profileName, *logsJob)
		if err != nil {
			return err
		}
		return a.runLogs(ctx, jobID, *logsStderr, *logsTail)
	case *failuresUsed:
		ctx, jobID, err := a.resolveOptionalJobContext(*configPath, *profileName, *failuresJob)
		if err != nil {
			return err
		}
		return a.runFailures(ctx.Namespace, jobID, *failuresLimit)
	case *runNowUsed:
		ctx, jobID, err := a.resolveJobContext(*configPath, *profileName, *runNowJob)
		if err != nil {
			return err
		}
		return a.runNow(ctx, jobID)
	case *enableUsed:
		ctx, jobID, err := a.resolveJobContext(*configPath, *profileName, *enableJob)
		if err != nil {
			return err
		}
		return a.runEnable(ctx, jobID)
	case *disableUsed:
		ctx, jobID, err := a.resolveJobContext(*configPath, *profileName, *disableJob)
		if err != nil {
			return err
		}
		return a.runDisable(ctx, jobID)
	case *doctorUsed:
		return a.runDoctor()
	default:
		return nil
	}
}

func (a *App) runValidate(ctx commandContext) error {
	_, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("validation failed:"), err)
	}

	fmt.Fprintln(a.out, titleStyle.Render("Beagle Config"))
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render("OK"), infoStyle.Render(ctx.ConfigPath))
	return nil
}

func (a *App) runApply(ctx commandContext) error {
	cfg, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("apply failed:"), err)
	}

	summary, err := launchd.Apply(cfg, launchd.ApplyOptions{Namespace: ctx.Namespace})
	fmt.Fprintln(a.out, titleStyle.Render("Beagle Apply"))
	fmt.Fprintf(a.out, "created: %d, updated: %d, removed: %d, unchanged: %d\n",
		summary.Created, summary.Updated, summary.Removed, summary.Unchanged)
	if len(summary.Errors) > 0 {
		for _, e := range summary.Errors {
			fmt.Fprintf(a.out, "- %s\n", e)
		}
	}
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("apply failed:"), err)
	}
	return nil
}

func (a *App) runList(ctx commandContext) error {
	cfg, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("list failed:"), err)
	}
	items, err := launchd.List(cfg, launchd.StatusOptions{Namespace: ctx.Namespace})
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("list failed:"), err)
	}

	fmt.Fprintln(a.out, titleStyle.Render("Beagle Jobs"))
	for _, item := range items {
		state := "not-loaded"
		if item.Loaded {
			state = "loaded"
		}
		fmt.Fprintf(a.out, "- %s (%s) enabled=%t state=%s\n", item.ID, item.Type, item.Enabled, state)
	}
	return nil
}

func (a *App) runStatus(ctx commandContext, jobID string) error {
	cfg, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("status failed:"), err)
	}
	item, err := launchd.GetStatus(cfg, jobID, launchd.StatusOptions{Namespace: ctx.Namespace})
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("status failed:"), err)
	}
	fmt.Fprintln(a.out, titleStyle.Render("Beagle Status"))
	fmt.Fprintf(a.out, "job: %s\n", item.ID)
	fmt.Fprintf(a.out, "type: %s\n", item.Type)
	fmt.Fprintf(a.out, "configured: %t\n", item.Enabled)
	fmt.Fprintf(a.out, "active: %t\n", item.Loaded)
	if item.Disabled {
		fmt.Fprintln(a.out, "runtime state: paused")
	}
	return nil
}

func (a *App) runDoctor() error {
	report, err := launchd.Doctor(launchd.StatusOptions{})
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("doctor failed:"), err)
	}

	fmt.Fprintln(a.out, titleStyle.Render("Beagle Doctor"))
	fmt.Fprintf(a.out, "home dir ready: %t\n", report.HomeDirOK)
	fmt.Fprintf(a.out, "scheduler backend ready: %t\n", report.LaunchAgentsOK && report.LaunchctlOK)
	if len(report.Issues) > 0 {
		for _, issue := range report.Issues {
			fmt.Fprintf(a.out, "- %s\n", issue)
		}
	}
	return nil
}

func (a *App) runLogs(ctx commandContext, jobID string, stderr bool, tail int) error {
	cfg, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("logs failed:"), err)
	}
	out, err := launchd.ReadLogs(cfg, jobID, stderr, tail, launchd.OpsOptions{Namespace: ctx.Namespace})
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

func (a *App) runFailures(namespace string, jobID string, limit int) error {
	dbPath, err := runlog.DefaultPath()
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("failures failed:"), err)
	}
	store, err := runlog.Open(dbPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("failures failed:"), err)
	}
	defer store.Close()

	failures, err := store.RecentFailures(context.Background(), namespace, jobID, limit)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("failures failed:"), err)
	}
	if len(failures) == 0 {
		fmt.Fprintln(a.out, infoStyle.Render("no failures recorded"))
		return nil
	}
	fmt.Fprintln(a.out, titleStyle.Render("Beagle Failures"))
	for _, f := range failures {
		fmt.Fprintf(a.out, "- %s %s exit=%d class=%s\n",
			f.StartedAt.Format("2006-01-02 15:04:05"), f.JobKey, f.ExitCode, f.FailureCls)
	}
	return nil
}

func (a *App) runNow(ctx commandContext, jobID string) error {
	cfg, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("run-now failed:"), err)
	}
	if err := launchd.RunNow(cfg, jobID, launchd.OpsOptions{Namespace: ctx.Namespace}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("run-now failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render("triggered"), jobID)
	return nil
}

func (a *App) runEnable(ctx commandContext, jobID string) error {
	cfg, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("enable failed:"), err)
	}
	if err := launchd.Enable(cfg, jobID, launchd.OpsOptions{Namespace: ctx.Namespace}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("enable failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render("enabled"), jobID)
	return nil
}

func (a *App) runDisable(ctx commandContext, jobID string) error {
	cfg, err := config.Load(ctx.ConfigPath)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("disable failed:"), err)
	}
	if err := launchd.Disable(cfg, jobID, launchd.OpsOptions{Namespace: ctx.Namespace}); err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("disable failed:"), err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render("disabled"), jobID)
	return nil
}

func (a *App) runProfileRegister(name string, configPath string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("profile register failed: name is required")
	}
	if strings.TrimSpace(configPath) == "" {
		return fmt.Errorf("profile register failed: --config is required")
	}
	registryPath, registry, err := loadRegistry()
	if err != nil {
		return fmt.Errorf("profile register failed: %w", err)
	}
	entry, err := profile.Register(&registry, name, configPath)
	if err != nil {
		return fmt.Errorf("profile register failed: %w", err)
	}
	if err := profile.Save(registryPath, registry); err != nil {
		return fmt.Errorf("profile register failed: %w", err)
	}
	fmt.Fprintf(a.out, "%s %s -> %s (ns=%s)\n", okStyle.Render("registered"), profile.NormalizeName(name), entry.ConfigPath, entry.Namespace)
	return nil
}

func (a *App) runProfileList() error {
	_, registry, err := loadRegistry()
	if err != nil {
		return fmt.Errorf("profile ls failed: %w", err)
	}
	fmt.Fprintln(a.out, titleStyle.Render("Beagle Profiles"))
	if len(registry.Profiles) == 0 {
		fmt.Fprintln(a.out, infoStyle.Render("no profiles registered"))
		return nil
	}
	names := make([]string, 0, len(registry.Profiles))
	for name := range registry.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := registry.Profiles[name]
		active := ""
		if registry.Active == name {
			active = " *"
		}
		fmt.Fprintf(a.out, "- %s ns=%s config=%s%s\n", name, entry.Namespace, entry.ConfigPath, active)
	}
	return nil
}

func (a *App) runProfileRemove(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("profile rm failed: name is required")
	}
	registryPath, registry, err := loadRegistry()
	if err != nil {
		return fmt.Errorf("profile rm failed: %w", err)
	}
	if err := profile.Remove(&registry, name); err != nil {
		return fmt.Errorf("profile rm failed: %w", err)
	}
	if err := profile.Save(registryPath, registry); err != nil {
		return fmt.Errorf("profile rm failed: %w", err)
	}
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render("removed"), profile.NormalizeName(name))
	return nil
}

func (a *App) runProfileUse(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("profile use failed: name is required")
	}
	registryPath, registry, err := loadRegistry()
	if err != nil {
		return fmt.Errorf("profile use failed: %w", err)
	}
	entry, err := profile.Use(&registry, name)
	if err != nil {
		return fmt.Errorf("profile use failed: %w", err)
	}
	if err := profile.Save(registryPath, registry); err != nil {
		return fmt.Errorf("profile use failed: %w", err)
	}
	fmt.Fprintf(a.out, "%s %s (ns=%s config=%s)\n", okStyle.Render("active"), profile.NormalizeName(name), entry.Namespace, entry.ConfigPath)
	return nil
}

func (a *App) resolveJobContext(configPath string, profileName string, rawJob string) (commandContext, string, error) {
	selectorProfile, jobID := core.SplitJobSelector(rawJob)
	if jobID == "" {
		return commandContext{}, "", fmt.Errorf("job id is required")
	}
	override := profileName
	if selectorProfile != "" {
		override = selectorProfile
	}
	ctx, err := a.resolveContext(configPath, override)
	if err != nil {
		return commandContext{}, "", err
	}
	return ctx, jobID, nil
}

func (a *App) resolveOptionalJobContext(configPath string, profileName string, rawJob string) (commandContext, string, error) {
	selectorProfile, jobID := core.SplitJobSelector(rawJob)
	override := profileName
	if selectorProfile != "" {
		override = selectorProfile
	}
	if strings.TrimSpace(configPath) == "" && strings.TrimSpace(override) == "" && strings.TrimSpace(jobID) == "" {
		return commandContext{}, "", nil
	}
	ctx, err := a.resolveContext(configPath, override)
	if err != nil {
		return commandContext{}, "", err
	}
	return ctx, jobID, nil
}

func (a *App) resolveContext(configPath string, profileName string) (commandContext, error) {
	profileName = profile.NormalizeName(profileName)
	_, registry, err := loadRegistry()
	if err != nil {
		return commandContext{}, err
	}

	if strings.TrimSpace(configPath) != "" {
		abs, err := filepath.Abs(configPath)
		if err != nil {
			return commandContext{}, fmt.Errorf("resolve config path: %w", err)
		}
		ctx := commandContext{ConfigPath: abs, Namespace: core.NamespaceFromPath(abs)}
		if profileName != "" {
			entry, ok := registry.Profiles[profileName]
			if !ok {
				return commandContext{}, fmt.Errorf("profile not found: %s", profileName)
			}
			if entry.ConfigPath != abs {
				return commandContext{}, fmt.Errorf("--config %s does not match profile %s", abs, profileName)
			}
			ctx.ProfileName = profileName
			ctx.Namespace = entry.Namespace
		}
		return ctx, nil
	}

	if profileName != "" {
		entry, ok := registry.Profiles[profileName]
		if !ok {
			return commandContext{}, fmt.Errorf("profile not found: %s", profileName)
		}
		return commandContext{ConfigPath: entry.ConfigPath, ProfileName: profileName, Namespace: entry.Namespace}, nil
	}

	if registry.Active != "" {
		entry, ok := registry.Profiles[registry.Active]
		if ok {
			return commandContext{ConfigPath: entry.ConfigPath, ProfileName: registry.Active, Namespace: entry.Namespace}, nil
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return commandContext{}, err
	}
	cfg := filepath.Join(wd, "beagle.yaml")
	return commandContext{ConfigPath: cfg, Namespace: core.NamespaceFromPath(cfg)}, nil
}

func loadRegistry() (string, profile.Registry, error) {
	path, err := profile.DefaultPath()
	if err != nil {
		return "", profile.Registry{}, err
	}
	r, err := profile.Load(path)
	if err != nil {
		return "", profile.Registry{}, err
	}
	return path, r, nil
}
