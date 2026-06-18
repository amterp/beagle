package supervisor

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amterp/beagle/internal/config"
	"github.com/amterp/beagle/internal/runlog"
)

type fakeRunner struct {
	calls []string
	err   error
}

func (f *fakeRunner) Run(name string, args ...string) error {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return f.err
}

func newStore(t *testing.T) *runlog.Store {
	t.Helper()
	s, err := runlog.Open(filepath.Join(t.TempDir(), "beagle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func scheduleJob(cron, catchUp string) config.File {
	return config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"job_a": {
				Type:     "schedule",
				Command:  []string{"/bin/true"},
				CatchUp:  catchUp,
				Schedule: config.Schedule{Cron: cron, Timezone: "UTC"},
			},
		},
	}
}

func deps(store *runlog.Store, runner *fakeRunner, now time.Time) Deps {
	return Deps{Store: store, Runner: runner, Username: "tester", UID: "501", Now: now}
}

func TestTickFiresOnTime(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	// 07:00 daily, evaluated at 07:00:30.
	now := time.Date(2026, 6, 18, 7, 0, 30, 0, time.UTC)
	res, err := Tick(scheduleJob("0 7 * * *", "none"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || len(runner.calls) != 1 {
		t.Fatalf("expected one fire, got fired=%v calls=%v errs=%v", res.Fired, runner.calls, res.Errors)
	}
	if !strings.Contains(runner.calls[0], "kickstart") || strings.Contains(runner.calls[0], "-k") {
		t.Fatalf("supervisor must kickstart without -k, got %q", runner.calls[0])
	}
}

func TestTickDedupSameOccurrence(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	cfg := scheduleJob("0 7 * * *", "none")
	// First tick at 07:00:30 fires; second tick at 07:01:10 (same 07:00
	// occurrence still within grace) must NOT fire again.
	if _, err := Tick(cfg, deps(store, runner, time.Date(2026, 6, 18, 7, 0, 30, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	res, err := Tick(cfg, deps(store, runner, time.Date(2026, 6, 18, 7, 1, 10, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 {
		t.Fatalf("expected no second fire for the same occurrence, got %v", res.Fired)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected exactly one kick across both ticks, got %d", len(runner.calls))
	}
}

func TestTickStrictSkipsAfterGrace(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	// Strict job, machine booted 10 minutes after the 07:00 occurrence.
	now := time.Date(2026, 6, 18, 7, 10, 0, 0, time.UTC)
	res, err := Tick(scheduleJob("0 7 * * *", "none"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 || len(runner.calls) != 0 {
		t.Fatalf("strict job should not catch up 10m late, got fired=%v calls=%v", res.Fired, runner.calls)
	}
}

func TestTickCatchUpWithinWindow(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	// 6h window, evaluated 2h after the 07:00 occurrence (e.g. booted at 09:00).
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	res, err := Tick(scheduleJob("0 7 * * *", "6h"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 {
		t.Fatalf("expected catch-up fire within window, got fired=%v errs=%v", res.Fired, res.Errors)
	}
}

func TestTickCatchUpBeyondWindowSkips(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	// 1h window, evaluated 2h after the occurrence - too late.
	now := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	res, err := Tick(scheduleJob("0 7 * * *", "1h"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 {
		t.Fatalf("expected skip beyond window, got %v", res.Fired)
	}
}

func TestTickCoalescesMissedOccurrences(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	// Hourly job, machine off for hours, booted at 09:30 with a 24h window.
	// Only the most recent occurrence (09:00) should fire - once.
	now := time.Date(2026, 6, 18, 9, 30, 0, 0, time.UTC)
	res, err := Tick(scheduleJob("0 * * * *", "24h"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || len(runner.calls) != 1 {
		t.Fatalf("expected a single coalesced fire, got fired=%v calls=%v", res.Fired, runner.calls)
	}
}

func TestTickKickFailureLeavesStateForRetry(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{err: errFakeKick}
	cfg := scheduleJob("0 7 * * *", "6h")
	now := time.Date(2026, 6, 18, 7, 0, 30, 0, time.UTC)

	res, err := Tick(cfg, deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 || len(res.Errors) != 1 {
		t.Fatalf("expected a kick error and no fire, got fired=%v errs=%v", res.Fired, res.Errors)
	}

	// Retry with a healthy runner at the next tick: the occurrence must still be
	// pending (state was not advanced on failure) and now fire.
	healthy := &fakeRunner{}
	res2, err := Tick(cfg, deps(store, healthy, now.Add(40*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Fired) != 1 {
		t.Fatalf("expected retry to fire after earlier kick failure, got %v", res2.Fired)
	}
}

func TestTickIgnoresServiceJobs(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	cfg := config.File{
		Version: config.CurrentVersion,
		Jobs: config.Jobs{
			"svc": {Type: "service", Command: []string{"/bin/true"}, Restart: "always"},
		},
	}
	res, err := Tick(cfg, deps(store, runner, time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 || len(runner.calls) != 0 {
		t.Fatalf("service jobs are not the supervisor's concern, got fired=%v calls=%v", res.Fired, runner.calls)
	}
}

var errFakeKick = &fakeKickErr{}

type fakeKickErr struct{}

func (*fakeKickErr) Error() string { return "kick failed" }
