package runlog

import (
	"context"
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
