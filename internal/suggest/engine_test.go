package suggest

import (
	"context"
	"testing"
	"time"

	"github.com/hasnain/cmdmind/internal/storage"
)

type fakeStore struct {
	candidates []storage.Candidate
}

func (f fakeStore) SuggestionCandidates(context.Context, string, int) ([]storage.Candidate, error) {
	return f.candidates, nil
}

func TestSuggestRanksSuccessfulProjectCommandFirst(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(fakeStore{candidates: []storage.Candidate{
		{
			CommandText:       "docker compose upp",
			NormalizedCommand: "docker compose upp",
			ProjectRoot:       "/repo",
			ProjectName:       "repo",
			LastCWD:           "/repo",
			GitBranch:         "main",
			UsedCount:         4,
			FailureCount:      4,
			LastUsedAt:        now.Add(-time.Minute),
		},
		{
			CommandText:       "docker compose up -d",
			NormalizedCommand: "docker compose up -d",
			ProjectRoot:       "/repo",
			ProjectName:       "repo",
			LastCWD:           "/repo",
			GitBranch:         "main",
			UsedCount:         8,
			SuccessCount:      8,
			LastUsedAt:        now.Add(-2 * time.Minute),
		},
		{
			CommandText:       "docker ps",
			NormalizedCommand: "docker ps",
			ProjectRoot:       "/other",
			ProjectName:       "other",
			LastCWD:           "/other",
			UsedCount:         20,
			SuccessCount:      20,
			LastUsedAt:        now,
		},
	}})

	got, err := engine.Suggest(context.Background(), Request{
		Prefix:    "dock",
		CWD:       "/repo",
		Project:   ProjectContext{RootPath: "/repo", Name: "repo"},
		GitBranch: "main",
		Limit:     3,
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected suggestions")
	}
	if got[0].CommandText != "docker compose up -d" {
		t.Fatalf("top suggestion = %q, want docker compose up -d", got[0].CommandText)
	}
}
