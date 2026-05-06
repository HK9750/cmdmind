package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordSuggestSearchAndStats(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "cmdmind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	input := RecordInput{
		CommandText:       "docker compose up -d",
		NormalizedCommand: "docker compose up -d",
		CWD:               "/repo",
		Project:           ProjectInput{RootPath: "/repo", Name: "repo", Language: "go"},
		GitBranch:         "main",
		ExitCode:          0,
		DurationMS:        NullableInt64{Int64: 1200, Valid: true},
		Shell:             "bash",
		Hostname:          "test-host",
		CreatedAt:         now,
	}

	if err := store.Record(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.CreatedAt = now.Add(time.Minute)
	if err := store.Record(ctx, input); err != nil {
		t.Fatal(err)
	}

	candidates, err := store.SuggestionCandidates(ctx, "dock", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}
	if candidates[0].UsedCount != 2 || candidates[0].SuccessCount != 2 {
		t.Fatalf("stats = used %d success %d, want used 2 success 2", candidates[0].UsedCount, candidates[0].SuccessCount)
	}

	results, err := store.SearchCommands(ctx, SearchRequest{Query: "compose", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	stats, err := store.TopStats(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].UsedCount != 2 || stats[0].SuccessRate != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
