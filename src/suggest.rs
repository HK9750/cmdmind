use std::cmp::Ordering;
use std::path::{Path, PathBuf};

use anyhow::Result;
use chrono::{DateTime, Duration, Utc};
use serde::Serialize;

use crate::safety;
use crate::storage::{Candidate, Store};

#[derive(Debug, Clone)]
pub struct ProjectContext {
    pub root_path: String,
}

#[derive(Debug, Clone)]
pub struct Request {
    pub prefix: String,
    pub cwd: String,
    pub project: ProjectContext,
    pub git_branch: Option<String>,
    pub limit: usize,
    pub now: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize)]
pub struct Suggestion {
    pub command_text: String,
    pub score: i32,
    pub reason: String,
    pub last_used_at: DateTime<Utc>,
    pub used_count: i32,
    pub success_rate: f64,
    pub project_match: bool,
    pub cwd_match: bool,
    pub dangerous: bool,
}

pub fn suggest(store: &Store, req: Request) -> Result<Vec<Suggestion>> {
    let limit = if req.limit == 0 { 10 } else { req.limit };
    let raw_limit = (limit * 80).max(500);
    let candidates = store.suggestion_candidates(&req.prefix, raw_limit)?;
    let mut suggestions = candidates
        .into_iter()
        .filter_map(|candidate| rank(candidate, &req))
        .collect::<Vec<_>>();

    suggestions.sort_by(|a, b| {
        b.score
            .cmp(&a.score)
            .then_with(|| b.last_used_at.cmp(&a.last_used_at))
            .then_with(|| a.command_text.cmp(&b.command_text))
    });
    suggestions.truncate(limit);
    Ok(suggestions)
}

fn rank(candidate: Candidate, req: &Request) -> Option<Suggestion> {
    let prefix = normalize(&req.prefix);
    let cmd = normalize(&candidate.command_text);
    let mut reasons = Vec::new();
    let mut score = 0;
    let mut matched = prefix.is_empty();

    if !prefix.is_empty() {
        let s = prefix_score(&prefix, &cmd);
        if s > 0 {
            matched = true;
            score += s;
            if cmd.starts_with(&prefix) {
                reasons.push("prefix match".to_string());
            } else if token_prefix(&prefix, &cmd) {
                reasons.push("token match".to_string());
            } else if cmd.contains(&prefix) {
                reasons.push("contains match".to_string());
            } else {
                reasons.push("fuzzy match".to_string());
            }
        }
    } else {
        score += 5;
        reasons.push("recent command".to_string());
    }

    if !matched {
        return None;
    }

    let cwd_match =
        !clean(&req.cwd).as_os_str().is_empty() && clean(&req.cwd) == clean(&candidate.last_cwd);
    if cwd_match {
        score += 25;
        reasons.push("same directory".to_string());
    } else if related_path(&req.cwd, &candidate.last_cwd) {
        score += 10;
        reasons.push("nearby directory".to_string());
    }

    let project_match = !clean(&req.project.root_path).as_os_str().is_empty()
        && clean(&req.project.root_path) == clean(&candidate.project_root);
    if project_match {
        score += 40;
        reasons.push("same project".to_string());
    }

    if let (Some(current), Some(candidate_branch)) = (&req.git_branch, &candidate.git_branch) {
        if current == candidate_branch {
            score += 10;
            reasons.push("same branch".to_string());
        }
    }

    let frequency = ((candidate.used_count as f64).ln_1p() * 10.0).min(30.0) as i32;
    if frequency > 0 {
        score += frequency;
        reasons.push(format!("used {} times", candidate.used_count));
    }

    let recency = recency_score(req.now, candidate.last_used_at);
    if recency > 0 {
        score += recency;
        reasons.push("recent".to_string());
    }

    let success_rate = if candidate.used_count > 0 {
        candidate.success_count as f64 / candidate.used_count as f64
    } else {
        0.0
    };
    score += (success_rate * 25.0) as i32;
    if success_rate >= 0.8 {
        reasons.push("usually succeeds".to_string());
    }

    if candidate.used_count > 0 && candidate.failure_count > 0 {
        let failure_rate = candidate.failure_count as f64 / candidate.used_count as f64;
        score -= (failure_rate * 40.0) as i32;
        if failure_rate >= 0.5 {
            reasons.push("often failed".to_string());
        }
    }

    let dangerous = safety::is_dangerous(&candidate.command_text);
    if dangerous {
        score -= 50;
        reasons.push("dangerous".to_string());
    }

    Some(Suggestion {
        command_text: candidate.command_text,
        score,
        reason: reasons.join(", "),
        last_used_at: candidate.last_used_at,
        used_count: candidate.used_count,
        success_rate,
        project_match,
        cwd_match,
        dangerous,
    })
}

fn prefix_score(prefix: &str, cmd: &str) -> i32 {
    if cmd.starts_with(prefix) {
        60
    } else if token_prefix(prefix, cmd) {
        40
    } else if cmd.contains(prefix) {
        25
    } else if fuzzy_match(prefix, cmd) {
        10 + (20.0 * prefix.len() as f64 / cmd.len().max(1) as f64) as i32
    } else {
        0
    }
}

fn token_prefix(prefix: &str, cmd: &str) -> bool {
    cmd.split_whitespace()
        .any(|token| token.starts_with(prefix))
}

fn fuzzy_match(pattern: &str, text: &str) -> bool {
    let mut chars = pattern.chars();
    let mut current = chars.next();
    if current.is_none() {
        return true;
    }
    for ch in text.chars() {
        if Some(ch) == current {
            current = chars.next();
            if current.is_none() {
                return true;
            }
        }
    }
    false
}

fn recency_score(now: DateTime<Utc>, last: DateTime<Utc>) -> i32 {
    let age = now.signed_duration_since(last);
    match age.cmp(&Duration::zero()) {
        Ordering::Less => 25,
        Ordering::Equal | Ordering::Greater if age < Duration::days(1) => 25,
        Ordering::Equal | Ordering::Greater if age < Duration::days(7) => 18,
        Ordering::Equal | Ordering::Greater if age < Duration::days(30) => 10,
        Ordering::Equal | Ordering::Greater if age < Duration::days(180) => 4,
        _ => 0,
    }
}

fn related_path(a: &str, b: &str) -> bool {
    let a = clean(a);
    let b = clean(b);
    if a.as_os_str().is_empty() || b.as_os_str().is_empty() || a == b {
        return false;
    }
    is_prefix_path(&a, &b) || is_prefix_path(&b, &a)
}

fn is_prefix_path(parent: &Path, child: &Path) -> bool {
    child.starts_with(parent)
}

fn clean(path: &str) -> PathBuf {
    if path.is_empty() {
        return PathBuf::new();
    }
    let path = PathBuf::from(path);
    path.canonicalize().unwrap_or(path)
}

fn normalize(value: &str) -> String {
    value
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::storage::Candidate;

    #[test]
    fn ranks_successful_project_command_first() {
        let now = Utc::now();
        let req = Request {
            prefix: "dock".to_string(),
            cwd: "/repo".to_string(),
            project: ProjectContext {
                root_path: "/repo".to_string(),
            },
            git_branch: Some("main".to_string()),
            limit: 3,
            now,
        };

        let failed = Candidate {
            command_text: "docker compose upp".to_string(),
            project_root: "/repo".to_string(),
            last_cwd: "/repo".to_string(),
            git_branch: Some("main".to_string()),
            used_count: 4,
            success_count: 0,
            failure_count: 4,
            last_used_at: now,
        };
        let succeeded = Candidate {
            command_text: "docker compose up -d".to_string(),
            project_root: "/repo".to_string(),
            last_cwd: "/repo".to_string(),
            git_branch: Some("main".to_string()),
            used_count: 8,
            success_count: 8,
            failure_count: 0,
            last_used_at: now - Duration::minutes(1),
        };

        let mut ranked = [rank(failed, &req).unwrap(), rank(succeeded, &req).unwrap()];
        ranked.sort_by(|a, b| b.score.cmp(&a.score));
        assert_eq!(ranked[0].command_text, "docker compose up -d");
    }
}
