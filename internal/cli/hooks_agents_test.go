package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tf "github.com/khanakia/ai-logger/internal/testfixtures"
)

// testBin is the command the installed hooks run in these tests: the
// test binary itself (os.Executable).
func testBin(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// claudeSettingsWith writes a Claude settings.json holding a foreign hook
// on each ailog event plus the given extra command hooks on them.
func claudeSettingsWith(t *testing.T, path string, prompt, stop []string) {
	t.Helper()
	group := func(cmds ...string) string {
		var hs []string
		for _, c := range cmds {
			b, _ := json.Marshal(c)
			hs = append(hs, `{"type": "command", "command": `+string(b)+`}`)
		}
		return `{"hooks": [` + strings.Join(hs, ", ") + `]}`
	}
	doc := `{
  "hooks": {
    "UserPromptSubmit": [` + group(append([]string{"runbay-hook prompt"}, prompt...)...) + `],
    "Stop": [` + group(append([]string{"emdash notify"}, stop...)...) + `]
  },
  "model": "opus"
}
`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHooksInstall_EveryClaudeHome(t *testing.T) {
	home := tf.Isolate(t)
	main := filepath.Join(home, ".claude/settings.json")
	bob := filepath.Join(home, ".claude-bob/settings.json")
	claudeSettingsWith(t, main, nil, nil)
	claudeSettingsWith(t, bob, nil, nil)
	out, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{main, bob} {
		got := read(t, p)
		for _, want := range []string{"runbay-hook prompt", "emdash notify", `"model": "opus"`,
			testBin(t) + " hook claude-code prompt", testBin(t) + " hook claude-code stop"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s lacks %q:\n%s", p, want, got)
			}
		}
		if !strings.Contains(out, "installed claude-code hooks → "+p) {
			t.Fatalf("output does not name %s:\n%s", p, out)
		}
	}

	// --home narrows to one home.
	out, _, err = runCLI(t, "", "hooks", "uninstall", "--tool", "claude-code", "--home", filepath.Join(home, ".claude-bob"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, bob), "hook claude-code") || !strings.Contains(read(t, main), "hook claude-code") {
		t.Fatalf("--home uninstall touched the wrong home:\n%s", out)
	}
}

// TestHooksInstall_UpgradesOldEntries: hook entries an older ailog wrote
// (same "hook claude-code <sub>" command line, another ailog binary) keep
// working until install runs; install then replaces them in place with
// this binary's, after a backup, and leaves every other hook alone.
func TestHooksInstall_UpgradesOldEntries(t *testing.T) {
	home := tf.Isolate(t)
	path := filepath.Join(home, ".claude/settings.json")
	claudeSettingsWith(t, path,
		[]string{"/Users/alice/go/bin/ailog hook claude-code prompt"},
		[]string{"'/Applications/My Tools/ailog' hook claude-code stop"})
	before := read(t, path)

	dry, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"replace UserPromptSubmit     /Users/alice/go/bin/ailog hook claude-code prompt",
		"replace Stop                 '/Applications/My Tools/ailog' hook claude-code stop"} {
		if !strings.Contains(dry, want) {
			t.Fatalf("dry run does not show %q:\n%s", want, dry)
		}
	}
	if strings.Contains(dry, "runbay-hook") || read(t, path) != before {
		t.Fatalf("dry run lists a foreign hook as replaced or wrote the file:\n%s", dry)
	}

	out, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if strings.Contains(got, "/Users/alice/go/bin/ailog") || strings.Contains(got, "My Tools") {
		t.Fatalf("old entries kept next to the new ones (double capture):\n%s", got)
	}
	for _, want := range []string{"runbay-hook prompt", "emdash notify", testBin(t) + " hook claude-code prompt", testBin(t) + " hook claude-code stop"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lacks %q:\n%s", want, got)
		}
	}
	if !strings.Contains(out, "replaced 2 older ailog hook entries") {
		t.Fatalf("output: %s", out)
	}
	bs := backups(t, path)
	if len(bs) == 0 || read(t, bs[0]) != before {
		t.Fatalf("no backup of the original: %v", bs)
	}
}

// TestHooksInstall_CurrentEntriesAreKept: entries already running this
// binary (the exact command line every ailog writes) are present, so
// install writes nothing.
func TestHooksInstall_CurrentEntriesAreKept(t *testing.T) {
	home := tf.Isolate(t)
	path := filepath.Join(home, ".claude/settings.json")
	claudeSettingsWith(t, path, []string{testBin(t) + " hook claude-code prompt"}, []string{testBin(t) + " hook claude-code stop"})
	before := read(t, path)
	out, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if read(t, path) != before || len(backups(t, path)) != 0 || !strings.Contains(out, "already installed") {
		t.Fatalf("install rewrote a file that already had the hooks:\n%s", out)
	}
}

func TestHooksInstall_CodexAndGemini(t *testing.T) {
	home := tf.Isolate(t)
	tf.Write(t, home, ".codex/config.toml", "model = \"gpt-test\"\n")
	tf.Write(t, home, ".gemini/settings.json", "{\n  \"theme\": \"dark\"\n}\n")

	out, _, err := runCLI(t, "", "hooks", "install", "--tool", "codex")
	if err != nil {
		t.Fatal(err)
	}
	hooksJSON := read(t, filepath.Join(home, ".codex/hooks.json"))
	for _, want := range []string{"UserPromptSubmit", "Stop", testBin(t) + " hook codex prompt", testBin(t) + " hook codex stop"} {
		if !strings.Contains(hooksJSON, want) {
			t.Fatalf("hooks.json lacks %q:\n%s", want, hooksJSON)
		}
	}
	if !strings.Contains(out, "manual step:") {
		t.Fatalf("codex manual steps not printed:\n%s", out)
	}
	if got := read(t, filepath.Join(home, ".codex/config.toml")); got != "model = \"gpt-test\"\n" {
		t.Fatalf("config.toml changed: %q", got)
	}

	if _, _, err := runCLI(t, "", "hooks", "install", "--tool", "gemini"); err != nil {
		t.Fatal(err)
	}
	gs := read(t, filepath.Join(home, ".gemini/settings.json"))
	for _, want := range []string{`"theme": "dark"`, "BeforeAgent", "AfterAgent", testBin(t) + " hook gemini prompt"} {
		if !strings.Contains(gs, want) {
			t.Fatalf("gemini settings lack %q:\n%s", want, gs)
		}
	}

	for _, tool := range []string{"codex", "gemini"} {
		if _, _, err := runCLI(t, "", "hooks", "uninstall", "--tool", tool); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(read(t, filepath.Join(home, ".codex/hooks.json")), "hook codex") ||
		strings.Contains(read(t, filepath.Join(home, ".gemini/settings.json")), "hook gemini") {
		t.Fatal("uninstall left ailog entries")
	}
}

func TestHooksInstall_RefusesAgentsWithoutHooks(t *testing.T) {
	tf.Isolate(t)
	for _, tool := range []string{"pi", "opencode", "cursor"} {
		if _, _, err := runCLI(t, "", "hooks", "install", "--tool", tool); err == nil {
			t.Errorf("%s: install did not fail", tool)
		}
	}
}

func TestHooksList_EveryAgent(t *testing.T) {
	tf.Isolate(t)
	out, _, err := runCLI(t, "", "hooks", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"claude-code\n  command hooks", "codex\n  command hooks", "gemini\n  command hooks",
		"pi\n  no command hooks", "opencode\n  no command hooks"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list lacks %q:\n%s", want, out)
		}
	}
}

func lastEntries(t *testing.T, n int) []map[string]any {
	t.Helper()
	out, _, err := runCLI(t, "", "last", fmt.Sprint(n), "--json")
	if err != nil {
		t.Fatal(err)
	}
	var es []map[string]any
	if err := json.Unmarshal([]byte(out), &es); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return es
}

// TestClaudeStopHook_ReadsFromCursor: each Stop reads only what the turn
// appended (the cursor is stored per transcript), and still finds the
// answer and its usage.
func TestClaudeStopHook_ReadsFromCursor(t *testing.T) {
	_ = isolatedHome(t)
	dir := t.TempDir()
	tr := tf.Write(t, dir, "-work-alice/"+tf.ClaudeSession+".jsonl", strings.Join(tf.ClaudeLines()[:3], "\n")+"\n")
	prompt := fmt.Sprintf(`{"session_id":%q,"cwd":"/tmp","prompt":"Explain goroutines briefly.","transcript_path":%q,"hook_event_name":"UserPromptSubmit"}`, tf.ClaudeSession, tr)
	stop := fmt.Sprintf(`{"session_id":%q,"transcript_path":%q,"hook_event_name":"Stop"}`, tf.ClaudeSession, tr)
	if _, _, err := runCLI(t, prompt, "hook", "claude-code", "prompt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, stop, "hook", "claude-code", "stop"); err != nil {
		t.Fatal(err)
	}
	e := lastEntries(t, 1)[0]
	if e["response"] != "Lightweight threads.\nScheduled by the runtime." || e["model"] != "claude-test" ||
		e["token_count_in"] != float64(12) || e["token_count_cache_read"] != float64(3) || e["tool_version"] != "2.1.300" {
		t.Fatalf("first turn = %v", e)
	}

	tf.Append(t, tr, `{"type":"assistant","uuid":"a-3","sessionId":"`+tf.ClaudeSession+`","timestamp":"2026-03-01T10:02:00.000Z","version":"2.1.300","message":{"role":"assistant","model":"claude-next","content":[{"type":"text","text":"Typed pipes."}],"usage":{"input_tokens":21,"output_tokens":4}}}`+"\n")
	prompt2 := strings.Replace(prompt, "Explain goroutines briefly.", "And channels?", 1)
	if _, _, err := runCLI(t, prompt2, "hook", "claude-code", "prompt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, stop, "hook", "claude-code", "stop"); err != nil {
		t.Fatal(err)
	}
	e = lastEntries(t, 1)[0]
	if e["prompt"] != "And channels?" || e["response"] != "Typed pipes." || e["model"] != "claude-next" || e["token_count_in"] != float64(21) {
		t.Fatalf("second turn = %v", e)
	}
}

func TestCodexHooks_PromptAndStop(t *testing.T) {
	_ = isolatedHome(t)
	home := t.TempDir()
	rollout := tf.Write(t, home, ".codex/"+tf.CodexRollout(tf.CodexSession)[len(".codex/"):], tf.CodexCurrent())
	prompt := fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":%q,"transcript_path":%q,"cwd":"/tmp","model":"gpt-test-1","permission_mode":"default","turn_id":"t1","prompt":"How do I list files?"}`, tf.CodexSession, rollout)
	stop := fmt.Sprintf(`{"hook_event_name":"Stop","session_id":%q,"transcript_path":%q,"cwd":"/tmp","model":"gpt-test-1","permission_mode":"default","turn_id":"t1","stop_hook_active":false,"last_assistant_message":"Use ls."}`, tf.CodexSession, rollout)
	if _, _, err := runCLI(t, prompt, "hook", "codex", "prompt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, stop, "hook", "codex", "stop"); err != nil {
		t.Fatal(err)
	}
	e := lastEntries(t, 1)[0]
	if e["tool"] != "codex" || e["prompt"] != "How do I list files?" || e["response"] != "Use ls." ||
		e["token_count_in"] != float64(200) || e["token_count_out"] != float64(27) || e["permission_mode"] != "default" {
		t.Fatalf("codex entry = %v", e)
	}
}

func TestGeminiHooks_PromptAndStop(t *testing.T) {
	_ = isolatedHome(t)
	prompt := `{"hook_event_name":"BeforeAgent","session_id":"g-1","transcript_path":"","cwd":"/tmp","timestamp":"2026-03-03T08:00:00Z","prompt":"Summarise the README."}`
	stop := `{"hook_event_name":"AfterAgent","session_id":"g-1","transcript_path":"","cwd":"/tmp","timestamp":"2026-03-03T08:00:05Z","prompt":"Summarise the README.","prompt_response":"It explains setup.","stop_hook_active":false}`
	if _, _, err := runCLI(t, prompt, "hook", "gemini", "prompt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, stop, "hook", "gemini", "stop"); err != nil {
		t.Fatal(err)
	}
	e := lastEntries(t, 1)[0]
	if e["tool"] != "gemini" || e["prompt"] != "Summarise the README." || e["response"] != "It explains setup." {
		t.Fatalf("gemini entry = %v", e)
	}
}

func TestAgentHook_BadPayload(t *testing.T) {
	_ = isolatedHome(t)
	if _, _, err := runCLI(t, "{not json", "hook", "claude-code", "prompt"); err == nil {
		t.Fatal("undecodable payload accepted")
	}
	if _, _, err := runCLI(t, "  \n", "hook", "codex", "stop"); err != nil {
		t.Fatalf("blank payload should be a no-op: %v", err)
	}
	if _, _, err := runCLI(t, `{"hook_event_name":"Stop","last_assistant_message":"x"}`, "hook", "claude-code", "stop"); err != nil {
		t.Fatalf("stop without a session should be a no-op: %v", err)
	}
	if len(lastEntries(t, 5)) != 0 {
		t.Fatal("a bad payload stored an entry")
	}
}
