package main

import (
	"fmt"
	"os"

	"github.com/amterp/beagle/internal/runlog"
	"github.com/amterp/beagle/internal/runner"
	"github.com/amterp/ra"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cmd := ra.NewCmd("beagle-run").SetDescription("Internal Beagle runner")
	jobID, err := ra.NewString("job").SetUsage("Job id").Register(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	command, err := ra.NewStringSlice("command").SetVariadic(true).SetUsage("Command and args").Register(cmd)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := cmd.ParseOrError(args); err != nil {
		if err == ra.HelpInvokedErr {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if len(*command) == 0 {
		fmt.Fprintln(os.Stderr, "missing command arguments")
		return 2
	}

	dbPath, err := runlog.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	store, err := runlog.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer store.Close()

	runner.RotateLogs(*jobID)

	return runner.Run(runner.RunConfig{
		JobID:   *jobID,
		Command: *command,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}, store, os.Stderr)
}
