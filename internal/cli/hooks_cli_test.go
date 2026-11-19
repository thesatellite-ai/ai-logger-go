package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedForeign writes a settings.json carrying other tools' hooks on
// ailog's events and returns its path and bytes.
func seedForeign(t *testing.T) (string, []byte) {
	t.Helper()
	claudeHome := t.TempDir()
	t.Setenv("AILOG_CLAUDE_HOME", claudeHome)
	path := filepath.Join(claudeHome, "settings.json")
	doc := []byte(`{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "toola prompt && echo ok > /dev/null"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "toolb stop"
          }
        ]
      }
    ]
  },
  "env": { "A": "1" }
}
`)
	if err := os.WriteFile(path, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, doc
}

func backups(t *testing.T, path string) []string {
	t.Helper()
	m, err := filepath.Glob(path + ".bak-*")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestHooksInstall_MergesBacksUpAndIsIdempotent(t *testing.T) {
	path, before := seedForeign(t)

	if _, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	for _, keep := range []string{"toola prompt && echo ok > /dev/null", "toolb stop", `"env": { "A": "1" }`} {
		if !strings.Contains(string(after), keep) {
			t.Fatalf("foreign content %q lost:\n%s", keep, after)
		}
	}
	if !strings.Contains(string(after), " hook claude-code prompt") || !strings.Contains(string(after), " hook claude-code stop") {
		t.Fatalf("ailog hooks missing:\n%s", after)
	}
	b := backups(t, path)
	if len(b) != 1 {
		t.Fatalf("want 1 backup, got %v", b)
	}
	if got, _ := os.ReadFile(b[0]); string(got) != string(before) {
		t.Fatal("backup does not hold the original bytes")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %v", fi.Mode().Perm())
	}

	out, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "already installed") {
		t.Fatalf("second run output: %q", out)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(after) || len(backups(t, path)) != 1 {
		t.Fatal("second install rewrote the file or made another backup")
	}

	if _, _, err := runCLI(t, "", "hooks", "uninstall", "--tool", "claude-code"); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(path)
	if string(restored) != string(before) {
		t.Fatalf("uninstall did not restore the original:\n%s", restored)
	}
}

func TestHooksInstall_DryRunWritesNothing(t *testing.T) {
	path, before := seedForeign(t)
	out, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+") || !strings.Contains(out, "dry run: would add Stop, UserPromptSubmit") {
		t.Fatalf("dry-run output: %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			t.Fatalf("dry run shows a removed line: %q", line)
		}
	}
	now, _ := os.ReadFile(path)
	if string(now) != string(before) || len(backups(t, path)) != 0 {
		t.Fatal("dry run modified the file or wrote a backup")
	}
}

func TestHooksInstall_InvalidJSONLeftUntouched(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("AILOG_CLAUDE_HOME", claudeHome)
	path := filepath.Join(claudeHome, "settings.json")
	broken := []byte(`{"hooks": {"Stop": [`)
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code"); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	now, _ := os.ReadFile(path)
	if string(now) != string(broken) {
		t.Fatal("invalid settings file was overwritten")
	}
}
