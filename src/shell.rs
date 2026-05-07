use std::env;
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::path::PathBuf;

use anyhow::{Context, Result};

use crate::storage::home_dir;

#[derive(Debug, Clone)]
pub struct InstallResult {
    pub script_path: PathBuf,
    pub bashrc_path: PathBuf,
    pub source_line: String,
    pub bashrc_updated: bool,
}

pub fn install(binary_path: &str, install_bashrc: bool) -> Result<InstallResult> {
    let script_path = default_script_path();
    if let Some(parent) = script_path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(&script_path, script(binary_path))
        .with_context(|| format!("write {}", script_path.display()))?;

    let bashrc_path = home_dir().join(".bashrc");
    let source_line = source_line(&script_path);
    let bashrc_updated = if install_bashrc {
        append_source_line(&bashrc_path, &source_line)?
    } else {
        false
    };

    Ok(InstallResult {
        script_path,
        bashrc_path,
        source_line,
        bashrc_updated,
    })
}

pub fn default_script_path() -> PathBuf {
    home_dir().join(".cmdmind").join("cmdmind.sh")
}

pub fn source_line(script_path: &PathBuf) -> String {
    format!("source {}", shell_quote(&script_path.to_string_lossy()))
}

pub fn script(binary_path: &str) -> String {
    let quoted = shell_quote(binary_path);
    include_str!("../scripts/cmdmind.sh")
        .replace("CMDMIND_BIN=cmdmind", &format!("CMDMIND_BIN={quoted}"))
}

pub fn resolve_binary_path(explicit: Option<&str>) -> String {
    if let Some(path) = explicit.filter(|s| !s.trim().is_empty()) {
        return path.to_string();
    }
    if let Ok(path) = env::var("CMDMIND_BIN") {
        if !path.trim().is_empty() {
            return path;
        }
    }
    if let Some(path) = find_in_path("cmdmind") {
        return path.to_string_lossy().to_string();
    }
    env::current_exe()
        .ok()
        .filter(|p| !p.to_string_lossy().contains("/target/"))
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|| "cmdmind".to_string())
}

fn append_source_line(path: &PathBuf, line: &str) -> Result<bool> {
    let content = fs::read_to_string(path).unwrap_or_default();
    if content.contains(line) || content.contains(".cmdmind/cmdmind.sh") {
        return Ok(false);
    }

    let mut file = OpenOptions::new().create(true).append(true).open(path)?;
    if !content.is_empty() && !content.ends_with('\n') {
        writeln!(file)?;
    }
    writeln!(file, "\n# CmdMind command memory\n{line}")?;
    Ok(true)
}

fn find_in_path(name: &str) -> Option<PathBuf> {
    let path = env::var_os("PATH")?;
    env::split_paths(&path)
        .map(|dir| dir.join(name))
        .find(|path| path.is_file())
}

fn shell_quote(value: &str) -> String {
    if value.is_empty() {
        return "''".to_string();
    }
    format!("'{}'", value.replace('\'', "'\\''"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generated_hook_does_not_use_picker() {
        let script = script("cmdmind");
        assert!(!script.contains("--picker"));
        assert!(script.contains("__cmdmind_bind_autosuggest_keys"));
        assert!(script.contains("CMDMIND_BIN='cmdmind'"));
    }
}
