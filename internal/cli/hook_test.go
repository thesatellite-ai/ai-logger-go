package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/khanakia/ai-logger/internal/cli"
	"github.com/khanakia/ai-logger/internal/store"
)

// runCLI invokes a fresh cobra root with stdin/stdout/stderr + args,
// isolating each test to its own AILOG_HOME temp dir.
func runCLI(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cli.NewRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	if stdin != "" {
		// Redirect os.Stdin for the subcommand's io.ReadAll(os.Stdin).
		r, w, _ := os.Pipe()
		origStdin := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = origStdin }()
		go func() {
			_, _ = io.WriteString(w, stdin)
			_ = w.Close()
		}()
	}
	err = root.ExecuteContext(context.Background())
	return outBuf.String(), errBuf.String(), err
}

func isolatedHome(t *testing.T) (dbPath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AILOG_HOME", home)
	t.Setenv("AILOG_HOOK_DEBUG", "0")
	// Initialize the DB so openStore in hooks finds it.
	_, _, err := runCLI(t, "", "init")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	return filepath.Join(home, "log.db")
}

func TestClaudeCodePromptHook_StoresEntry(t *testing.T) {
	_ = isolatedHome(t)
	payload := map[string]any{
		"session_id":      "abc-session-1",
		"transcript_path": "/tmp/not-a-real-transcript.jsonl",
		"cwd":             "/tmp",
		"prompt":          "what is a goroutine",
	}
	raw, _ := json.Marshal(payload)
	if _, _, err := runCLI(t, string(raw), "hook", "claude-code", "prompt"); err != nil {
		t.Fatalf("prompt hook: %v", err)
	}
	out, _, err := runCLI(t, "", "last", "1", "--json")
	if err != nil {
		t.Fatalf("last: %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("decode last: %v; output was %q", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e["tool"] != "claude-code" {
		t.Fatalf("tool: got %v", e["tool"])
	}
	if e["session_id"] != "abc-session-1" {
		t.Fatalf("session: got %v", e["session_id"])
	}
	if e["prompt"] != "what is a goroutine" {
		t.Fatalf("prompt: got %v", e["prompt"])
	}
}

func TestClaudeCodeStopHook_AttachesInlineResponse(t *testing.T) {
	_ = isolatedHome(t)
	// First insert a prompt.
	prompt := `{"session_id":"s1","cwd":"/tmp","prompt":"q1","transcript_path":"/tmp/missing"}`
	if _, _, err := runCLI(t, prompt, "hook", "claude-code", "prompt"); err != nil {
		t.Fatal(err)
	}
	// Then fire Stop with inline last_assistant_message (no transcript needed).
	stop := `{"session_id":"s1","last_assistant_message":"answer one"}`
	if _, _, err := runCLI(t, stop, "hook", "claude-code", "stop"); err != nil {
		t.Fatal(err)
	}
	out, _, err := runCLI(t, "", "last", "1", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	_ = json.Unmarshal([]byte(out), &entries)
	if len(entries) != 1 || entries[0]["response"] != "answer one" {
		t.Fatalf("response not attached: %+v", entries)
	}
}

func TestClaudeCodeStopHook_ReadsTranscriptWhenInlineMissing(t *testing.T) {
	_ = isolatedHome(t)
	// Build a real transcript file with one assistant line.
	tr := filepath.Join(t.TempDir(), "t.jsonl")
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"from transcript"}],"model":"claude-opus-4-7"}}`
	if err := os.WriteFile(tr, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := fmt.Sprintf(`{"session_id":"s2","cwd":"/tmp","prompt":"q2","transcript_path":%q}`, tr)
	if _, _, err := runCLI(t, prompt, "hook", "claude-code", "prompt"); err != nil {
		t.Fatal(err)
	}
	stop := fmt.Sprintf(`{"session_id":"s2","transcript_path":%q}`, tr)
	if _, _, err := runCLI(t, stop, "hook", "claude-code", "stop"); err != nil {
		t.Fatal(err)
	}
	out, _, _ := runCLI(t, "", "last", "1", "--json")
	var entries []map[string]any
	_ = json.Unmarshal([]byte(out), &entries)
	if entries[0]["response"] != "from transcript" {
		t.Fatalf("transcript fallback failed: %+v", entries[0])
	}
	if entries[0]["model"] != "claude-opus-4-7" {
		t.Fatalf("model from transcript not captured: %+v", entries[0])
	}
}

func TestGenericHook_StoresPrompt(t *testing.T) {
	_ = isolatedHome(t)
	raw := `{"tool":"mycli","session_id":"s3","prompt":"hello world"}`
	if _, _, err := runCLI(t, raw, "hook", "generic"); err != nil {
		t.Fatal(err)
	}
	out, _, _ := runCLI(t, "", "last", "1", "--json")
	var entries []map[string]any
	_ = json.Unmarshal([]byte(out), &entries)
	if entries[0]["tool"] != "mycli" || entries[0]["prompt"] != "hello world" {
		t.Fatalf("generic prompt: %+v", entries[0])
	}
}

func TestGenericHook_ToolFlagOverride(t *testing.T) {
	_ = isolatedHome(t)
	raw := `{"tool":"ignored","session_id":"s4","prompt":"hi"}`
	if _, _, err := runCLI(t, raw, "hook", "generic", "--tool", "override"); err != nil {
		t.Fatal(err)
	}
	out, _, _ := runCLI(t, "", "last", "1", "--json")
	var entries []map[string]any
	_ = json.Unmarshal([]byte(out), &entries)
	if entries[0]["tool"] != "override" {
		t.Fatalf("override flag ignored: %+v", entries[0])
	}
}

func TestGenericHook_ResponseAttachesToExistingSession(t *testing.T) {
	_ = isolatedHome(t)
	if _, _, err := runCLI(t, `{"tool":"mycli","session_id":"s5","prompt":"q"}`, "hook", "generic"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, `{"tool":"mycli","session_id":"s5","response":"a","model":"m1"}`, "hook", "generic"); err != nil {
		t.Fatal(err)
	}
	out, _, _ := runCLI(t, "", "last", "1", "--json")
	var entries []map[string]any
	_ = json.Unmarshal([]byte(out), &entries)
	if entries[0]["response"] != "a" || entries[0]["model"] != "m1" {
		t.Fatalf("response not attached: %+v", entries[0])
	}
}

func TestHooksShow_PrintsAbsolutePath(t *testing.T) {
	out, _, err := runCLI(t, "", "hooks", "show", "--tool", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(out), []byte("hook claude-code prompt")) {
		t.Fatalf("expected claude-code prompt command, got %q", out)
	}
	if !bytes.Contains([]byte(out), []byte("hook claude-code stop")) {
		t.Fatalf("expected claude-code stop command, got %q", out)
	}
}

func TestHooksInstall_WritesClaudeSettings(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("AILOG_CLAUDE_HOME", claudeHome)
	out, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(out), []byte("installed claude-code hooks")) {
		t.Fatalf("unexpected install message: %q", out)
	}
	settingsPath := filepath.Join(claudeHome, "settings.json")
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if _, ok := hooks["UserPromptSubmit"]; !ok {
		t.Fatalf("UserPromptSubmit missing: %+v", hooks)
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Fatalf("Stop missing: %+v", hooks)
	}
}

func TestHooksInstall_PreservesExistingUnrelatedKeys(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("AILOG_CLAUDE_HOME", claudeHome)
	// Seed an existing settings.json with some unrelated config.
	settingsPath := filepath.Join(claudeHome, "settings.json")
	existing := map[string]any{
		"env":              map[string]any{"FOO": "bar"},
		"someOtherUserKey": "keep-me",
		"hooks": map[string]any{
			"PreToolUse": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "other"}}}},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(settingsPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCLI(t, "", "hooks", "install", "--tool", "claude-code"); err != nil {
		t.Fatal(err)
	}
	merged, _ := os.ReadFile(settingsPath)
	var s map[string]any
	_ = json.Unmarshal(merged, &s)
	if s["someOtherUserKey"] != "keep-me" {
		t.Fatal("unrelated key was clobbered")
	}
	hooks := s["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatal("PreToolUse was clobbered")
	}
	if _, ok := hooks["UserPromptSubmit"]; !ok {
		t.Fatal("UserPromptSubmit missing")
	}
}

// TestPromptHook_EmptyPayload is a degenerate case we want to silently succeed.
func TestPromptHook_EmptyPayload_NoCrash(t *testing.T) {
	_ = isolatedHome(t)
	if _, _, err := runCLI(t, "", "hook", "claude-code", "prompt"); err != nil {
		t.Fatalf("empty stdin should not error: %v", err)
	}
}

var _ = store.InsertEntryInput{} // keep store import alive if we later add more tests
var _ = exec.Cmd{}               // in case we want exec-based integration tests
