package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testBin = "/opt/bin/ailog"

// foreignSettings is a settings.json full of other tools' hooks on the
// same events ailog uses, with shell metacharacters (`&&`, `>`, `<`),
// unicode, matchers, unusual key order and unrelated keys — everything
// a map round trip would reorder or re-escape.
const foreignSettings = `{
  "model": "opus",
  "permissions": {
    "allow": ["Bash(git status)"],
    "deny": []
  },
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo '{\"note\":\"self-check — ünïcode\"}' && true > /dev/null"
          }
        ]
      },
      {
        "matcher": "",
        "hooks": [
          { "type": "command", "command": "TOOLA_VERSION=1; if [ -z \"$TOOLA_PORT\" ]; then exit 0; fi; curl -s localhost:1 < /dev/null" }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": "toolb notify --event stop", "timeout": 5 }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [ { "type": "command", "command": "\"$HOME/.claude/hooks/guard\"" } ]
      }
    ]
  },
  "statusLine": { "type": "command", "command": "status && date" },
  "zLast": true
}
`

func claudeEvents() map[string]string { return knownTools["claude-code"].events }

// commandsOf returns every hook command per event.
func commandsOf(t *testing.T, doc []byte) map[string][]string {
	t.Helper()
	var s struct {
		Hooks map[string][]hookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(doc, &s); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, doc)
	}
	out := map[string][]string{}
	for ev, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				out[ev] = append(out[ev], h.Command)
			}
		}
	}
	return out
}

func TestPlanInstall_PureAdditionKeepsForeignBytes(t *testing.T) {
	before := []byte(foreignSettings)
	after, added, err := planClaudeInstall(before, testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, ",") != "Stop,UserPromptSubmit" {
		t.Fatalf("added = %v", added)
	}
	// Every original line survives, in order: the edit is a pure insertion.
	ops := diffOps(splitLines(string(before)), splitLines(string(after)))
	for _, op := range ops {
		if op.kind == '-' {
			t.Fatalf("install removed or changed a line: %q\n%s", op.line, after)
		}
	}
	cmds := commandsOf(t, after)
	if n := len(cmds["UserPromptSubmit"]); n != 3 {
		t.Fatalf("UserPromptSubmit has %d commands, want 3 (2 foreign + ailog): %v", n, cmds)
	}
	if n := len(cmds["Stop"]); n != 2 {
		t.Fatalf("Stop has %d commands, want 2: %v", n, cmds)
	}
	if got := cmds["UserPromptSubmit"][2]; got != testBin+" hook claude-code prompt" {
		t.Fatalf("ailog command = %q", got)
	}
	if bytes.Contains(after, []byte(`\`+"u0026")) {
		t.Fatal("shell metacharacters were HTML-escaped")
	}
}

func TestPlanInstall_Idempotent(t *testing.T) {
	once, _, err := planClaudeInstall([]byte(foreignSettings), testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	twice, added, err := planClaudeInstall(once, testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 || !bytes.Equal(once, twice) {
		t.Fatalf("second install changed the file (added %v)", added)
	}
	// A different binary path still counts as installed: no duplicate.
	other, added, err := planClaudeInstall(once, "/usr/local/bin/ailog", claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 || !bytes.Equal(once, other) {
		t.Fatalf("install from another path added %v", added)
	}
}

func TestPlanInstall_PartialAddsOnlyMissingEvent(t *testing.T) {
	doc := []byte(`{
  "hooks": {
    "Stop": [
      { "hooks": [ { "type": "command", "command": "/old/path/ailog hook claude-code stop" } ] }
    ]
  }
}
`)
	after, added, err := planClaudeInstall(doc, testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, ",") != "UserPromptSubmit" {
		t.Fatalf("added = %v", added)
	}
	if n := len(commandsOf(t, after)["Stop"]); n != 1 {
		t.Fatalf("Stop duplicated: %d", n)
	}
}

func TestPlanInstall_EmptyAndCompactFiles(t *testing.T) {
	for name, doc := range map[string]string{
		"missing":  "",
		"empty":    "{}",
		"compact":  `{"model":"x","hooks":{"Stop":[{"hooks":[{"type":"command","command":"a && b"}]}]}}`,
		"no-hooks": "{\n  \"model\": \"x\"\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			after, added, err := planClaudeInstall([]byte(doc), testBin, claudeEvents())
			if err != nil {
				t.Fatal(err)
			}
			if len(added) != 2 {
				t.Fatalf("added = %v", added)
			}
			cmds := commandsOf(t, after)
			if len(cmds["UserPromptSubmit"]) != 1 {
				t.Fatalf("cmds = %v\n%s", cmds, after)
			}
			if name == "compact" && (len(cmds["Stop"]) != 2 || !bytes.HasPrefix(after, []byte(`{"model":"x","hooks":{"Stop":[{"hooks":[{"type":"command","command":"a && b"}]}`))) {
				t.Fatalf("compact file not preserved:\n%s", after)
			}
		})
	}
}

func TestPlanInstall_RefusesUnexpectedShapes(t *testing.T) {
	for _, doc := range []string{
		`{"hooks": []}`,
		`{"hooks": {"Stop": "oops"}}`,
		`[1, 2]`,
		`{"hooks": {`,
	} {
		if _, _, err := planClaudeInstall([]byte(doc), testBin, claudeEvents()); err == nil {
			t.Fatalf("expected an error for %s", doc)
		}
	}
}

func TestPlanInstall_QuotesPathWithSpaces(t *testing.T) {
	bin := "/Users/me/My Tools/ailog"
	after, _, err := planClaudeInstall([]byte("{}"), bin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	got := commandsOf(t, after)["Stop"][0]
	if got != "'/Users/me/My Tools/ailog' hook claude-code stop" {
		t.Fatalf("command = %q", got)
	}
	if !isAilogCommandFor(got, "claude-code stop") {
		t.Fatal("quoted command not recognised as ailog's")
	}
}

func TestIsAilogCommandFor(t *testing.T) {
	cases := map[string]bool{
		"/usr/local/bin/ailog hook claude-code prompt":    true,
		"ailog hook claude-code prompt":                   true,
		`"/a b/ailog" hook claude-code prompt`:            true,
		"/usr/local/bin/ailog hook claude-code stop":      false, // other event
		"echo ailog hook claude-code prompt":              false, // mentions ailog
		"/opt/ailog-wrapper hook claude-code prompt":      false,
		"x=1; ailog hook claude-code prompt":              false,
		"/usr/local/bin/ailog hook claude-code prompt -v": false,
		"": false,
	}
	for cmd, want := range cases {
		if got := isAilogCommandFor(cmd, "claude-code prompt"); got != want {
			t.Errorf("isAilogCommandFor(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestPlanUninstall_RestoresOriginalBytes(t *testing.T) {
	before := []byte(foreignSettings)
	installed, _, err := planClaudeInstall(before, testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	after, removed, err := planClaudeUninstall(installed, testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("uninstall did not restore the original file:\n%s", after)
	}
	again, removed, err := planClaudeUninstall(after, testBin, claudeEvents())
	if err != nil || removed != 0 || !bytes.Equal(again, after) {
		t.Fatalf("second uninstall changed the file (removed %d, err %v)", removed, err)
	}
}

func TestPlanUninstall_OnlyAilogEntries(t *testing.T) {
	doc := []byte(`{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          { "type": "command", "command": "/x/ailog hook claude-code prompt" },
          { "type": "command", "command": "echo ailog hook claude-code prompt" }
        ]
      }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "/x/ailog hook claude-code stop" } ] }
    ],
    "Notification": [
      { "hooks": [ { "type": "command", "command": "/x/ailog hook claude-code prompt" } ] }
    ]
  },
  "keep": 1
}
`)
	after, removed, err := planClaudeUninstall(doc, testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d", removed)
	}
	cmds := commandsOf(t, after)
	if len(cmds["UserPromptSubmit"]) != 1 || cmds["UserPromptSubmit"][0] != "echo ailog hook claude-code prompt" {
		t.Fatalf("UserPromptSubmit = %v", cmds["UserPromptSubmit"])
	}
	if _, ok := cmds["Stop"]; ok {
		t.Fatal("empty Stop event kept")
	}
	if len(cmds["Notification"]) != 1 {
		t.Fatal("an event ailog does not own was touched")
	}
}

func TestPlanUninstall_DropsHooksKeyWhenOnlyAilog(t *testing.T) {
	before := []byte("{\n  \"model\": \"x\"\n}\n")
	installed, _, _ := planClaudeInstall(before, testBin, claudeEvents())
	after, _, err := planClaudeUninstall(installed, testBin, claudeEvents())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("got:\n%s", after)
	}
}

func TestWriteSettings_BackupAndModeAndSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "settings.json")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	backup, err := writeSettings(link, []byte("{}"), []byte(`{"a":1}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(backup); string(b) != "{}" {
		t.Fatalf("backup content = %q", b)
	}
	if !strings.HasPrefix(filepath.Base(backup), "settings.json.bak-") {
		t.Fatalf("backup name = %s", backup)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced by a regular file")
	}
	if b, _ := os.ReadFile(real); string(b) != `{"a":1}` {
		t.Fatalf("target content = %q", b)
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestBackupPath_Unique(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	first, err := backupPath(p, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := backupPath(p, now)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasSuffix(second, "-1") {
		t.Fatalf("first %s second %s", first, second)
	}
}
