package cli

import (
	"fmt"
	"io"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/launchd"
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
		SetDescription("Beagle orchestrates scheduled and always-on jobs from beagle.yaml").
		SetAutoHelpOnNoArgs(true)

	configPath, err := ra.NewString("config").
		SetShort("c").
		SetDefault("beagle.yaml").
		SetOptional(true).
		SetUsage("Path to beagle.yaml").
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

	statusJob, _ := ra.NewString("job").SetUsage("Job id").Register(statusCmd)
	logsJob, _ := ra.NewString("job").SetUsage("Job id").Register(logsCmd)
	failuresJob, _ := ra.NewString("job").SetUsage("Job id").SetOptional(true).Register(failuresCmd)
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

	if err := root.ParseOrError(args); err != nil {
		if err == ra.HelpInvokedErr {
			return nil
		}
		return err
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
		return a.notImplemented(fmt.Sprintf("logs %s", *logsJob))
	case *failuresUsed:
		if *failuresJob == "" {
			return a.notImplemented("failures")
		}
		return a.notImplemented(fmt.Sprintf("failures %s", *failuresJob))
	case *runNowUsed:
		return a.notImplemented(fmt.Sprintf("run-now %s", *runNowJob))
	case *enableUsed:
		return a.notImplemented(fmt.Sprintf("enable %s", *enableJob))
	case *disableUsed:
		return a.notImplemented(fmt.Sprintf("disable %s", *disableJob))
	case *doctorUsed:
		return a.runDoctor()
	default:
		return nil
	}
}

func (a *App) runValidate(path string) error {
	_, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("validation failed:"), err)
	}

	fmt.Fprintln(a.out, titleStyle.Render("Beagle Config"))
	fmt.Fprintf(a.out, "%s %s\n", okStyle.Render("OK"), infoStyle.Render(path))
	return nil
}

func (a *App) runApply(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("apply failed:"), err)
	}

	summary, err := launchd.Apply(cfg, launchd.ApplyOptions{})
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

func (a *App) runList(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("list failed:"), err)
	}
	items, err := launchd.List(cfg, launchd.StatusOptions{})
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

func (a *App) runStatus(path string, jobID string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("%s %v", errStyle.Render("status failed:"), err)
	}
	item, err := launchd.GetStatus(cfg, jobID, launchd.StatusOptions{})
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

func (a *App) notImplemented(name string) error {
	fmt.Fprintf(a.out, "%s %s\n", infoStyle.Render("TODO:"), name)
	return nil
}
