package supervisor

import (
	"context"
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

// seedFire gives a job a prior occurrence, so the tick under test exercises the
// catch-up path rather than first-sight adoption. Any test modelling "the Mac
// was off and missed a run" needs this: beagle must already have known the job.
func seedFire(t *testing.T, store *runlog.Store, jobID, occurrence string) {
	t.Helper()
	if err := store.RecordScheduleFire(context.Background(), jobID, occurrence, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
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
	// Yesterday's fire is on record, so today's is a genuine miss to catch up.
	seedFire(t, store, "job_a", "2026-06-17T07:00")
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
	// Hourly job last seen firing at 03:00, machine off since, booted at 09:30
	// with a 24h window. Only the most recent occurrence (09:00) should fire -
	// once, not once per missed hour.
	seedFire(t, store, "job_a", "2026-06-18T03:00")
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

// TestTickFirstSightAdoptsWithoutFiring covers the core of first-sight
// adoption: a job beagle has never seen has missed nothing, so its last
// occurrence becomes the baseline instead of a catch-up run. Without this, a
// long catch_up window would make every newly added job run the moment it was
// applied.
func TestTickFirstSightAdoptsWithoutFiring(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	// 30d window, evaluated 3h after the 07:00 occurrence, with no prior state.
	now := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	res, err := Tick(scheduleJob("0 7 * * *", "30d"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 || len(runner.calls) != 0 {
		t.Fatalf("expected no fire on first sight, got fired=%v calls=%v", res.Fired, runner.calls)
	}
	if len(res.Adopted) != 1 || res.Adopted[0] != "job_a" {
		t.Fatalf("expected job_a to be adopted, got %v", res.Adopted)
	}
	got, ok, err := store.GetScheduleFire(context.Background(), "job_a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || decodeState(got).Occurrence != "2026-06-18T07:00" {
		t.Fatalf("expected the occurrence recorded as baseline, got %q (present=%v)", got, ok)
	}
}

// TestTickFirstSightStillFiresOnTime guards against over-suppression: adoption
// must not swallow a job that is due right now, only one whose occurrence
// predates beagle knowing about it.
func TestTickFirstSightStillFiresOnTime(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	now := time.Date(2026, 6, 18, 7, 0, 30, 0, time.UTC)
	res, err := Tick(scheduleJob("0 7 * * *", "30d"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || len(res.Adopted) != 0 {
		t.Fatalf("expected an on-time fire, not adoption; fired=%v adopted=%v", res.Fired, res.Adopted)
	}
}

// TestTickFirstSightStrictGraceEdge pins the arithmetic in the adoption check.
// PrevFire measures from the truncated minute, so at 07:02:59 a strict job
// legitimately returns the 07:00 occurrence - 2m59s "late" by wall clock, yet
// within the 2m grace it was found under. Testing Now.Sub(prevFire) against
// GraceWindow would wrongly adopt it and silently drop the job's first run.
func TestTickFirstSightStrictGraceEdge(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	now := time.Date(2026, 6, 18, 7, 2, 59, 0, time.UTC)
	res, err := Tick(scheduleJob("0 7 * * *", "none"), deps(store, runner, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || len(res.Adopted) != 0 {
		t.Fatalf("a strict job found within its grace must fire, not be adopted; fired=%v adopted=%v",
			res.Fired, res.Adopted)
	}
}

// TestTickCatchesUpAfterAdoption confirms adoption only costs the first
// occurrence: once a baseline exists, catch-up behaves normally.
func TestTickCatchesUpAfterAdoption(t *testing.T) {
	store := newStore(t)
	runner := &fakeRunner{}
	cfg := scheduleJob("0 7 * * *", "30d")

	adopt := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	if _, err := Tick(cfg, deps(store, runner, adopt)); err != nil {
		t.Fatal(err)
	}

	// Next day, machine booted 3h after the occurrence: a real miss now.
	later := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	res, err := Tick(cfg, deps(store, runner, later))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 {
		t.Fatalf("expected catch-up after adoption, got fired=%v adopted=%v errs=%v",
			res.Fired, res.Adopted, res.Errors)
	}
}
