package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/khanakia/ai-logger/internal/config"
)

func TestHome_RespectsAILOG_HOME(t *testing.T) {
	t.Setenv("AILOG_HOME", "/custom/dir")
	got, err := config.Home()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/custom/dir" {
		t.Fatalf("got %q", got)
	}
}

func TestHome_DefaultsToUserHomeDotAilog(t *testing.T) {
	t.Setenv("AILOG_HOME", "")
	got, err := config.Home()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := os.UserHomeDir()
	if got != filepath.Join(u, ".ailog") {
		t.Fatalf("got %q", got)
	}
}

func TestDBPath_JoinsLogDB(t *testing.T) {
	t.Setenv("AILOG_HOME", "/x")
	got, err := config.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/x/log.db" {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureHome_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "a")
	t.Setenv("AILOG_HOME", dir)
	got, err := config.EnsureHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("got %q", got)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() {
		t.Fatal("expected dir")
	}
}

func TestClaudePaths_RespectAILOG_CLAUDE_HOME(t *testing.T) {
	t.Setenv("AILOG_CLAUDE_HOME", "/fake/claude")
	skills, err := config.ClaudeSkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	if skills != "/fake/claude/skills" {
		t.Fatalf("skills dir: %q", skills)
	}
	settings, err := config.ClaudeSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if settings != "/fake/claude/settings.json" {
		t.Fatalf("settings path: %q", settings)
	}
}

func TestClaudePaths_DefaultsToDotClaude(t *testing.T) {
	t.Setenv("AILOG_CLAUDE_HOME", "")
	u, _ := os.UserHomeDir()
	settings, err := config.ClaudeSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(u, ".claude", "settings.json")
	if settings != want {
		t.Fatalf("got %q want %q", settings, want)
	}
}
