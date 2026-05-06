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
