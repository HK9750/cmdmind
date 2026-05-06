package storage

import (
	"context"
	"database/sql"
	"errors"
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

type NullableInt64 struct {
	Int64 int64 `json:"int64"`
	Valid bool  `json:"valid"`
}

type ProjectInput struct {
	RootPath  string
	Name      string
	GitRemote string
	Language  string
	Framework string
}

type RecordInput struct {
	CommandText       string
	NormalizedCommand string
	CWD               string
	Project           ProjectInput
	GitBranch         string
	ExitCode          int
	DurationMS        NullableInt64
	Shell             string
	Hostname          string
	CreatedAt         time.Time
}

type Candidate struct {
	CommandText       string    `json:"command_text"`
	NormalizedCommand string    `json:"normalized_command"`
	ProjectID         int64     `json:"project_id"`
	ProjectRoot       string    `json:"project_root"`
	ProjectName       string    `json:"project_name"`
	LastCWD           string    `json:"last_cwd"`
	GitBranch         string    `json:"git_branch"`
	UsedCount         int       `json:"used_count"`
	SuccessCount      int       `json:"success_count"`
	FailureCount      int       `json:"failure_count"`
	AvgDurationMS     int64     `json:"avg_duration_ms,omitempty"`
	LastUsedAt        time.Time `json:"last_used_at"`
}

type SearchRequest struct {
	Query     string
	ProjectID int64
	Limit     int
}

type SearchResult struct {
	CommandText string    `json:"command_text"`
	CWD         string    `json:"cwd"`
	ProjectName string    `json:"project_name"`
	ProjectRoot string    `json:"project_root"`
	GitBranch   string    `json:"git_branch"`
	ExitCode    int       `json:"exit_code"`
	DurationMS  int64     `json:"duration_ms,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type Stat struct {
	CommandText   string    `json:"command_text"`
	ProjectName   string    `json:"project_name"`
	ProjectRoot   string    `json:"project_root"`
	UsedCount     int       `json:"used_count"`
	SuccessCount  int       `json:"success_count"`
	FailureCount  int       `json:"failure_count"`
	LastUsedAt    time.Time `json:"last_used_at"`
	SuccessRate   float64   `json:"success_rate"`
	AvgDurationMS int64     `json:"avg_duration_ms,omitempty"`
}

func DefaultDBPath() string {
	if p := strings.TrimSpace(os.Getenv("CMDMIND_DB")); p != "" {
		return expandHome(p)
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(expandHome(xdg), "cmdmind", "cmdmind.db")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "cmdmind.db")
	}
	return filepath.Join(home, ".local", "share", "cmdmind", "cmdmind.db")
}

func Open(ctx context.Context, dbPath string) (*Store, error) {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = DefaultDBPath()
	}
	dbPath = expandHome(dbPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

func (s *Store) Record(ctx context.Context, input RecordInput) error {
	if strings.TrimSpace(input.CommandText) == "" {
		return errors.New("command text is required")
	}
	if strings.TrimSpace(input.NormalizedCommand) == "" {
		input.NormalizedCommand = strings.Join(strings.Fields(input.CommandText), " ")
	}
	if strings.TrimSpace(input.CWD) == "" {
		return errors.New("cwd is required")
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now()
	}
	if input.Shell == "" {
		input.Shell = "bash"
	}

	projectID, err := s.UpsertProject(ctx, input.Project)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	var duration any
	if input.DurationMS.Valid {
		duration = input.DurationMS.Int64
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO commands (
  command_text, normalized_command, cwd, project_id, git_branch, exit_code,
  duration_ms, shell, hostname, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, input.CommandText, input.NormalizedCommand, input.CWD, projectID, input.GitBranch, input.ExitCode, duration, input.Shell, input.Hostname, input.CreatedAt.UTC())
	if err != nil {
		return err
	}

	success := 0
	failure := 0
	if input.ExitCode == 0 {
		success = 1
	} else {
		failure = 1
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO command_stats (
  command_text, normalized_command, project_id, used_count, success_count,
  failure_count, avg_duration_ms, last_used_at, created_at, updated_at
) VALUES (?, ?, ?, 1, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(normalized_command, project_id) DO UPDATE SET
  command_text = excluded.command_text,
  used_count = command_stats.used_count + 1,
  success_count = command_stats.success_count + excluded.success_count,
  failure_count = command_stats.failure_count + excluded.failure_count,
  avg_duration_ms = CASE
    WHEN excluded.avg_duration_ms IS NULL THEN command_stats.avg_duration_ms
    WHEN command_stats.avg_duration_ms IS NULL THEN excluded.avg_duration_ms
    ELSE ((command_stats.avg_duration_ms * command_stats.used_count) + excluded.avg_duration_ms) / (command_stats.used_count + 1)
  END,
  last_used_at = excluded.last_used_at,
  updated_at = CURRENT_TIMESTAMP
`, input.CommandText, input.NormalizedCommand, projectID, success, failure, duration, input.CreatedAt.UTC())
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) UpsertProject(ctx context.Context, input ProjectInput) (int64, error) {
	root := strings.TrimSpace(input.RootPath)
	if root == "" {
		root = "."
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(root)
		if name == "." || name == string(filepath.Separator) {
			name = "shell"
		}
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO projects (root_path, name, git_remote, language, framework, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(root_path) DO UPDATE SET
  name = excluded.name,
  git_remote = excluded.git_remote,
  language = excluded.language,
  framework = excluded.framework,
  updated_at = CURRENT_TIMESTAMP
`, root, name, nullIfEmpty(input.GitRemote), nullIfEmpty(input.Language), nullIfEmpty(input.Framework))
	if err != nil {
		return 0, err
	}

	return s.ProjectIDByRoot(ctx, root)
}

func (s *Store) ProjectIDByRoot(ctx context.Context, root string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE root_path = ?`, root).Scan(&id)
	return id, err
}

func (s *Store) SuggestionCandidates(ctx context.Context, prefix string, limit int) ([]Candidate, error) {
	if limit <= 0 {
		limit = 500
	}
	like := "%" + strings.ToLower(strings.TrimSpace(prefix)) + "%"
	args := []any{limit}
	where := ""
	if strings.TrimSpace(prefix) != "" {
		where = "WHERE lower(cs.normalized_command) LIKE ? OR lower(cs.command_text) LIKE ?"
		args = []any{like, like, limit}
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT
  cs.command_text,
  cs.normalized_command,
  cs.project_id,
  p.root_path,
  p.name,
  COALESCE((
    SELECT c.cwd FROM commands c
    WHERE c.normalized_command = cs.normalized_command AND c.project_id = cs.project_id
    ORDER BY c.created_at DESC, c.id DESC LIMIT 1
  ), '') AS last_cwd,
  COALESCE((
    SELECT c.git_branch FROM commands c
    WHERE c.normalized_command = cs.normalized_command AND c.project_id = cs.project_id
    ORDER BY c.created_at DESC, c.id DESC LIMIT 1
  ), '') AS git_branch,
  cs.used_count,
  cs.success_count,
  cs.failure_count,
  COALESCE(cs.avg_duration_ms, 0),
  cs.last_used_at
FROM command_stats cs
JOIN projects p ON p.id = cs.project_id
`+where+`
ORDER BY cs.last_used_at DESC
LIMIT ?
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.CommandText, &c.NormalizedCommand, &c.ProjectID, &c.ProjectRoot, &c.ProjectName, &c.LastCWD, &c.GitBranch, &c.UsedCount, &c.SuccessCount, &c.FailureCount, &c.AvgDurationMS, &c.LastUsedAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

func (s *Store) SearchCommands(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(req.Query)))
	if len(terms) == 0 {
		return nil, nil
	}

	var where []string
	var args []any
	for _, term := range terms {
		where = append(where, `(lower(c.normalized_command) LIKE ? OR lower(c.cwd) LIKE ? OR lower(p.name) LIKE ? OR lower(p.root_path) LIKE ?)`)
		like := "%" + term + "%"
		args = append(args, like, like, like, like)
	}
	if req.ProjectID > 0 {
		where = append(where, `c.project_id = ?`)
		args = append(args, req.ProjectID)
	}
	args = append(args, req.Limit)

	rows, err := s.db.QueryContext(ctx, `
SELECT c.command_text, c.cwd, p.name, p.root_path, COALESCE(c.git_branch, ''), c.exit_code,
       COALESCE(c.duration_ms, 0), c.created_at
FROM commands c
JOIN projects p ON p.id = c.project_id
WHERE `+strings.Join(where, " AND ")+`
ORDER BY CASE WHEN c.exit_code = 0 THEN 0 ELSE 1 END, c.created_at DESC, c.id DESC
LIMIT ?
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.CommandText, &r.CWD, &r.ProjectName, &r.ProjectRoot, &r.GitBranch, &r.ExitCode, &r.DurationMS, &r.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *Store) TopStats(ctx context.Context, limit int) ([]Stat, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT cs.command_text, p.name, p.root_path, cs.used_count, cs.success_count, cs.failure_count,
       COALESCE(cs.avg_duration_ms, 0), cs.last_used_at
FROM command_stats cs
JOIN projects p ON p.id = cs.project_id
ORDER BY cs.used_count DESC, cs.last_used_at DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []Stat
	for rows.Next() {
		var s Stat
		if err := rows.Scan(&s.CommandText, &s.ProjectName, &s.ProjectRoot, &s.UsedCount, &s.SuccessCount, &s.FailureCount, &s.AvgDurationMS, &s.LastUsedAt); err != nil {
			return nil, err
		}
		if s.UsedCount > 0 {
			s.SuccessRate = float64(s.SuccessCount) / float64(s.UsedCount)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func expandHome(path string) string {
	if path == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
	}
	if strings.HasPrefix(path, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

const schemaSQL = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  root_path TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  git_remote TEXT,
  language TEXT,
  framework TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS commands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  command_text TEXT NOT NULL,
  normalized_command TEXT NOT NULL,
  cwd TEXT NOT NULL,
  project_id INTEGER NOT NULL,
  git_branch TEXT,
  exit_code INTEGER NOT NULL,
  duration_ms INTEGER,
  shell TEXT NOT NULL DEFAULT 'bash',
  hostname TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);

CREATE TABLE IF NOT EXISTS command_stats (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  command_text TEXT NOT NULL,
  normalized_command TEXT NOT NULL,
  project_id INTEGER NOT NULL,
  used_count INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  avg_duration_ms INTEGER,
  last_used_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(normalized_command, project_id),
  FOREIGN KEY (project_id) REFERENCES projects(id)
);

CREATE TABLE IF NOT EXISTS recipes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT,
  commands_json TEXT NOT NULL,
  project_id INTEGER,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (project_id) REFERENCES projects(id)
);

CREATE INDEX IF NOT EXISTS idx_commands_created_at ON commands(created_at);
CREATE INDEX IF NOT EXISTS idx_commands_normalized ON commands(normalized_command);
CREATE INDEX IF NOT EXISTS idx_commands_cwd ON commands(cwd);
CREATE INDEX IF NOT EXISTS idx_commands_project ON commands(project_id);
CREATE INDEX IF NOT EXISTS idx_stats_project ON command_stats(project_id);
CREATE INDEX IF NOT EXISTS idx_stats_last_used ON command_stats(last_used_at);
`
