package config

import (
	"path/filepath"
	"testing"
)

func TestTokenFileDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HANDOFF_TOKEN_FILE", "")

	want := filepath.Join(home, ".handoff", "token")
	if got := TokenFile(); got != want {
		t.Fatalf("TokenFile() = %q，想要 %q", got, want)
	}
}

func TestTokenFileEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HANDOFF_TOKEN_FILE", "~/secrets/handoff-token")

	want := filepath.Join(home, "secrets", "handoff-token")
	if got := TokenFile(); got != want {
		t.Fatalf("TokenFile() = %q，想要 %q", got, want)
	}
}
