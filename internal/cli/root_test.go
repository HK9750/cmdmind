package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuggestAcceptsPositionalPrefixAndInterspersedFlags(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cmdmind.db")
	cwd := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	commands := [][]string{
		{"record", "--db", dbPath, "--cmd", "docker compose up -d", "--cwd", cwd, "--exit-code", "0", "--duration", "12"},
		{"record", "--db", dbPath, "--cmd", "go test ./...", "--cwd", cwd, "--exit-code", "0", "--duration-ms", "15"},
	}
	for _, args := range commands {
		if err := Run(args, &stdout, &stderr); err != nil {
			t.Fatalf("Run(%v) returned error: %v", args, err)
		}
	}

	stdout.Reset()
	if err := Run([]string{"suggest", "dock", "--db", dbPath, "--cwd", cwd}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(stdout.String())
	want := "docker compose up -d"
	if got != want {
		t.Fatalf("suggest output = %q, want %q", got, want)
	}
}

func TestReorderFlagsAllowsFlagsAfterPositionals(t *testing.T) {
	got := reorderFlags([]string{"dock", "--limit", "5", "--json"}, valueFlags("limit"))
	want := []string{"--limit", "5", "--json", "dock"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("reorderFlags() = %#v, want %#v", got, want)
	}
}
