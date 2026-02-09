package runlog

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type RunStart struct {
	JobID     string
	JobKey    string
	Namespace string
	Command   string
	PID       int
	Started   time.Time
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
	JobKey     string
	Namespace  string
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
	jobKey := r.JobKey
	if jobKey == "" {
		jobKey = r.JobID
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO runs(job_id, job_key, namespace, started_at, finished_at, duration_ms, pid, exit_code, term_signal, status, failure_class, notes, command)
VALUES (?, ?, ?, ?, NULL, NULL, ?, NULL, NULL, 'running', NULL, NULL, ?)
`, r.JobID, jobKey, r.Namespace, r.Started.UTC().Format(time.RFC3339Nano), r.PID, r.Command)
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
INSERT INTO events(ts, job_id, job_key, namespace, level, event_type, message)
SELECT ?, job_id, job_key, namespace, 'error', 'run_failed', ? FROM runs WHERE id = ?
`, f.Finished.UTC().Format(time.RFC3339Nano), fmt.Sprintf("run failed: exit=%d signal=%s", f.ExitCode, f.TermSignal), f.ID)
	}

	return nil
}

func (s *Store) RecentFailures(ctx context.Context, namespace string, jobID string, limit int) ([]Failure, error) {
	if limit <= 0 {
		limit = 20
	}

	base := `
SELECT job_id, IFNULL(job_key, job_id), IFNULL(namespace, ''), started_at, IFNULL(finished_at, started_at), IFNULL(exit_code, -1), status, IFNULL(failure_class, ''), IFNULL(notes, '')
FROM runs
WHERE status = 'failed'`
	args := []any{}
	if namespace != "" {
		base += ` AND namespace = ?`
		args = append(args, namespace)
	}
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
		if err := rows.Scan(&f.JobID, &f.JobKey, &f.Namespace, &startedRaw, &finishedRaw, &f.ExitCode, &f.Status, &f.FailureCls, &f.Notes); err != nil {
			return nil, err
		}
		f.StartedAt, _ = time.Parse(time.RFC3339Nano, startedRaw)
		f.FinishedAt, _ = time.Parse(time.RFC3339Nano, finishedRaw)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) IsBreakerOpen(ctx context.Context, jobKey string, now time.Time) (bool, time.Time, error) {
	var openUntilRaw string
	err := s.db.QueryRowContext(ctx, `SELECT open_until FROM breaker_state WHERE job_key = ?`, jobKey).Scan(&openUntilRaw)
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

func (s *Store) RecordOutcome(ctx context.Context, jobKey string, now time.Time, failed bool, policy BreakerPolicy) error {
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
	row := tx.QueryRowContext(ctx, `SELECT failure_count, window_started_at, open_until FROM breaker_state WHERE job_key = ?`, jobKey)
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
INSERT INTO breaker_state(job_key, job_id, namespace, failure_count, window_started_at, open_until, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(job_key) DO UPDATE SET
	failure_count = excluded.failure_count,
	window_started_at = excluded.window_started_at,
	open_until = excluded.open_until,
	updated_at = excluded.updated_at
`, jobKey, splitJobKey(jobKey), splitNamespace(jobKey), st.count, windowFrom.UTC().Format(time.RFC3339Nano), openUntil.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			job_key TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
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
			job_key TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			level TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_job_started ON runs(job_id, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_job_key_started ON runs(job_key, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_namespace_started ON runs(namespace, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_status_started ON runs(status, started_at DESC);`,
		`CREATE TABLE IF NOT EXISTS breaker_state (
			job_key TEXT PRIMARY KEY,
			job_id TEXT NOT NULL DEFAULT '',
			namespace TEXT NOT NULL DEFAULT '',
			failure_count INTEGER NOT NULL,
			window_started_at TEXT NOT NULL,
			open_until TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_breaker_namespace_job ON breaker_state(namespace, job_id);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if err := s.ensureColumn(ctx, "runs", "job_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "runs", "namespace", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE runs SET job_key = job_id WHERE job_key = ''`); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := s.ensureColumn(ctx, "events", "job_key", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "events", "namespace", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.migrateBreakerState(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table string, col string, def string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer rows.Close()

	var (
		cid       int
		name      string
		colType   string
		notNull   int
		dfltValue sql.NullString
		pk        int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if name == col {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, col, def)); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func (s *Store) migrateBreakerState(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(breaker_state)`)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer rows.Close()

	hasJobKey := false
	var (
		cid       int
		name      string
		colType   string
		notNull   int
		dfltValue sql.NullString
		pk        int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		if name == "job_key" {
			hasJobKey = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if hasJobKey {
		return nil
	}

	stmts := []string{
		`CREATE TABLE breaker_state_v2 (
			job_key TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			namespace TEXT NOT NULL,
			failure_count INTEGER NOT NULL,
			window_started_at TEXT NOT NULL,
			open_until TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`INSERT INTO breaker_state_v2(job_key, job_id, namespace, failure_count, window_started_at, open_until, updated_at)
		 SELECT job_id, job_id, '', failure_count, window_started_at, open_until, updated_at FROM breaker_state;`,
		`DROP TABLE breaker_state;`,
		`ALTER TABLE breaker_state_v2 RENAME TO breaker_state;`,
		`CREATE INDEX IF NOT EXISTS idx_breaker_namespace_job ON breaker_state(namespace, job_id);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func splitNamespace(jobKey string) string {
	parts := strings.SplitN(jobKey, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func splitJobKey(jobKey string) string {
	parts := strings.SplitN(jobKey, ":", 2)
	if len(parts) != 2 {
		return jobKey
	}
	return parts[1]
}
