use std::env;
use std::fs;
use std::path::{Path, PathBuf};

use anyhow::{Context, Result, anyhow};
use chrono::{DateTime, NaiveDateTime, TimeZone, Utc};
use rusqlite::{Connection, OptionalExtension, params};
use serde::Serialize;

use crate::project::ProjectInfo;

#[derive(Debug, Clone)]
pub struct RecordInput {
    pub command_text: String,
    pub normalized_command: String,
    pub cwd: String,
    pub project: ProjectInfo,
    pub git_branch: Option<String>,
    pub exit_code: i32,
    pub duration_ms: Option<i64>,
    pub shell: String,
    pub hostname: Option<String>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone)]
pub struct Candidate {
    pub command_text: String,
    pub project_root: String,
    pub last_cwd: String,
    pub git_branch: Option<String>,
    pub used_count: i32,
    pub success_count: i32,
    pub failure_count: i32,
    pub last_used_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize)]
pub struct SearchResult {
    pub command_text: String,
    pub cwd: String,
    pub project_name: String,
    pub project_root: String,
    pub git_branch: Option<String>,
    pub exit_code: i32,
    pub duration_ms: Option<i64>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize)]
pub struct Stat {
    pub command_text: String,
    pub project_name: String,
    pub project_root: String,
    pub used_count: i32,
    pub success_count: i32,
    pub failure_count: i32,
    pub last_used_at: DateTime<Utc>,
    pub success_rate: f64,
    pub avg_duration_ms: Option<i64>,
}

pub struct Store {
    conn: Connection,
}

impl Store {
    pub fn open(path: impl AsRef<Path>) -> Result<Self> {
        let path = path.as_ref();
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
        }

        let conn = Connection::open(path).with_context(|| format!("open {}", path.display()))?;
        conn.pragma_update(None, "busy_timeout", 5000)?;
        conn.pragma_update(None, "journal_mode", "WAL")?;
        conn.pragma_update(None, "foreign_keys", "ON")?;
        Ok(Self { conn })
    }

    pub fn migrate(&self) -> Result<()> {
        self.conn.execute_batch(SCHEMA_SQL)?;
        Ok(())
    }

    pub fn is_initialized(&self) -> Result<bool> {
        let exists = self
            .conn
            .query_row(
                "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'command_stats' LIMIT 1",
                [],
                |_| Ok(()),
            )
            .optional()?
            .is_some();
        Ok(exists)
    }

    pub fn record(&mut self, input: RecordInput) -> Result<()> {
        if input.command_text.trim().is_empty() {
            return Err(anyhow!("command text is required"));
        }
        if input.cwd.trim().is_empty() {
            return Err(anyhow!("cwd is required"));
        }

        let project_id = self.upsert_project(&input.project)?;
        let tx = self.conn.transaction()?;
        let created_at = format_time(input.created_at);

        tx.execute(
            r#"
INSERT INTO commands (
  command_text, normalized_command, cwd, project_id, git_branch, exit_code,
  duration_ms, shell, hostname, created_at
) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)
"#,
            params![
                input.command_text,
                input.normalized_command,
                input.cwd,
                project_id,
                input.git_branch,
                input.exit_code,
                input.duration_ms,
                input.shell,
                input.hostname,
                created_at
            ],
        )?;

        let success = i32::from(input.exit_code == 0);
        let failure = i32::from(input.exit_code != 0);
        tx.execute(
            r#"
INSERT INTO command_stats (
  command_text, normalized_command, project_id, used_count, success_count,
  failure_count, avg_duration_ms, last_used_at, created_at, updated_at
) VALUES (?1, ?2, ?3, 1, ?4, ?5, ?6, ?7, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
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
"#,
            params![
                input.command_text,
                input.normalized_command,
                project_id,
                success,
                failure,
                input.duration_ms,
                created_at
            ],
        )?;

        tx.commit()?;
        Ok(())
    }

    pub fn upsert_project(&self, project: &ProjectInfo) -> Result<i64> {
        let root = if project.root_path.trim().is_empty() {
            ".".to_string()
        } else {
            project.root_path.clone()
        };
        let name = if project.name.trim().is_empty() {
            Path::new(&root)
                .file_name()
                .and_then(|s| s.to_str())
                .unwrap_or("shell")
                .to_string()
        } else {
            project.name.clone()
        };

        self.conn.execute(
            r#"
INSERT INTO projects (root_path, name, git_remote, language, framework, created_at, updated_at)
VALUES (?1, ?2, ?3, ?4, ?5, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(root_path) DO UPDATE SET
  name = excluded.name,
  git_remote = excluded.git_remote,
  language = excluded.language,
  framework = excluded.framework,
  updated_at = CURRENT_TIMESTAMP
"#,
            params![
                root,
                name,
                project.git_remote,
                project.language,
                project.framework
            ],
        )?;
        self.project_id_by_root(&root)
    }

    pub fn project_id_by_root(&self, root: &str) -> Result<i64> {
        self.conn
            .query_row(
                "SELECT id FROM projects WHERE root_path = ?1",
                params![root],
                |row| row.get(0),
            )
            .optional()?
            .ok_or_else(|| anyhow!("project not found: {root}"))
    }

    pub fn suggestion_candidates(&self, prefix: &str, limit: usize) -> Result<Vec<Candidate>> {
        let limit = if limit == 0 { 500 } else { limit };
        let trimmed = prefix.trim().to_lowercase();
        let (sql, like) = if trimmed.is_empty() {
            (CANDIDATES_SQL_NO_PREFIX.to_string(), None)
        } else {
            (
                CANDIDATES_SQL_WITH_PREFIX.to_string(),
                Some(format!("%{trimmed}%")),
            )
        };

        let mut stmt = self.conn.prepare(&sql)?;
        let rows = if let Some(like) = like {
            stmt.query_map(params![like, like, limit as i64], candidate_from_row)?
                .collect::<rusqlite::Result<Vec<_>>>()?
        } else {
            stmt.query_map(params![limit as i64], candidate_from_row)?
                .collect::<rusqlite::Result<Vec<_>>>()?
        };
        Ok(rows)
    }

    pub fn search_commands(
        &self,
        query: &str,
        project_id: Option<i64>,
        limit: usize,
    ) -> Result<Vec<SearchResult>> {
        let limit = if limit == 0 { 20 } else { limit };
        let terms: Vec<String> = query.split_whitespace().map(|s| s.to_lowercase()).collect();
        if terms.is_empty() {
            return Ok(Vec::new());
        }

        let mut where_parts = Vec::new();
        let mut values: Vec<rusqlite::types::Value> = Vec::new();
        for term in terms {
            where_parts.push("(lower(c.normalized_command) LIKE ? OR lower(c.cwd) LIKE ? OR lower(p.name) LIKE ? OR lower(p.root_path) LIKE ?)".to_string());
            let like = format!("%{term}%");
            for _ in 0..4 {
                values.push(like.clone().into());
            }
        }
        if let Some(project_id) = project_id {
            where_parts.push("c.project_id = ?".to_string());
            values.push(project_id.into());
        }
        values.push((limit as i64).into());

        let sql = format!(
            r#"
SELECT c.command_text, c.cwd, p.name, p.root_path, COALESCE(c.git_branch, ''), c.exit_code,
       c.duration_ms, c.created_at
FROM commands c
JOIN projects p ON p.id = c.project_id
WHERE {}
ORDER BY CASE WHEN c.exit_code = 0 THEN 0 ELSE 1 END, c.created_at DESC, c.id DESC
LIMIT ?
"#,
            where_parts.join(" AND ")
        );
        let mut stmt = self.conn.prepare(&sql)?;
        let rows = stmt
            .query_map(rusqlite::params_from_iter(values), |row| {
                let git_branch: String = row.get(4)?;
                let duration_ms: Option<i64> = row.get(6)?;
                let created_at: String = row.get(7)?;
                Ok(SearchResult {
                    command_text: row.get(0)?,
                    cwd: row.get(1)?,
                    project_name: row.get(2)?,
                    project_root: row.get(3)?,
                    git_branch: (!git_branch.is_empty()).then_some(git_branch),
                    exit_code: row.get(5)?,
                    duration_ms,
                    created_at: parse_time(&created_at),
                })
            })?
            .collect::<rusqlite::Result<Vec<_>>>()?;
        Ok(rows)
    }

    pub fn top_stats(&self, limit: usize) -> Result<Vec<Stat>> {
        let limit = if limit == 0 { 10 } else { limit };
        let mut stmt = self.conn.prepare(
            r#"
SELECT cs.command_text, p.name, p.root_path, cs.used_count, cs.success_count, cs.failure_count,
       cs.avg_duration_ms, cs.last_used_at
FROM command_stats cs
JOIN projects p ON p.id = cs.project_id
ORDER BY cs.used_count DESC, cs.last_used_at DESC
LIMIT ?1
"#,
        )?;
        let rows = stmt
            .query_map(params![limit as i64], |row| {
                let used_count: i32 = row.get(3)?;
                let success_count: i32 = row.get(4)?;
                let avg_duration_ms: Option<i64> = row.get(6)?;
                let last_used_at: String = row.get(7)?;
                Ok(Stat {
                    command_text: row.get(0)?,
                    project_name: row.get(1)?,
                    project_root: row.get(2)?,
                    used_count,
                    success_count,
                    failure_count: row.get(5)?,
                    last_used_at: parse_time(&last_used_at),
                    success_rate: if used_count > 0 {
                        success_count as f64 / used_count as f64
                    } else {
                        0.0
                    },
                    avg_duration_ms,
                })
            })?
            .collect::<rusqlite::Result<Vec<_>>>()?;
        Ok(rows)
    }
}

fn candidate_from_row(row: &rusqlite::Row<'_>) -> rusqlite::Result<Candidate> {
    let git_branch: String = row.get(6)?;
    let last_used_at: String = row.get(11)?;
    Ok(Candidate {
        command_text: row.get(0)?,
        project_root: row.get(3)?,
        last_cwd: row.get(5)?,
        git_branch: (!git_branch.is_empty()).then_some(git_branch),
        used_count: row.get(7)?,
        success_count: row.get(8)?,
        failure_count: row.get(9)?,
        last_used_at: parse_time(&last_used_at),
    })
}

pub fn default_db_path() -> PathBuf {
    if let Ok(path) = env::var("CMDMIND_DB") {
        let path = path.trim();
        if !path.is_empty() {
            return expand_home(path);
        }
    }
    if let Ok(path) = env::var("XDG_DATA_HOME") {
        let path = path.trim();
        if !path.is_empty() {
            return expand_home(path).join("cmdmind").join("cmdmind.db");
        }
    }
    home_dir()
        .join(".local")
        .join("share")
        .join("cmdmind")
        .join("cmdmind.db")
}

pub fn home_dir() -> PathBuf {
    env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
}

pub fn expand_home(path: &str) -> PathBuf {
    if path == "~" {
        return home_dir();
    }
    if let Some(rest) = path.strip_prefix("~/") {
        return home_dir().join(rest);
    }
    PathBuf::from(path)
}

fn format_time(time: DateTime<Utc>) -> String {
    time.format("%Y-%m-%d %H:%M:%S").to_string()
}

fn parse_time(value: &str) -> DateTime<Utc> {
    if let Ok(time) = DateTime::parse_from_rfc3339(value) {
        return time.with_timezone(&Utc);
    }
    if let Ok(time) = NaiveDateTime::parse_from_str(value, "%Y-%m-%d %H:%M:%S%.f") {
        return Utc.from_utc_datetime(&time);
    }
    Utc::now()
}

const CANDIDATES_SQL_NO_PREFIX: &str = r#"
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
ORDER BY cs.last_used_at DESC
LIMIT ?1
"#;

const CANDIDATES_SQL_WITH_PREFIX: &str = r#"
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
WHERE lower(cs.normalized_command) LIKE ?1 OR lower(cs.command_text) LIKE ?2
ORDER BY cs.last_used_at DESC
LIMIT ?3
"#;

const SCHEMA_SQL: &str = r#"
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
"#;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn records_searches_and_stats_commands() -> Result<()> {
        let dir = tempfile::tempdir()?;
        let db_path = dir.path().join("cmdmind.db");
        let mut store = Store::open(&db_path)?;
        store.migrate()?;

        let input = RecordInput {
            command_text: "docker compose up -d".to_string(),
            normalized_command: "docker compose up -d".to_string(),
            cwd: "/repo".to_string(),
            project: ProjectInfo {
                root_path: "/repo".to_string(),
                name: "repo".to_string(),
                language: Some("rust".to_string()),
                ..ProjectInfo::default()
            },
            git_branch: Some("main".to_string()),
            exit_code: 0,
            duration_ms: Some(1200),
            shell: "bash".to_string(),
            hostname: Some("test-host".to_string()),
            created_at: Utc::now(),
        };
        store.record(input.clone())?;
        store.record(input)?;

        let candidates = store.suggestion_candidates("dock", 10)?;
        assert_eq!(candidates.len(), 1);
        assert_eq!(candidates[0].used_count, 2);
        assert_eq!(candidates[0].success_count, 2);

        let results = store.search_commands("compose", None, 10)?;
        assert_eq!(results.len(), 2);

        let stats = store.top_stats(10)?;
        assert_eq!(stats.len(), 1);
        assert_eq!(stats[0].used_count, 2);
        assert_eq!(stats[0].success_rate, 1.0);
        Ok(())
    }
}
