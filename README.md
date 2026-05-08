# CmdMind

CmdMind is a local-first Bash command memory and autocomplete tool. It records commands that ran in your terminal, remembers where they worked, and suggests the right command for the current project.

Tagline: Your terminal remembers what worked.

## MVP Features

- Capture Bash commands after execution
- Store command text, cwd, project, Git branch, exit code, duration, and timestamp in SQLite
- Search command history with `cmdmind search`
- Suggest commands by typed prefix with `cmdmind suggest`
- Rank suggestions by prefix match, project, directory, branch, frequency, recency, success rate, and safety
- Use `ble.sh` for smooth inline autosuggestions and a no-flicker plain Bash fallback

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

CmdMind has two Bash UI modes.

### Smooth Mode With `ble.sh`

For the best UI, install and source `ble.sh`, then source CmdMind after it:

```bash
source -- ~/.local/share/blesh/ble.sh --attach=none
source ~/.cmdmind/cmdmind.sh
[[ ! ${BLE_VERSION-} ]] || ble-attach
```

In this mode, CmdMind registers a native `ble.sh` auto-complete source. `ble.sh` owns line rendering, so suggestions do not flicker while you type.

Type a prefix:

```bash
dock
```

CmdMind shows the top suggestion as native `ble.sh` ghost text:

```text
dock|er compose up -d
```

The `|` above represents your cursor.

Keys:

- Type normally: `ble.sh` refreshes the top suggestion after a short debounce
- `Tab`: accept the CmdMind suggestion
- Right arrow / `Ctrl+F`: use the default `ble.sh` auto-complete accept behavior

### Plain Bash Fallback

Without `ble.sh`, CmdMind does not bind every printable key. That avoids Bash Readline repaint flicker.

- `Ctrl + Space`: show the top suggestion preview
- `Tab`: accept the preview

CmdMind inserts suggestions into the prompt. It never executes them automatically.

Autosuggest settings:

```bash
export CMDMIND_AUTOSUGGEST=0   # disable automatic inline suggestions
export CMDMIND_MIN_PREFIX=2    # require at least 2 typed characters
export CMDMIND_DEBOUNCE_MS=80  # ble.sh auto-complete delay
export CMDMIND_UI=plain        # force no-flicker plain Bash fallback
```

If you want pasted multiline command blocks to run like normal Bash instead of
showing ble.sh's `-- MULTILINE --` staging prompt, set these after sourcing
`ble.sh` and before `ble-attach`:

```bash
bleopt term_bracketed_paste_mode=
bleopt accept_line_threshold=-1
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
