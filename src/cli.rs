use std::fs;
use std::path::PathBuf;

use anyhow::{Context, Result, anyhow};
use chrono::Utc;
use clap::{Args, CommandFactory, Parser, Subcommand};

use crate::project;
use crate::recorder;
use crate::shell;
use crate::storage::{self, RecordInput, Store};
use crate::suggest::{self, ProjectContext, Request};

#[derive(Debug, Parser)]
#[command(
    name = "cmdmind",
    version,
    about = "CmdMind remembers terminal commands that worked in each project."
)]
struct Cli {
    #[command(subcommand)]
    command: Option<Command>,
}

#[derive(Debug, Subcommand)]
enum Command {
    Init(InitArgs),
    Record(RecordArgs),
    Suggest(SuggestArgs),
    Search(SearchArgs),
    Stats(StatsArgs),
    Doctor(DoctorArgs),
}

#[derive(Debug, Args)]
struct InitArgs {
    #[arg(long)]
    db: Option<PathBuf>,
    #[arg(long)]
    bin: Option<String>,
    #[arg(long)]
    install_bashrc: bool,
}

#[derive(Debug, Args)]
struct RecordArgs {
    #[arg(long)]
    cmd: String,
    #[arg(long)]
    cwd: Option<PathBuf>,
    #[arg(long)]
    exit_code: i32,
    #[arg(long)]
    duration: Option<i64>,
    #[arg(long)]
    duration_ms: Option<i64>,
    #[arg(long, default_value = "bash")]
    shell: String,
    #[arg(long)]
    hostname: Option<String>,
    #[arg(long)]
    db: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct SuggestArgs {
    #[arg(value_name = "PREFIX", num_args = 0..)]
    prefix_args: Vec<String>,
    #[arg(long)]
    prefix: Option<String>,
    #[arg(long)]
    cwd: Option<PathBuf>,
    #[arg(long, default_value_t = 10)]
    limit: usize,
    #[arg(long)]
    picker: bool,
    #[arg(long)]
    json: bool,
    #[arg(long)]
    verbose: bool,
    #[arg(long)]
    fast: bool,
    #[arg(long)]
    db: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct SearchArgs {
    #[arg(value_name = "QUERY", required = true, num_args = 1..)]
    query: Vec<String>,
    #[arg(long)]
    cwd: Option<PathBuf>,
    #[arg(long, default_value_t = 20)]
    limit: usize,
    #[arg(long)]
    json: bool,
    #[arg(long)]
    db: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct StatsArgs {
    #[arg(long, default_value_t = 10)]
    limit: usize,
    #[arg(long)]
    json: bool,
    #[arg(long)]
    db: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct DoctorArgs {
    #[arg(long)]
    db: Option<PathBuf>,
}

pub fn run() -> Result<()> {
    let cli = Cli::parse();
    match cli.command {
        Some(Command::Init(args)) => init(args),
        Some(Command::Record(args)) => record(args),
        Some(Command::Suggest(args)) => suggest(args),
        Some(Command::Search(args)) => search(args),
        Some(Command::Stats(args)) => stats(args),
        Some(Command::Doctor(args)) => doctor(args),
        None => {
            Cli::command().print_help()?;
            println!();
            Ok(())
        }
    }
}

fn init(args: InitArgs) -> Result<()> {
    let db_path = db_path(args.db);
    let store = Store::open(&db_path)?;
    store.migrate()?;

    let binary_path = shell::resolve_binary_path(args.bin.as_deref());
    let installed = shell::install(&binary_path, args.install_bashrc)?;

    println!("CmdMind initialized.");
    println!("Database: {}", db_path.display());
    println!("Bash integration: {}", installed.script_path.display());
    if installed.bashrc_updated {
        println!("Updated: {}", installed.bashrc_path.display());
    } else {
        println!("Add this to ~/.bashrc if it is not already there:");
        println!("{}", installed.source_line);
    }
    println!("Then reload Bash with: source ~/.bashrc");
    Ok(())
}

fn record(args: RecordArgs) -> Result<()> {
    if recorder::should_skip(&args.cmd) {
        return Ok(());
    }

    let normalized_command = recorder::normalize(&args.cmd);

    let cwd = match args.cwd {
        Some(path) => path,
        None => std::env::current_dir().context("current directory")?,
    };
    let detected = project::detect(&cwd);
    let duration_ms = args
        .duration_ms
        .or(args.duration)
        .filter(|value| *value > 0);
    let hostname = args.hostname.or_else(default_hostname);

    let input = RecordInput {
        command_text: normalized_command.clone(),
        normalized_command,
        cwd: cwd.to_string_lossy().to_string(),
        git_branch: detected.git_branch.clone(),
        project: detected,
        exit_code: args.exit_code,
        duration_ms,
        shell: args.shell,
        hostname,
        created_at: Utc::now(),
    };

    let mut store = Store::open(db_path(args.db))?;
    store.migrate()?;
    store.record(input)
}

fn suggest(args: SuggestArgs) -> Result<()> {
    let prefix = args.prefix.unwrap_or_else(|| args.prefix_args.join(" "));
    let cwd = args
        .cwd
        .unwrap_or(std::env::current_dir().context("current directory")?);
    let detected = if args.fast {
        project::detect_fast(&cwd)
    } else {
        project::detect(&cwd)
    };
    let store = Store::open(db_path(args.db))?;
    if args.fast {
        if !store.is_initialized()? {
            return Ok(());
        }
    } else {
        store.migrate()?;
    }

    let suggestions = suggest::suggest(
        &store,
        Request {
            prefix,
            cwd: cwd.to_string_lossy().to_string(),
            project: ProjectContext {
                root_path: detected.root_path,
            },
            git_branch: detected.git_branch,
            limit: args.limit,
            now: Utc::now(),
            fast: args.fast,
        },
    )?;

    if args.json {
        println!("{}", serde_json::to_string_pretty(&suggestions)?);
        return Ok(());
    }

    if args.picker {
        if let Some(top) = suggestions.first() {
            println!("{}", top.command_text);
        }
        return Ok(());
    }

    for suggestion in suggestions {
        if args.verbose {
            println!(
                "{}\t{}\t{}",
                suggestion.command_text, suggestion.score, suggestion.reason
            );
        } else {
            println!("{}", suggestion.command_text);
        }
    }
    Ok(())
}

fn search(args: SearchArgs) -> Result<()> {
    let query = args.query.join(" ");
    if query.trim().is_empty() {
        return Err(anyhow!("search requires a query"));
    }

    let store = Store::open(db_path(args.db))?;
    store.migrate()?;
    let project_id = if let Some(cwd) = args.cwd {
        let detected = project::detect(&cwd);
        store.project_id_by_root(&detected.root_path).ok()
    } else {
        None
    };

    let results = store.search_commands(&query, project_id, args.limit)?;
    if args.json {
        println!("{}", serde_json::to_string_pretty(&results)?);
        return Ok(());
    }

    for result in results {
        let status = if result.exit_code == 0 {
            "ok".to_string()
        } else {
            format!("exit {}", result.exit_code)
        };
        println!("{}", result.command_text);
        println!(
            "  {} | {} | {}",
            status,
            result.project_name,
            result.created_at.format("%Y-%m-%d %H:%M")
        );
    }
    Ok(())
}

fn stats(args: StatsArgs) -> Result<()> {
    let store = Store::open(db_path(args.db))?;
    store.migrate()?;
    let stats = store.top_stats(args.limit)?;

    if args.json {
        println!("{}", serde_json::to_string_pretty(&stats)?);
        return Ok(());
    }

    println!("Most used commands:");
    for (i, stat) in stats.iter().enumerate() {
        if stat.project_name.is_empty() {
            println!(
                "{}. {:<36} {} times",
                i + 1,
                stat.command_text,
                stat.used_count
            );
        } else {
            println!(
                "{}. {:<36} {} times  {}",
                i + 1,
                stat.command_text,
                stat.used_count,
                stat.project_name
            );
        }
    }
    Ok(())
}

fn doctor(args: DoctorArgs) -> Result<()> {
    let db_path = db_path(args.db);
    println!("CmdMind {}", env!("CARGO_PKG_VERSION"));
    println!("Database path: {}", db_path.display());

    match Store::open(&db_path).and_then(|store| store.migrate()) {
        Ok(()) => println!("Migrations: ok"),
        Err(err) => println!("Migrations: error: {err:#}"),
    }

    let script_path = shell::default_script_path();
    if script_path.exists() {
        println!("Bash integration: ok ({})", script_path.display());
    } else {
        println!("Bash integration: missing ({})", script_path.display());
    }

    let bashrc = storage::home_dir().join(".bashrc");
    if bashrc.exists() {
        println!(".bashrc: found");
    } else {
        println!(".bashrc: not found");
    }
    Ok(())
}

fn db_path(path: Option<PathBuf>) -> PathBuf {
    path.unwrap_or_else(storage::default_db_path)
}

fn default_hostname() -> Option<String> {
    if let Ok(hostname) = std::env::var("HOSTNAME") {
        if !hostname.trim().is_empty() {
            return Some(hostname);
        }
    }
    fs::read_to_string("/etc/hostname")
        .ok()
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty())
}
