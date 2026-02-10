package runlog

import (
	"context"
	"database/sql"
	"path/filepath"
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
		JobID:     "worker_a",
		JobKey:    "team-a:worker_a",
		Namespace: "team-a",
		Command:   "/bin/echo hello",
		PID:       123,
		Started:   started,
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

	fails, err := store.RecentFailures(context.Background(), "team-a", "worker_a", 10)
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
		JobKey:  "ns:worker_a",
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

func TestEnsureColumnQuotesIdentifiers(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "beagle.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Verify the ensureColumn function works correctly with double-quoted
	// identifiers by adding a test column to the runs table.
	err = store.ensureColumn(context.Background(), "runs", "test_col", "TEXT DEFAULT 'ok'")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the column exists
	var val sql.NullString
	err = store.db.QueryRow(`SELECT test_col FROM runs LIMIT 1`).Scan(&val)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("column should exist: %v", err)
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

	if err := store.RecordOutcome(context.Background(), "team-a:worker_a", now, true, policy); err != nil {
		t.Fatal(err)
	}
	open, _, err := store.IsBreakerOpen(context.Background(), "team-a:worker_a", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if open {
		t.Fatal("breaker should not be open after first failure")
	}

	if err := store.RecordOutcome(context.Background(), "team-a:worker_a", now.Add(2*time.Second), true, policy); err != nil {
		t.Fatal(err)
	}
	open, until, err := store.IsBreakerOpen(context.Background(), "team-a:worker_a", now.Add(3*time.Second))
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
