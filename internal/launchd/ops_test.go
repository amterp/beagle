package launchd

import (
	"errors"
	"strings"
	"testing"

	"github.com/amterp/beagle/internal/config"
)

// TestEnableUsesReload is the regression for `beagle enable`: like apply, it
// must tolerate a transient bootstrap failure rather than leaving the job down.
// Pre-fix Enable returned that first error; reload retries and heals.
func TestEnableUsesReload(t *testing.T) {
	dir := t.TempDir()
	f := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"worker_a": {
				Type:    "service",
				Command: []string{"/bin/echo", "hello"},
				Restart: "always",
			},
		},
	}

	bootstraps := 0
	r := &scriptRunner{}
	r.fn = func(call string, _ []string) error {
		switch {
		case strings.HasPrefix(call, "launchctl print"):
			return errors.New("not loaded")
		case strings.HasPrefix(call, "launchctl bootstrap"):
			bootstraps++
			if bootstraps == 1 {
				return errors.New("5: Input/output error")
			}
			return nil
		default:
			return nil
		}
	}

	if err := Enable(f, "worker_a", OpsOptions{HomeDir: dir, Runner: r}); err != nil {
		t.Fatalf("Enable returned error: %v", err)
	}
	if bootstraps != 2 {
		t.Fatalf("expected Enable to retry bootstrap (2 attempts), got %d", bootstraps)
	}
}
