package runlog

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreStartFinishAndFailures(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	started := time.Now().Add(-2 * time.Second).UTC()
	runID, err := store.StartRun(context.Background(), RunStart{
		JobID:   "worker_a",
		Command: "/bin/echo hello",
		PID:     123,
		Started: started,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.FinishRun(context.Background(), RunFinish{
		ID:         runID,
		Finished:   time.Now().UTC(),
		ExitCode:   2,
		Status:     "failed",
		FailureCls: "exit_nonzero",
		Notes:      "boom",
	})
	if err != nil {
		t.Fatal(err)
	}

	fails, err := store.RecentFailures(context.Background(), "worker_a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fails) != 1 {
		t.Fatalf("expected one failure, got %d", len(fails))
	}
	if fails[0].FailureCls != "exit_nonzero" {
		t.Fatalf("unexpected failure class: %+v", fails[0])
	}
}

func TestWALModeEnabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var mode string
	if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("expected WAL mode, got %q", mode)
	}
}

func TestFinishRunRecordsFailureEvent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	runID, err := store.StartRun(context.Background(), RunStart{
		JobID:   "worker_a",
		Command: "/bin/false",
		PID:     1,
		Started: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.FinishRun(context.Background(), RunFinish{
		ID:         runID,
		Finished:   time.Now().UTC(),
		ExitCode:   1,
		Status:     "failed",
		FailureCls: "exit_nonzero",
	})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	err = store.db.QueryRow(`SELECT COUNT(*) FROM events WHERE event_type = 'run_failed'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 failure event, got %d", count)
	}
}

// TestForeignSchemaWiped verifies that opening a database created by an older,
// incompatible schema (here: a runs table with the dropped namespace column,
// no user_version) recreates it cleanly rather than erroring.
func TestForeignSchemaWiped(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")

	pre, err := Open(dbPath) // borrow Open just to get a connection, then clobber
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pre.db.Exec(`DROP TABLE runs`); err != nil {
		t.Fatal(err)
	}
	if _, err := pre.db.Exec(`CREATE TABLE runs (id INTEGER PRIMARY KEY, job_id TEXT, namespace TEXT, started_at TEXT, status TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pre.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	pre.Close()

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopening a foreign-schema DB should recreate it, got: %v", err)
	}
	defer store.Close()

	if _, err := store.StartRun(context.Background(), RunStart{JobID: "j", Command: "c", PID: 1, Started: time.Now().UTC()}); err != nil {
		t.Fatalf("StartRun on recreated schema failed: %v", err)
	}
}

// TestConcurrentOpensSucceed mirrors normal operation: the supervisor holds an
// open Store while it kicks a job, and beagle-run (plus any CLI command) opens
// the same DB at the same moment. Before the busy_timeout + read-only
// steady-state path, these raced the version write and failed with SQLITE_BUSY,
// so the kicked job's command never ran.
func TestConcurrentOpensSucceed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")

	// The supervisor's open creates the schema and keeps its connection.
	supervisor, err := Open(dbPath)
	if err != nil {
		t.Fatalf("initial open: %v", err)
	}
	defer supervisor.Close()

	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(dbPath)
			if err != nil {
				errs <- err
				return
			}
			errs <- s.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent open while supervisor holds the DB: %v", err)
		}
	}
}

// TestConcurrentColdOpensSucceed covers the first-boot race: several processes
// (e.g. the supervisor and a `beagle apply` fired right after it) open a
// brand-new DB simultaneously, each racing to create the schema. busy_timeout
// makes the losers wait for the init write rather than failing with SQLITE_BUSY.
func TestConcurrentColdOpensSucceed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")

	const n = 8
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(dbPath)
			if err != nil {
				errs <- err
				return
			}
			errs <- s.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent cold open of a fresh DB: %v", err)
		}
	}
}

func TestBreakerStateOpensAfterFailureThreshold(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	policy := BreakerPolicy{
		MaxFailures:     2,
		WindowSeconds:   60,
		CooldownSeconds: 120,
	}

	if err := store.RecordOutcome(context.Background(), "worker_a", now, true, policy); err != nil {
		t.Fatal(err)
	}
	open, _, err := store.IsBreakerOpen(context.Background(), "worker_a", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if open {
		t.Fatal("breaker should not be open after first failure")
	}

	if err := store.RecordOutcome(context.Background(), "worker_a", now.Add(2*time.Second), true, policy); err != nil {
		t.Fatal(err)
	}
	open, until, err := store.IsBreakerOpen(context.Background(), "worker_a", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !open {
		t.Fatal("breaker should be open after second failure")
	}
	if until.Before(now) {
		t.Fatalf("expected future open-until time: %v", until)
	}
}
