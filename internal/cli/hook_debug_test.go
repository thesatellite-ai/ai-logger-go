package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
