package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hasnain/cmdmind/internal/project"
	"github.com/hasnain/cmdmind/internal/recorder"
	"github.com/hasnain/cmdmind/internal/shell"
	"github.com/hasnain/cmdmind/internal/storage"
	"github.com/hasnain/cmdmind/internal/suggest"
	"github.com/hasnain/cmdmind/internal/tui"
)

const version = "0.1.0"

func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	case "version", "--version", "-v":
		_, _ = fmt.Fprintf(stdout, "cmdmind %s\n", version)
		return nil
	case "init":
		return runInit(args[1:], stdout)
	case "record":
		return runRecord(args[1:])
	case "suggest":
		return runSuggest(args[1:], stdout, stderr)
	case "search":
		return runSearch(args[1:], stdout)
	case "stats":
		return runStats(args[1:], stdout)
	case "doctor":
		return runDoctor(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `CmdMind remembers terminal commands that worked in each project.

Usage:
  cmdmind init [--install-bashrc] [--bin /path/to/cmdmind]
  cmdmind record --cmd <command> --cwd <dir> --exit-code <code> [--duration-ms <ms>]
  cmdmind suggest <prefix> [--cwd <dir>] [--limit 10] [--picker] [--json]
  cmdmind search <query> [--cwd <dir>] [--limit 20] [--json]
  cmdmind stats [--limit 10] [--json]
  cmdmind doctor

Examples:
  cmdmind suggest dock
  cmdmind suggest --prefix dock --cwd "$PWD"
  cmdmind search redis
  cmdmind stats
`)
}

func runInit(args []string, stdout io.Writer) error {
	fs := newFlagSet("init")
	dbPath := fs.String("db", storage.DefaultDBPath(), "SQLite database path")
	binPath := fs.String("bin", "", "cmdmind binary path for Bash integration")
	installBashrc := fs.Bool("install-bashrc", false, "append CmdMind source line to ~/.bashrc")
	if err := fs.Parse(reorderFlags(args, valueFlags("db", "bin"))); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		return err
	}

	binaryPath := resolveBinaryPath(*binPath)

	installed, err := shell.Install(binaryPath, *installBashrc)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "CmdMind initialized.\n")
	_, _ = fmt.Fprintf(stdout, "Database: %s\n", *dbPath)
	_, _ = fmt.Fprintf(stdout, "Bash integration: %s\n", installed.ScriptPath)
	if installed.BashrcUpdated {
		_, _ = fmt.Fprintf(stdout, "Updated: %s\n", installed.BashrcPath)
	} else {
		_, _ = fmt.Fprintf(stdout, "Add this to ~/.bashrc if it is not already there:\n%s\n", installed.SourceLine)
	}
	_, _ = fmt.Fprintf(stdout, "Then reload Bash with: source ~/.bashrc\n")
	return nil
}

func runRecord(args []string) error {
	fs := newFlagSet("record")
	cmd := fs.String("cmd", "", "command text")
	cwd := fs.String("cwd", "", "current working directory")
	exitCode := fs.Int("exit-code", 0, "command exit code")
	duration := fs.Int64("duration", 0, "command duration in milliseconds")
	durationMS := fs.Int64("duration-ms", 0, "command duration in milliseconds")
	shellName := fs.String("shell", "bash", "shell name")
	hostname := fs.String("hostname", "", "hostname")
	dbPath := fs.String("db", storage.DefaultDBPath(), "SQLite database path")
	if err := fs.Parse(reorderFlags(args, valueFlags("cmd", "cwd", "exit-code", "duration", "duration-ms", "shell", "hostname", "db"))); err != nil {
		return err
	}
	if *durationMS == 0 && *duration > 0 {
		*durationMS = *duration
	}

	if strings.TrimSpace(*cmd) == "" {
		return errors.New("record requires --cmd")
	}

	if recorder.ShouldSkip(*cmd) {
		return nil
	}

	if strings.TrimSpace(*cwd) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		*cwd = wd
	}

	if *hostname == "" {
		if h, err := os.Hostname(); err == nil {
			*hostname = h
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		return err
	}

	detected := project.Detect(ctx, *cwd)
	input := storage.RecordInput{
		CommandText:       *cmd,
		NormalizedCommand: recorder.Normalize(*cmd),
		CWD:               *cwd,
		Project:           toStorageProject(detected),
		GitBranch:         detected.GitBranch,
		ExitCode:          *exitCode,
		DurationMS:        nullableDuration(*durationMS),
		Shell:             *shellName,
		Hostname:          *hostname,
		CreatedAt:         time.Now(),
	}

	return store.Record(ctx, input)
}

func runSuggest(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("suggest")
	prefix := fs.String("prefix", "", "typed prefix")
	cwd := fs.String("cwd", "", "current working directory")
	limit := fs.Int("limit", 10, "maximum suggestions")
	picker := fs.Bool("picker", false, "open interactive picker")
	jsonOut := fs.Bool("json", false, "print JSON")
	verbose := fs.Bool("verbose", false, "print score and reason")
	dbPath := fs.String("db", storage.DefaultDBPath(), "SQLite database path")
	if err := fs.Parse(reorderFlags(args, valueFlags("prefix", "cwd", "limit", "db"))); err != nil {
		return err
	}
	if strings.TrimSpace(*prefix) == "" && len(fs.Args()) > 0 {
		*prefix = strings.Join(fs.Args(), " ")
	}

	if *limit <= 0 {
		*limit = 10
	}

	if strings.TrimSpace(*cwd) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		*cwd = wd
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		return err
	}

	detected := project.Detect(ctx, *cwd)
	engine := suggest.NewEngine(store)
	suggestions, err := engine.Suggest(ctx, suggest.Request{
		Prefix:    *prefix,
		CWD:       *cwd,
		Project:   toSuggestProject(detected),
		GitBranch: detected.GitBranch,
		Limit:     *limit,
		Now:       time.Now(),
	})
	if err != nil {
		return err
	}

	if *picker {
		chosen, err := tui.Pick(stderr, suggestions)
		if err != nil {
			return err
		}
		if chosen != "" {
			_, _ = fmt.Fprintln(stdout, chosen)
		}
		return nil
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(suggestions)
	}

	for _, s := range suggestions {
		if *verbose {
			_, _ = fmt.Fprintf(stdout, "%s\t%d\t%s\n", s.CommandText, s.Score, s.Reason)
			continue
		}
		_, _ = fmt.Fprintln(stdout, s.CommandText)
	}

	return nil
}

func runSearch(args []string, stdout io.Writer) error {
	fs := newFlagSet("search")
	cwd := fs.String("cwd", "", "current working directory")
	limit := fs.Int("limit", 20, "maximum results")
	jsonOut := fs.Bool("json", false, "print JSON")
	dbPath := fs.String("db", storage.DefaultDBPath(), "SQLite database path")
	if err := fs.Parse(reorderFlags(args, valueFlags("cwd", "limit", "db"))); err != nil {
		return err
	}

	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return errors.New("search requires a query")
	}

	if *limit <= 0 {
		*limit = 20
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		return err
	}

	var projectID int64
	if strings.TrimSpace(*cwd) != "" {
		detected := project.Detect(ctx, *cwd)
		id, err := store.ProjectIDByRoot(ctx, detected.RootPath)
		if err == nil {
			projectID = id
		}
	}

	results, err := store.SearchCommands(ctx, storage.SearchRequest{Query: query, ProjectID: projectID, Limit: *limit})
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	for _, r := range results {
		status := "ok"
		if r.ExitCode != 0 {
			status = "exit " + strconv.Itoa(r.ExitCode)
		}
		_, _ = fmt.Fprintf(stdout, "%s\n  %s | %s | %s\n", r.CommandText, status, r.ProjectName, r.CreatedAt.Format("2006-01-02 15:04"))
	}

	return nil
}

func runStats(args []string, stdout io.Writer) error {
	fs := newFlagSet("stats")
	limit := fs.Int("limit", 10, "maximum results")
	jsonOut := fs.Bool("json", false, "print JSON")
	dbPath := fs.String("db", storage.DefaultDBPath(), "SQLite database path")
	if err := fs.Parse(reorderFlags(args, valueFlags("limit", "db"))); err != nil {
		return err
	}

	if *limit <= 0 {
		*limit = 10
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		return err
	}

	stats, err := store.TopStats(ctx, *limit)
	if err != nil {
		return err
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(stats)
	}

	_, _ = fmt.Fprintln(stdout, "Most used commands:")
	for i, s := range stats {
		_, _ = fmt.Fprintf(stdout, "%d. %-36s %d times", i+1, s.CommandText, s.UsedCount)
		if s.ProjectName != "" {
			_, _ = fmt.Fprintf(stdout, "  %s", s.ProjectName)
		}
		_, _ = fmt.Fprintln(stdout)
	}

	return nil
}

func runDoctor(args []string, stdout io.Writer) error {
	fs := newFlagSet("doctor")
	dbPath := fs.String("db", storage.DefaultDBPath(), "SQLite database path")
	if err := fs.Parse(reorderFlags(args, valueFlags("db"))); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = fmt.Fprintf(stdout, "CmdMind %s\n", version)
	_, _ = fmt.Fprintf(stdout, "Database path: %s\n", *dbPath)

	store, err := storage.Open(ctx, *dbPath)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "Database: error: %v\n", err)
		return nil
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		_, _ = fmt.Fprintf(stdout, "Migrations: error: %v\n", err)
	} else {
		_, _ = fmt.Fprintln(stdout, "Migrations: ok")
	}

	if _, err := os.Stat(shell.DefaultScriptPath()); err != nil {
		_, _ = fmt.Fprintf(stdout, "Bash integration: missing (%s)\n", shell.DefaultScriptPath())
	} else {
		_, _ = fmt.Fprintf(stdout, "Bash integration: ok (%s)\n", shell.DefaultScriptPath())
	}

	if _, err := os.Stat(filepath.Join(homeDir(), ".bashrc")); err != nil {
		_, _ = fmt.Fprintln(stdout, ".bashrc: not found")
	} else {
		_, _ = fmt.Fprintln(stdout, ".bashrc: found")
	}

	return nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func valueFlags(names ...string) map[string]bool {
	flags := make(map[string]bool, len(names))
	for _, name := range names {
		flags[name] = true
	}
	return flags
}

func reorderFlags(args []string, takesValue map[string]bool) []string {
	if len(args) == 0 {
		return args
	}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}

		flags = append(flags, arg)
		name := flagName(arg)
		if takesValue[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positionals...)
}

func flagName(arg string) string {
	arg = strings.TrimLeft(arg, "-")
	if before, _, ok := strings.Cut(arg, "="); ok {
		return before
	}
	return arg
}

func resolveBinaryPath(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if env := strings.TrimSpace(os.Getenv("CMDMIND_BIN")); env != "" {
		return env
	}
	if path, err := exec.LookPath("cmdmind"); err == nil && path != "" {
		return path
	}
	path, err := os.Executable()
	if err != nil || path == "" || strings.Contains(path, string(filepath.Separator)+"go-build") {
		return "cmdmind"
	}
	return path
}

func nullableDuration(ms int64) storage.NullableInt64 {
	if ms <= 0 {
		return storage.NullableInt64{}
	}
	return storage.NullableInt64{Int64: ms, Valid: true}
}

func toStorageProject(info project.Info) storage.ProjectInput {
	return storage.ProjectInput{
		RootPath:  info.RootPath,
		Name:      info.Name,
		GitRemote: info.GitRemote,
		Language:  info.Language,
		Framework: info.Framework,
	}
}

func toSuggestProject(info project.Info) suggest.ProjectContext {
	return suggest.ProjectContext{
		RootPath: info.RootPath,
		Name:     info.Name,
	}
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "."
}
