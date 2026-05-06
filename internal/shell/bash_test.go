package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashIntegrationSkipsIgnoredHistoryCommand(t *testing.T) {
	dir, logPath, scriptPath := writeBashTestFiles(t, `#!/usr/bin/env bash
if [ "$1" != "record" ]; then
  exit 0
fi
shift
cmd=""
exit_code=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --cmd) cmd="$2"; shift 2 ;;
    --exit-code) exit_code="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf '%s|%s\n' "$cmd" "$exit_code" >> "$CMDMIND_TEST_LOG"
`)

	runInteractiveBash(t, dir, logPath, "source "+scriptPath+"\ntrue\n false\nexit 0\n")

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(content))
	want := "true|0"
	if got != want {
		t.Fatalf("recorded commands = %q, want %q", got, want)
	}
}

func TestBashIntegrationSkipsIgnoredFirstCommandWithExistingHistory(t *testing.T) {
	dir, logPath, scriptPath := writeBashTestFiles(t, `#!/usr/bin/env bash
if [ "$1" != "record" ]; then
  exit 0
fi
shift
cmd=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --cmd) cmd="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf '%s\n' "$cmd" >> "$CMDMIND_TEST_LOG"
`)

	runInteractiveBash(t, dir, logPath, "history -s previous-command\nsource "+scriptPath+"\n false\nexit 0\n")

	content, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(content)) != "" {
		t.Fatalf("expected no recorded command, got %q", strings.TrimSpace(string(content)))
	}
}

func TestBashAutosuggestDrawsGhostAndAcceptsTopSuggestion(t *testing.T) {
	dir, logPath, scriptPath := writeBashTestFiles(t, `#!/usr/bin/env bash
if [ "$1" = "suggest" ]; then
  printf '%s\n' 'docker compose up -d'
  exit 0
fi
exit 0
`)

	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", `
source "$CMDMIND_TEST_SCRIPT"
READLINE_LINE=dock
READLINE_POINT=${#READLINE_LINE}
__cmdmind_autosuggest >/dev/null 2>/dev/null
printf '%s|%s|' "$READLINE_LINE" "$__CMDMIND_GHOST_SUFFIX"
__cmdmind_accept >/dev/null 2>/dev/null
printf '%s|%s' "$READLINE_LINE" "$__CMDMIND_TAB_BOUND"
`)
	cmd.Env = append(os.Environ(),
		"HOME="+dir,
		"HISTFILE="+filepath.Join(dir, "history"),
		"CMDMIND_TEST_LOG="+logPath,
		"CMDMIND_TEST_SCRIPT="+scriptPath,
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(string(output))
	want := "docker compose up -d|er compose up -d|docker compose up -d|0"
	if got != want {
		t.Fatalf("autosuggest state = %q, want %q", got, want)
	}
}

func TestBashSuggestionHookDoesNotUsePicker(t *testing.T) {
	script := Script("cmdmind")
	if strings.Contains(script, "--picker") {
		t.Fatal("Bash hook must not use the blocking picker")
	}
	if !strings.Contains(script, "__cmdmind_bind_autosuggest_keys") {
		t.Fatal("Bash hook must bind autosuggest keys")
	}
}

func writeBashTestFiles(t *testing.T, fakeBinContents string) (dir, logPath, scriptPath string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}

	dir = t.TempDir()
	logPath = filepath.Join(dir, "record.log")
	fakeBin := filepath.Join(dir, "cmdmind")
	scriptPath = filepath.Join(dir, "cmdmind.sh")

	if err := os.WriteFile(fakeBin, []byte(fakeBinContents), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(Script(fakeBin)), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, logPath, scriptPath
}

func runInteractiveBash(t *testing.T, dir, logPath, input string) {
	t.Helper()
	cmd := exec.Command("bash", "--noprofile", "--norc", "-i")
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(),
		"HOME="+dir,
		"HISTFILE="+filepath.Join(dir, "history"),
		"HISTCONTROL=ignorespace",
		"CMDMIND_TEST_LOG="+logPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash integration failed: %v\n%s", err, output)
	}
}
