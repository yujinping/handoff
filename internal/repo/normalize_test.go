package repo

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"scp", "git@github.com:Owner/Repo.git", "github.com/owner/repo"},
		{"https with .git", "https://github.com/owner/repo.git", "github.com/owner/repo"},
		{"https trailing slash", "https://github.com/owner/repo/", "github.com/owner/repo"},
		{"ssh with port", "ssh://git@gitlab.com:2222/group/sub/repo.git", "gitlab.com/group/sub/repo"},
		{"credentials", "https://user:pass@example.com/A/B", "example.com/A/B"},
		{"uppercase scheme and host", "HTTPS://GitHub.com/O/R/", "github.com/o/r"},
		{"scp unknown host keeps case", "git@example.com:a/B.git", "example.com/a/B"},
		{"no scheme", "github.com/owner/repo", "github.com/owner/repo"},
		{"git protocol", "git://github.com/owner/repo.git", "github.com/owner/repo"},
		{"double slash", "https://github.com//owner//repo//", "github.com/owner/repo"},
		{"leading whitespace", "  https://github.com/owner/repo  ", "github.com/owner/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.in)
			if err != nil {
				t.Fatalf("Normalize(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"local absolute", "/Users/me/repo"},
		{"local relative", "./repo"},
		{"local parent", "../repo"},
		{"home", "~/repo"},
		{"file scheme", "file:///Users/me/repo"},
		{"no path", "https://github.com"},
		{"no path slash", "https://github.com/"},
		{"dotdot", "https://github.com/owner/../repo"},
		{"unsupported scheme", "ftp://example.com/a/b"},
		{"control char", "https://github.com/a/\x00b"},
		{"windows path", `C:\Users\me\repo`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Normalize(tt.in); err == nil {
				t.Fatalf("Normalize(%q) = %q, want error", tt.in, got)
			}
		})
	}
}
