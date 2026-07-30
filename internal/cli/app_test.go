package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amterp/beagle/internal/runlog"
)

func TestRunValidateSuccess(t *testing.T) {
	yaml := `version: 1
jobs:
  worker_a:
    type: service
    command: ["/bin/echo", "hello"]
`
	dir := t.TempDir()
	cfg := dir + "/beagle.yaml"
	if err := os.WriteFile(cfg, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	if err := app.Run([]string{"validate", "--config", cfg}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.String(), "config valid") {
		t.Fatalf("expected valid-config output, got: %s", out.String())
	}
}

func TestRunValidateFailure(t *testing.T) {
	var out, errOut bytes.Buffer
	app := New(&out, &errOut)
	err := app.Run([]string{"validate", "--config", "/nope/beagle.yaml"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "validation failed:") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBreakerGate pins the behavior that stops `restart`/`run-now` from
// reporting success for a run the breaker would silently swallow.
func TestBreakerGate(t *testing.T) {
	now := time.Date(2026, 7, 29, 18, 30, 0, 0, time.UTC)
	open := runlog.BreakerState{
		FailureCount: 5,
		WindowFrom:   now.Add(-10 * time.Minute),
		OpenUntil:    now.Add(12 * time.Minute),
	}

	t.Run("open refuses and explains", func(t *testing.T) {
		clear, err := breakerGate(open, true, false, "helm_refresh", now)
		if err == nil {
			t.Fatal("expected an open breaker to refuse the run")
		}
		if clear {
			t.Fatal("a refusal must not clear the breaker")
		}
		msg := err.Error()
		// The message renders the reopen time in local time, which is what an
		// operator reads, so derive the expectation the same way.
		reopen := open.OpenUntil.Local().Format("2006-01-02 15:04:05")
		for _, want := range []string{reopen, "12m0s", "5 failures", "--force", "beagle failures --job helm_refresh"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message should mention %q, got: %s", want, msg)
			}
		}
	})

	t.Run("open with force clears", func(t *testing.T) {
		clear, err := breakerGate(open, true, true, "helm_refresh", now)
		if err != nil {
			t.Fatalf("--force should not error: %v", err)
		}
		if !clear {
			t.Fatal("--force should clear an open breaker")
		}
	})

	t.Run("expired breaker proceeds untouched", func(t *testing.T) {
		expired := open
		expired.OpenUntil = now.Add(-time.Second)
		clear, err := breakerGate(expired, true, false, "helm_refresh", now)
		if err != nil || clear {
			t.Fatalf("a closed breaker should just proceed, got clear=%v err=%v", clear, err)
		}
	})

	t.Run("no breaker record proceeds", func(t *testing.T) {
		clear, err := breakerGate(runlog.BreakerState{}, false, true, "helm_refresh", now)
		if err != nil || clear {
			t.Fatalf("a job with no breaker row should proceed, got clear=%v err=%v", clear, err)
		}
	})
}

// TestDeprecatedAliasesRouteToNewCommands: enable/disable must keep working and
// must say what replaced them, on stderr so piped stdout stays clean.
func TestDeprecatedAliasesRouteToNewCommands(t *testing.T) {
	for _, tc := range []struct{ old, replacement string }{
		{"enable", "start"},
		{"disable", "stop"},
	} {
		t.Run(tc.old, func(t *testing.T) {
			var out, errOut bytes.Buffer
			app := New(&out, &errOut)
			// Points at a missing config, so it fails after routing - enough to
			// prove which handler ran without touching this machine's launchd.
			err := app.Run([]string{tc.old, "worker_a", "--config", "/nope/beagle.yaml"})
			if err == nil {
				t.Fatal("expected the missing config to fail")
			}
			if !strings.Contains(err.Error(), tc.replacement+" failed:") {
				t.Errorf("expected routing to %s, got: %v", tc.replacement, err)
			}
			if !strings.Contains(errOut.String(), "beagle "+tc.replacement) {
				t.Errorf("expected a deprecation note naming `beagle %s`, got: %s", tc.replacement, errOut.String())
			}
		})
	}
}

// TestSupervisorRejectedByStopAndStart: `supervisor` is a reserved id, not a job,
// so these must explain that rather than report a confusing "job not found".
func TestSupervisorRejectedByStopAndStart(t *testing.T) {
	for _, cmd := range []string{"start", "stop"} {
		t.Run(cmd, func(t *testing.T) {
			var out, errOut bytes.Buffer
			app := New(&out, &errOut)
			err := app.Run([]string{cmd, "supervisor"})
			if err == nil {
				t.Fatal("expected an error for the supervisor")
			}
			if !strings.Contains(err.Error(), "beagle restart supervisor") {
				t.Errorf("error should point at `beagle restart supervisor`, got: %v", err)
			}
		})
	}
}
