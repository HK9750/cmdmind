package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Info struct {
	RootPath  string `json:"root_path"`
	Name      string `json:"name"`
	GitRemote string `json:"git_remote,omitempty"`
	GitBranch string `json:"git_branch,omitempty"`
	Language  string `json:"language,omitempty"`
	Framework string `json:"framework,omitempty"`
}

func Detect(ctx context.Context, cwd string) Info {
	if strings.TrimSpace(cwd) == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}

	if root, ok := gitRoot(ctx, cwd); ok {
		info := Info{
			RootPath:  root,
			Name:      filepath.Base(root),
			GitRemote: gitRemote(ctx, cwd),
			GitBranch: gitBranch(ctx, cwd),
		}
		info.Language, info.Framework = detectStack(root)
		return info
	}

	root := markerRoot(cwd)
	info := Info{RootPath: root, Name: filepath.Base(root)}
	if info.Name == "." || info.Name == string(filepath.Separator) || info.Name == "" {
		info.Name = "shell"
	}
	info.Language, info.Framework = detectStack(root)
	return info
}

func gitRoot(ctx context.Context, cwd string) (string, bool) {
	out, err := git(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(out)
	return root, root != ""
}

func gitBranch(ctx context.Context, cwd string) string {
	out, err := git(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(out)
	if branch == "HEAD" {
		return "detached"
	}
	return branch
}

func gitRemote(ctx context.Context, cwd string) string {
	out, err := git(ctx, cwd, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func git(parent context.Context, cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 700*time.Millisecond)
	defer cancel()
	fullArgs := append([]string{"-C", cwd}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func markerRoot(cwd string) string {
	markers := []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "requirements.txt", "docker-compose.yml", "compose.yml", "Makefile"}
	dir := cwd
	for {
		for _, marker := range markers {
			if exists(filepath.Join(dir, marker)) {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func detectStack(root string) (language, framework string) {
	switch {
	case exists(filepath.Join(root, "go.mod")):
		language = "go"
	case exists(filepath.Join(root, "package.json")):
		language = "javascript"
	case exists(filepath.Join(root, "Cargo.toml")):
		language = "rust"
	case exists(filepath.Join(root, "pyproject.toml")) || exists(filepath.Join(root, "requirements.txt")):
		language = "python"
	}

	switch {
	case exists(filepath.Join(root, "docker-compose.yml")) || exists(filepath.Join(root, "compose.yml")):
		framework = "docker-compose"
	case exists(filepath.Join(root, "next.config.js")) || exists(filepath.Join(root, "next.config.mjs")) || exists(filepath.Join(root, "next.config.ts")):
		framework = "nextjs"
	}
	return language, framework
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
