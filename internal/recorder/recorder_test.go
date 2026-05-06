package recorder

import "testing"

func TestNormalize(t *testing.T) {
	got := Normalize("  docker   compose   up -d  ")
	want := "docker compose up -d"
	if got != want {
		t.Fatalf("Normalize() = %q, want %q", got, want)
	}
}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{name: "empty", cmd: "  ", want: true},
		{name: "leading space", cmd: " export TOKEN=x", want: true},
		{name: "internal", cmd: "cmdmind stats", want: true},
		{name: "internal path", cmd: "/tmp/opencode/cmdmind stats", want: true},
		{name: "secret", cmd: "curl -H 'Authorization: Bearer abc' example.com", want: true},
		{name: "normal", cmd: "docker compose up -d", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldSkip(tt.cmd); got != tt.want {
				t.Fatalf("ShouldSkip(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
