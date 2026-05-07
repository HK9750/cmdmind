use std::path::{Path, PathBuf};
use std::process::Command;

#[derive(Debug, Clone, Default)]
pub struct ProjectInfo {
    pub root_path: String,
    pub name: String,
    pub git_remote: Option<String>,
    pub git_branch: Option<String>,
    pub language: Option<String>,
    pub framework: Option<String>,
}

pub fn detect(cwd: &Path) -> ProjectInfo {
    let cwd = cwd.canonicalize().unwrap_or_else(|_| cwd.to_path_buf());

    if let Some(root) = git_root(&cwd) {
        let (language, framework) = detect_stack(&root);
        return ProjectInfo {
            name: root
                .file_name()
                .and_then(|s| s.to_str())
                .unwrap_or("shell")
                .to_string(),
            root_path: root.to_string_lossy().to_string(),
            git_remote: git_value(&cwd, &["config", "--get", "remote.origin.url"]),
            git_branch: git_value(&cwd, &["rev-parse", "--abbrev-ref", "HEAD"]).map(|branch| {
                if branch == "HEAD" {
                    "detached".to_string()
                } else {
                    branch
                }
            }),
            language,
            framework,
        };
    }

    let root = marker_root(&cwd);
    let (language, framework) = detect_stack(&root);
    ProjectInfo {
        name: root
            .file_name()
            .and_then(|s| s.to_str())
            .unwrap_or("shell")
            .to_string(),
        root_path: root.to_string_lossy().to_string(),
        language,
        framework,
        ..ProjectInfo::default()
    }
}

fn git_root(cwd: &Path) -> Option<PathBuf> {
    git_value(cwd, &["rev-parse", "--show-toplevel"]).map(PathBuf::from)
}

fn git_value(cwd: &Path, args: &[&str]) -> Option<String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(cwd)
        .args(args)
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    let value = String::from_utf8_lossy(&output.stdout).trim().to_string();
    (!value.is_empty()).then_some(value)
}

fn marker_root(cwd: &Path) -> PathBuf {
    let markers = [
        "go.mod",
        "Cargo.toml",
        "package.json",
        "pyproject.toml",
        "requirements.txt",
        "docker-compose.yml",
        "compose.yml",
        "Makefile",
    ];

    let mut dir = cwd.to_path_buf();
    loop {
        if markers.iter().any(|marker| dir.join(marker).exists()) {
            return dir;
        }
        if !dir.pop() {
            return cwd.to_path_buf();
        }
    }
}

fn detect_stack(root: &Path) -> (Option<String>, Option<String>) {
    let language = if root.join("go.mod").exists() {
        Some("go".to_string())
    } else if root.join("Cargo.toml").exists() {
        Some("rust".to_string())
    } else if root.join("package.json").exists() {
        Some("javascript".to_string())
    } else if root.join("pyproject.toml").exists() || root.join("requirements.txt").exists() {
        Some("python".to_string())
    } else {
        None
    };

    let framework = if root.join("docker-compose.yml").exists() || root.join("compose.yml").exists()
    {
        Some("docker-compose".to_string())
    } else if root.join("next.config.js").exists()
        || root.join("next.config.mjs").exists()
        || root.join("next.config.ts").exists()
    {
        Some("nextjs".to_string())
    } else {
        None
    };

    (language, framework)
}
