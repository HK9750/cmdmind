const DANGEROUS_MARKERS: &[&str] = &[
    "rm -rf",
    "rm -fr",
    "sudo reboot",
    "sudo shutdown",
    "shutdown now",
    "docker system prune",
    "drop database",
    "truncate table",
    "mkfs",
    "dd if=",
    ":(){:|:&};:",
    "> /dev/sd",
];

pub fn is_dangerous(command: &str) -> bool {
    let lower = command
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase();
    DANGEROUS_MARKERS
        .iter()
        .any(|marker| lower.contains(marker))
}
