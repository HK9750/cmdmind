package suggest

import (
	"context"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hasnain/cmdmind/internal/safety"
	"github.com/hasnain/cmdmind/internal/storage"
)

type Store interface {
	SuggestionCandidates(ctx context.Context, prefix string, limit int) ([]storage.Candidate, error)
}

type Engine struct {
	store Store
}

type ProjectContext struct {
	RootPath string
	Name     string
}

type Request struct {
	Prefix    string
	CWD       string
	Project   ProjectContext
	GitBranch string
	Limit     int
	Now       time.Time
}

type Suggestion struct {
	CommandText  string    `json:"command_text"`
	Score        int       `json:"score"`
	Reason       string    `json:"reason"`
	LastUsedAt   time.Time `json:"last_used_at"`
	UsedCount    int       `json:"used_count"`
	SuccessRate  float64   `json:"success_rate"`
	ProjectMatch bool      `json:"project_match"`
	CWDMatch     bool      `json:"cwd_match"`
	Dangerous    bool      `json:"dangerous"`
}

func NewEngine(store Store) *Engine {
	return &Engine{store: store}
}

func (e *Engine) Suggest(ctx context.Context, req Request) ([]Suggestion, error) {
	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}

	rawLimit := req.Limit * 80
	if rawLimit < 500 {
		rawLimit = 500
	}
	candidates, err := e.store.SuggestionCandidates(ctx, req.Prefix, rawLimit)
	if err != nil {
		return nil, err
	}

	suggestions := make([]Suggestion, 0, len(candidates))
	for _, c := range candidates {
		s, matched := rank(c, req)
		if !matched {
			continue
		}
		suggestions = append(suggestions, s)
	}

	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Score == suggestions[j].Score {
			return suggestions[i].LastUsedAt.After(suggestions[j].LastUsedAt)
		}
		return suggestions[i].Score > suggestions[j].Score
	})

	if len(suggestions) > req.Limit {
		suggestions = suggestions[:req.Limit]
	}
	return suggestions, nil
}

func rank(c storage.Candidate, req Request) (Suggestion, bool) {
	prefix := normalize(req.Prefix)
	cmd := normalize(c.CommandText)
	reasons := make([]string, 0, 6)
	score := 0
	matched := prefix == ""

	if prefix != "" {
		s := prefixScore(prefix, cmd)
		if s > 0 {
			matched = true
			score += s
			switch {
			case strings.HasPrefix(cmd, prefix):
				reasons = append(reasons, "prefix match")
			case tokenPrefix(prefix, cmd):
				reasons = append(reasons, "token match")
			case strings.Contains(cmd, prefix):
				reasons = append(reasons, "contains match")
			default:
				reasons = append(reasons, "fuzzy match")
			}
		}
	} else {
		score += 5
		reasons = append(reasons, "recent command")
	}

	if !matched {
		return Suggestion{}, false
	}

	cwdMatch := clean(req.CWD) != "" && clean(req.CWD) == clean(c.LastCWD)
	if cwdMatch {
		score += 25
		reasons = append(reasons, "same directory")
	} else if relatedPath(req.CWD, c.LastCWD) {
		score += 10
		reasons = append(reasons, "nearby directory")
	}

	projectMatch := clean(req.Project.RootPath) != "" && clean(req.Project.RootPath) == clean(c.ProjectRoot)
	if projectMatch {
		score += 40
		reasons = append(reasons, "same project")
	}

	if req.GitBranch != "" && c.GitBranch != "" && req.GitBranch == c.GitBranch {
		score += 10
		reasons = append(reasons, "same branch")
	}

	freq := int(math.Min(30, math.Log1p(float64(c.UsedCount))*10))
	if freq > 0 {
		score += freq
		reasons = append(reasons, "used "+itoa(c.UsedCount)+" times")
	}

	recency := recencyScore(req.Now, c.LastUsedAt)
	if recency > 0 {
		score += recency
		reasons = append(reasons, "recent")
	}

	successRate := 0.0
	if c.UsedCount > 0 {
		successRate = float64(c.SuccessCount) / float64(c.UsedCount)
		successScore := int(successRate * 25)
		score += successScore
		if successRate >= 0.8 {
			reasons = append(reasons, "usually succeeds")
		}
	}

	if c.UsedCount > 0 && c.FailureCount > 0 {
		failureRate := float64(c.FailureCount) / float64(c.UsedCount)
		penalty := int(failureRate * 40)
		score -= penalty
		if failureRate >= 0.5 {
			reasons = append(reasons, "often failed")
		}
	}

	dangerous := safety.IsDangerous(c.CommandText)
	if dangerous {
		score -= 50
		reasons = append(reasons, "dangerous")
	}

	return Suggestion{
		CommandText:  c.CommandText,
		Score:        score,
		Reason:       strings.Join(reasons, ", "),
		LastUsedAt:   c.LastUsedAt,
		UsedCount:    c.UsedCount,
		SuccessRate:  successRate,
		ProjectMatch: projectMatch,
		CWDMatch:     cwdMatch,
		Dangerous:    dangerous,
	}, true
}

func prefixScore(prefix, cmd string) int {
	switch {
	case strings.HasPrefix(cmd, prefix):
		return 60
	case tokenPrefix(prefix, cmd):
		return 40
	case strings.Contains(cmd, prefix):
		return 25
	case fuzzyMatch(prefix, cmd):
		return 10 + int(20*float64(len(prefix))/float64(max(len(cmd), 1)))
	default:
		return 0
	}
}

func tokenPrefix(prefix, cmd string) bool {
	for _, token := range strings.Fields(cmd) {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

func fuzzyMatch(pattern, text string) bool {
	if pattern == "" {
		return true
	}
	i := 0
	for _, r := range text {
		if i < len(pattern) && byte(r) == pattern[i] {
			i++
			if i == len(pattern) {
				return true
			}
		}
	}
	return false
}

func recencyScore(now, last time.Time) int {
	if last.IsZero() {
		return 0
	}
	d := now.Sub(last)
	switch {
	case d < 24*time.Hour:
		return 25
	case d < 7*24*time.Hour:
		return 18
	case d < 30*24*time.Hour:
		return 10
	case d < 180*24*time.Hour:
		return 4
	default:
		return 0
	}
}

func relatedPath(a, b string) bool {
	a = clean(a)
	b = clean(b)
	if a == "" || b == "" || a == b {
		return false
	}
	rel, err := filepath.Rel(a, b)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		return true
	}
	rel, err = filepath.Rel(b, a)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func clean(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
