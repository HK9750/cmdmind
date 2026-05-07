# CmdMind

CmdMind is a local-first Bash command memory and autocomplete tool. It records commands that ran in your terminal, remembers where they worked, and suggests the right command for the current project.

Tagline: Your terminal remembers what worked.

## MVP Features

- Capture Bash commands after execution
- Store command text, cwd, project, Git branch, exit code, duration, and timestamp in SQLite
- Search command history with `cmdmind search`
- Suggest commands by typed prefix with `cmdmind suggest`
- Rank suggestions by prefix match, project, directory, branch, frequency, recency, success rate, and safety
- Show an inline suggestion while you type and accept it with `Tab`

## Install From Source

```bash
cargo build --release
install -Dm755 target/release/cmdmind ~/.local/bin/cmdmind
cmdmind init --bin ~/.local/bin/cmdmind --install-bashrc
source ~/.bashrc
```

If `~/.local/bin` is not on your `PATH`, either add it or run the binary by absolute path.

## CLI

```bash
cmdmind init
cmdmind record --cmd "docker compose up -d" --cwd "$PWD" --exit-code 0 --duration 1200
cmdmind suggest dock
cmdmind suggest --prefix dock --cwd "$PWD"
cmdmind search redis
cmdmind stats
cmdmind doctor
```

If you run `init` from a temporary binary or `cargo run`, pass the installed binary path explicitly:

```bash
cmdmind init --bin ~/.local/bin/cmdmind --install-bashrc
```

## Bash UX

Type a prefix:

```bash
dock
```

CmdMind shows the top suggestion inline after the cursor:

```text
dock|er compose up -d
```

The `|` above represents your cursor. Bash Readline cannot reliably render only part of `READLINE_LINE` with lower opacity, so CmdMind uses the safe Bash-native approximation: the suggested suffix is visible after the cursor, removed before execution unless you accept it, and accepted with `Tab`.

Keys:

- Type normally: refresh the top suggestion
- `Tab`: accept the ghost suggestion
- `Ctrl + Space`: manually refresh the current suggestion

CmdMind inserts suggestions into the prompt. It never executes them automatically.

Autosuggest settings:

```bash
export CMDMIND_AUTOSUGGEST=0   # disable automatic inline suggestions
export CMDMIND_MIN_PREFIX=2    # require at least 2 typed characters
```

## Storage

By default, CmdMind stores its SQLite database at:

```text
~/.local/share/cmdmind/cmdmind.db
```

Override with:

```bash
export CMDMIND_DB=/path/to/cmdmind.db
```

## Privacy And Safety

- Local only by default
- No telemetry
- No cloud sync
- Secret-looking commands are skipped
- Dangerous commands are ranked lower
- Suggestions are inserted, not executed
