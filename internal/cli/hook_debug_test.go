package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/khanakia/ai-logger/internal/agents"
)

// fakeSecret is a made-up token in a format redact.Scrub recognises.
const fakeSecret = "ghp_" + "0123456789abcdefghijABCDEFGHIJ0123456789"

func debugHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AILOG_HOME", home)
	return home
}

func TestHookDebug_OffByDefault(t *testing.T) {
	home := debugHome(t)
	for _, v := range []string{"", "0", "true", "yes"} {
		t.Setenv(hookDebugEnv, v)
		hookDebug("claude-code/prompt", []byte(`{"prompt":"hello"}`), "x")
	}
	if _, err := os.Stat(filepath.Join(home, hookLogName)); !os.IsNotExist(err) {
		t.Fatalf("hook.log written without AILOG_HOOK_DEBUG=1 (err %v)", err)
	}
}

func TestHookDebug_OnWritesRedactedPayload(t *testing.T) {
	home := debugHome(t)
	t.Setenv(hookDebugEnv, hookDebugOn)
	hookDebug("claude-code/prompt", []byte(`{"prompt":"deploy with `+fakeSecret+` now"}`), "decode-error: "+fakeSecret)
	b, err := os.ReadFile(filepath.Join(home, hookLogName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), fakeSecret) {
		t.Fatalf("secret written to hook.log: %s", b)
	}
	if !strings.Contains(string(b), "[redacted]") || !strings.Contains(string(b), "deploy with") {
		t.Fatalf("unexpected log line: %s", b)
	}
	if fi, _ := os.Stat(filepath.Join(home, hookLogName)); fi.Mode().Perm() != hookLogFilePerm {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}

func TestHookDebug_RotatesKeepingOne(t *testing.T) {
	home := debugHome(t)
	t.Setenv(hookDebugEnv, hookDebugOn)
	old := hookLogMax
	hookLogMax = 512
	t.Cleanup(func() { hookLogMax = old })

	payload := []byte(strings.Repeat("p", 100))
	for range 40 {
		hookDebug("e", payload, "")
	}
	log := filepath.Join(home, hookLogName)
	rotated := filepath.Join(home, hookLogRotated)
	for _, p := range []string{log, rotated} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if fi.Size() > hookLogMax {
			t.Fatalf("%s is %d bytes, over the %d cap", p, fi.Size(), hookLogMax)
		}
	}
	extra, _ := filepath.Glob(filepath.Join(home, hookLogName+".*"))
	if len(extra) != 1 {
		t.Fatalf("want exactly one rotated file, got %v", extra)
	}
}

// TestHookTools_MatchRegistry keeps the static hook subtree list equal to
// the agents whose agentio agent runs command hooks.
func TestHookTools_MatchRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg, err := agents.FromOS()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range reg.WithHooks() {
		got = append(got, a.Tool)
	}
	if strings.Join(got, ",") != strings.Join(hookTools(), ",") {
		t.Fatalf("WithHooks %v != hookTools %v", got, hookTools())
	}
}

// TestHookCommandLine_Compatible pins the command line every ailog
// release writes into settings files; changing it would orphan installed
// hooks.
func TestHookCommandLine_Compatible(t *testing.T) {
	if got := hookCommandLine("/Users/alice/go/bin/ailog", "claude-code", "prompt"); got != "/Users/alice/go/bin/ailog hook claude-code prompt" {
		t.Fatalf("got %q", got)
	}
	if got := hookCommandLine("/Applications/My Tools/ailog", "claude-code", "stop"); got != "'/Applications/My Tools/ailog' hook claude-code stop" {
		t.Fatalf("got %q", got)
	}
	if got := hookCommandLine("/tmp/it's/ailog", "codex", "stop"); got != `'/tmp/it'\''s/ailog' hook codex stop` {
		t.Fatalf("got %q", got)
	}
}

func TestIsAilogCommandFor(t *testing.T) {
	cases := map[string]bool{
		"/Users/alice/go/bin/ailog hook claude-code prompt":      true,
		"ailog hook claude-code prompt":                          true,
		"'/Applications/My Tools/ailog' hook claude-code prompt": true,
		`"/opt/tools/ailog.exe" hook claude-code prompt`:         true,
		"  /usr/local/bin/ailog hook claude-code prompt  ":       true,
		"/usr/local/bin/ailog hook claude-code stop":             false, // other sub
		"/usr/local/bin/ailog hook codex prompt":                 false, // other tool
		"/usr/local/bin/notailog hook claude-code prompt":        false,
		"echo hi; /usr/local/bin/ailog hook claude-code prompt":  false, // compound command
		"/usr/local/bin/ailog hook claude-code prompt --verbose": false,
		"$(which ailog) hook claude-code prompt":                 false,
	}
	for cmd, want := range cases {
		if got := isAilogCommandFor(cmd, "claude-code", "prompt"); got != want {
			t.Errorf("isAilogCommandFor(%q) = %v, want %v", cmd, got, want)
		}
	}
}
