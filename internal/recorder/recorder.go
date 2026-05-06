package recorder

import (
	"path/filepath"
	"strings"
)

var secretMarkers = []string{
	"password=",
	"passwd=",
	"token=",
	"api_key=",
	"apikey=",
	"secret=",
	"authorization:",
	"bearer ",
	"aws_secret_access_key",
	"github_token",
	"private_key",
}

var skippedPrefixes = []string{
	"cmdmind",
	"history",
	"export HIST",
	"fc ",
	":",
}

func Normalize(command string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
}

func ShouldSkip(command string) bool {
	if command == "" {
		return true
	}
	if strings.HasPrefix(command, " ") || strings.HasPrefix(command, "\t") {
		return true
	}
	normalized := Normalize(command)
	if normalized == "" {
		return true
	}
	fields := strings.Fields(normalized)
	if len(fields) > 0 && filepath.Base(fields[0]) == "cmdmind" {
		return true
	}
	for _, prefix := range skippedPrefixes {
		if strings.HasPrefix(strings.ToLower(normalized), strings.ToLower(prefix)) {
			return true
		}
	}
	lower := strings.ToLower(normalized)
	for _, marker := range secretMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
