package runlog

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/amterp/beagle/internal/core"
	_ "modernc.org/sqlite"
)

// schemaVersion identifies the current DB layout. A database whose
// user_version differs is treated as foreign (e.g. the pre-refactor
// namespaced schema) and wiped - beagle keeps no data worth migrating.
const schemaVersion = 1

type Store struct {
	db *sql.DB
}

type RunStart struct {
	JobID   string
	Command string
	PID     int
	Started time.Time
}

type RunFinish struct {
	ID         int64
	Finished   time.Time
	ExitCode   int
	TermSignal string
	Status     string
	FailureCls string
	Notes      string
}

type Failure struct {
	JobID      string
	StartedAt  time.Time
	FinishedAt time.Time
	ExitCode   int
	Status     string
	FailureCls string
	Notes      string
}

type BreakerPolicy struct {
	MaxFailures     int
	WindowSeconds   int
	CooldownSeconds int
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// WAL mode prevents "database is locked" when beagle-run and the CLI
	// access the database concurrently.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return core.RunlogDBPath(home), nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) StartRun(ctx context.Context, r RunStart) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO runs(job_id, started_at, finished_at, duration_ms, pid, exit_code, term_signal, status, failure_class, notes, command)
VALUES (?, ?, NULL, NULL, ?, NULL, NULL, 'running', NULL, NULL, ?)
`, r.JobID, r.Started.UTC().Format(time.RFC3339Nano), r.PID, r.Command)
	if err != nil {
		return 0, fmt.Errorf("insert run: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("run id: %w", err)
	}
	return id, nil
}

func (s *Store) FinishRun(ctx context.Context, f RunFinish) error {
	// Compute duration from the stored start time. If the clock has drifted
	// backwards (e.g. NTP correction) producing a negative duration, we
	// record 0 rather than a misleading negative value.
	durationMs := int64(0)
	if !f.Finished.IsZero() {
		var startedRaw string
		if err := s.db.QueryRowContext(ctx, `SELECT started_at FROM runs WHERE id = ?`, f.ID).Scan(&startedRaw); err == nil {
			if startedAt, parseErr := time.Parse(time.RFC3339Nano, startedRaw); parseErr == nil {
				d := f.Finished.Sub(startedAt)
				if d > 0 {
					durationMs = d.Milliseconds()
				}
			}
		}
	}

	_, err := s.db.ExecContext(ctx, `
UPDATE runs
SET finished_at = ?, duration_ms = ?, exit_code = ?, term_signal = ?, status = ?, failure_class = ?, notes = ?
WHERE id = ?
`, f.Finished.UTC().Format(time.RFC3339Nano), durationMs, f.ExitCode, f.TermSignal, f.Status, f.FailureCls, f.Notes, f.ID)
	if err != nil {
		return fmt.Errorf("update run: %w", err)
	}

	if f.Status == "failed" {
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO events(ts, job_id, level, event_type, message)
SELECT ?, job_id, 'error', 'run_failed', ? FROM runs WHERE id = ?
`, f.Finished.UTC().Format(time.RFC3339Nano), fmt.Sprintf("run failed: exit=%d signal=%s", f.ExitCode, f.TermSignal), f.ID); err != nil {
			return fmt.Errorf("record failure event: %w", err)
		}
	}

	return nil
}

// LastRun returns the most recent run's start time for a job, if any.
func (s *Store) LastRun(ctx context.Context, jobID string) (time.Time, bool, error) {
	var startedRaw string
	err := s.db.QueryRowContext(ctx,
		`SELECT started_at FROM runs WHERE job_id = ? ORDER BY started_at DESC LIMIT 1`, jobID).Scan(&startedRaw)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339Nano, startedRaw)
	if err != nil {
		return time.Time{}, false, nil
	}
	return t, true, nil
}

// RunSummary is the most recent run outcome for a job.
type RunSummary struct {
	JobID     string
	StartedAt time.Time
	Status    string
	ExitCode  int
}

// LastRunSummaries returns the latest run per job, newest run id wins.
func (s *Store) LastRunSummaries(ctx context.Context) ([]RunSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT job_id, started_at, status, IFNULL(exit_code, 0)
FROM runs
WHERE id IN (SELECT MAX(id) FROM runs GROUP BY job_id)
ORDER BY job_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RunSummary{}
	for rows.Next() {
		var rs RunSummary
		var startedRaw string
		if err := rows.Scan(&rs.JobID, &startedRaw, &rs.Status, &rs.ExitCode); err != nil {
			return nil, err
		}
		rs.StartedAt, _ = time.Parse(time.RFC3339Nano, startedRaw)
		out = append(out, rs)
	}
	return out, rows.Err()
}

func (s *Store) RecentFailures(ctx context.Context, jobID string, limit int) ([]Failure, error) {
	if limit <= 0 {
		limit = 20
	}

	base := `
SELECT job_id, started_at, IFNULL(finished_at, started_at), IFNULL(exit_code, -1), status, IFNULL(failure_class, ''), IFNULL(notes, '')
FROM runs
WHERE status = 'failed'`
	args := []any{}
	if jobID != "" {
		base += ` AND job_id = ?`
		args = append(args, jobID)
	}
	base += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Failure{}
	for rows.Next() {
		var f Failure
		var startedRaw, finishedRaw string
		if err := rows.Scan(&f.JobID, &startedRaw, &finishedRaw, &f.ExitCode, &f.Status, &f.FailureCls, &f.Notes); err != nil {
			return nil, err
		}
		f.StartedAt, _ = time.Parse(time.RFC3339Nano, startedRaw)
		f.FinishedAt, _ = time.Parse(time.RFC3339Nano, finishedRaw)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) IsBreakerOpen(ctx context.Context, jobID string, now time.Time) (bool, time.Time, error) {
	var openUntilRaw string
	err := s.db.QueryRowContext(ctx, `SELECT open_until FROM breaker_state WHERE job_id = ?`, jobID).Scan(&openUntilRaw)
	if err == sql.ErrNoRows {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	openUntil, err := time.Parse(time.RFC3339Nano, openUntilRaw)
	if err != nil {
		return false, time.Time{}, nil
	}
	return now.Before(openUntil), openUntil, nil
}

func (s *Store) RecordOutcome(ctx context.Context, jobID string, now time.Time, failed bool, policy BreakerPolicy) error {
	if policy.MaxFailures <= 0 || policy.WindowSeconds <= 0 || policy.CooldownSeconds <= 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	type state struct {
		count      int
		windowFrom string
		openUntil  string
	}
	var st state
	row := tx.QueryRowContext(ctx, `SELECT failure_count, window_started_at, open_until FROM breaker_state WHERE job_id = ?`, jobID)
	scanErr := row.Scan(&st.count, &st.windowFrom, &st.openUntil)
	if scanErr == sql.ErrNoRows {
		st = state{count: 0, windowFrom: now.UTC().Format(time.RFC3339Nano)}
	} else if scanErr != nil {
		return scanErr
	}

	windowFrom, _ := time.Parse(time.RFC3339Nano, st.windowFrom)
	if windowFrom.IsZero() {
		windowFrom = now
	}
	if now.Sub(windowFrom) > time.Duration(policy.WindowSeconds)*time.Second {
		st.count = 0
		windowFrom = now
	}

	if failed {
		st.count++
	}

	openUntil := time.Time{}
	if st.openUntil != "" {
		openUntil, _ = time.Parse(time.RFC3339Nano, st.openUntil)
	}
	if failed && st.count >= policy.MaxFailures {
		openUntil = now.Add(time.Duration(policy.CooldownSeconds) * time.Second)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO breaker_state(job_id, failure_count, window_started_at, open_until, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(job_id) DO UPDATE SET
	failure_count = excluded.failure_count,
	window_started_at = excluded.window_started_at,
	open_until = excluded.open_until,
	updated_at = excluded.updated_at
`, jobID, st.count, windowFrom.UTC().Format(time.RFC3339Nano), openUntil.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// GetScheduleFire returns the last-handled occurrence key for a schedule job.
// The key is an opaque string owned by the caller (the supervisor stores a
// wall-clock occurrence identity so DST fall-back fires exactly once).
func (s *Store) GetScheduleFire(ctx context.Context, jobID string) (string, bool, error) {
	var key string
	err := s.db.QueryRowContext(ctx, `SELECT last_fire FROM schedule_state WHERE job_id = ?`, jobID).Scan(&key)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return key, true, nil
}

// RecordScheduleFire records that an occurrence was handled. The supervisor
// calls this only after a successful kick, so a failed kick leaves the prior
// state and the next tick retries.
func (s *Store) RecordScheduleFire(ctx context.Context, jobID, occurrence string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO schedule_state(job_id, last_fire, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(job_id) DO UPDATE SET last_fire = excluded.last_fire, updated_at = excluded.updated_at
`, jobID, occurrence, now.UTC().Format(time.RFC3339Nano))
	return err
}

// SetMeta upserts a key/value with a timestamp. The supervisor heartbeat uses
// this so doctor can tell whether the scheduler is actually ticking.
func (s *Store) SetMeta(ctx context.Context, key, value string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO meta(key, value, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, key, value, now.UTC().Format(time.RFC3339Nano))
	return err
}

// GetMeta returns a meta value and when it was last written.
func (s *Store) GetMeta(ctx context.Context, key string) (value string, updatedAt time.Time, ok bool, err error) {
	var valRaw, tsRaw string
	scanErr := s.db.QueryRowContext(ctx, `SELECT value, updated_at FROM meta WHERE key = ?`, key).Scan(&valRaw, &tsRaw)
	if scanErr == sql.ErrNoRows {
		return "", time.Time{}, false, nil
	}
	if scanErr != nil {
		return "", time.Time{}, false, scanErr
	}
	ts, _ := time.Parse(time.RFC3339Nano, tsRaw)
	return valRaw, ts, true, nil
}

func (s *Store) migrate(ctx context.Context) error {
	var userVersion int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("migrate: read schema version: %w", err)
	}

	// A foreign schema (most importantly the pre-refactor namespaced layout)
	// is wiped rather than migrated. Dropping then creating sidesteps the
	// column-vs-index ordering hazards that bricked the old migrate path.
	if userVersion != schemaVersion {
		for _, table := range []string{"runs", "events", "breaker_state", "schedule_state"} {
			if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
				return fmt.Errorf("migrate: drop %s: %w", table, err)
			}
		}
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			duration_ms INTEGER,
			pid INTEGER,
			exit_code INTEGER,
			term_signal TEXT,
			status TEXT NOT NULL,
			failure_class TEXT,
			notes TEXT,
			command TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts TEXT NOT NULL,
			job_id TEXT NOT NULL,
			level TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS breaker_state (
			job_id TEXT PRIMARY KEY,
			failure_count INTEGER NOT NULL,
			window_started_at TEXT NOT NULL,
			open_until TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_job_started ON runs(job_id, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_status_started ON runs(status, started_at DESC);`,
		`CREATE TABLE IF NOT EXISTS schedule_state (
			job_id TEXT PRIMARY KEY,
			last_fire TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("migrate: set schema version: %w", err)
	}
	return nil
}
