use std::path::Path;

const SECRET_MARKERS: &[&str] = &[
    "password=",
    "passwd=",
    "token=",
    "api_key=",
    "apikey=",
    "secret=",
    "authorization:",
    "bearer ",
    "aws_secret_access_key",
    "github_token",
    "private_key",
];

const SKIPPED_PREFIXES: &[&str] = &["cmdmind", "history", "export HIST", "fc ", ":"];

pub fn normalize(command: &str) -> String {
    command.split_whitespace().collect::<Vec<_>>().join(" ")
}

pub fn should_skip(command: &str) -> bool {
    if command.is_empty() || command.starts_with(' ') || command.starts_with('\t') {
        return true;
    }

    let normalized = normalize(command);
    if normalized.is_empty() {
        return true;
    }

    if let Some(first) = normalized.split_whitespace().next() {
        if Path::new(first).file_name().and_then(|s| s.to_str()) == Some("cmdmind") {
            return true;
        }
    }

    let lower = normalized.to_lowercase();
    if SKIPPED_PREFIXES
        .iter()
        .any(|prefix| lower.starts_with(&prefix.to_lowercase()))
    {
        return true;
    }

    SECRET_MARKERS.iter().any(|marker| lower.contains(marker))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalizes_whitespace() {
        assert_eq!(
            normalize("  docker   compose   up -d  "),
            "docker compose up -d"
        );
    }

    #[test]
    fn skips_internal_and_secret_commands() {
        assert!(should_skip(""));
        assert!(should_skip(" export TOKEN=x"));
        assert!(should_skip("cmdmind stats"));
        assert!(should_skip("/tmp/opencode/cmdmind stats"));
        assert!(should_skip(
            "curl -H 'Authorization: Bearer abc' example.com"
        ));
        assert!(!should_skip("docker compose up -d"));
    }
}
