package launchd

import (
	"errors"
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/config"
)

func TestListUsesLaunchctlInspection(t *testing.T) {
	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo", "hello"},
			},
		},
	}

	runOut := func(name string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "print" {
			return "state = running", nil
		}
		return "", nil
	}

	items, err := List(f, StatusOptions{HomeDir: t.TempDir(), RunOut: runOut})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}
	if !items[0].Loaded {
		t.Fatalf("expected loaded item: %+v", items[0])
	}
}

func TestDoctorDetectsMissingLaunchctl(t *testing.T) {
	runOut := func(name string, args ...string) (string, error) {
		return "", errors.New("missing")
	}

	report, err := Doctor(StatusOptions{HomeDir: t.TempDir(), RunOut: runOut})
	if err != nil {
		t.Fatal(err)
	}
	if report.LaunchctlOK {
		t.Fatalf("expected launchctl to fail: %+v", report)
	}
	if !strings.Contains(strings.Join(report.Issues, "\n"), "scheduler backend command") {
		t.Fatalf("expected scheduler backend issue, got %+v", report)
	}
}
