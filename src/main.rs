mod cli;
mod project;
mod recorder;
mod safety;
mod shell;
mod storage;
mod suggest;

fn main() {
    if let Err(err) = cli::run() {
        eprintln!("cmdmind: {err:#}");
        std::process::exit(1);
    }
}
