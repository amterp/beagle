package runlog

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

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

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
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
	return filepath.Join(home, ".local", "share", "beagle", "beagle.db"), nil
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
		_, _ = s.db.ExecContext(ctx, `
INSERT INTO events(ts, job_id, level, event_type, message)
SELECT ?, job_id, 'error', 'run_failed', ? FROM runs WHERE id = ?
`, f.Finished.UTC().Format(time.RFC3339Nano), fmt.Sprintf("run failed: exit=%d signal=%s", f.ExitCode, f.TermSignal), f.ID)
	}

	return nil
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

func (s *Store) migrate(ctx context.Context) error {
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
		`CREATE INDEX IF NOT EXISTS idx_runs_job_started ON runs(job_id, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_status_started ON runs(status, started_at DESC);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
