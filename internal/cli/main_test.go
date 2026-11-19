package cli_test

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points HOME at a throwaway folder and clears every agent home
// variable before any test runs, so no test of this package can read or
// write the real ~/.claude, ~/.codex, ~/.gemini or ~/.ailog even when it
// forgets its own isolation. Tests that need agent homes write them under
// their own temp HOME (testfixtures.Isolate).
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "ailog-cli-test-home-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for k, v := range map[string]string{"HOME": home, "USERPROFILE": home, "AILOG_HOME": home + "/.ailog"} {
		_ = os.Setenv(k, v)
	}
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GEMINI_CLI_HOME", "PI_CODING_AGENT_DIR",
		"XDG_DATA_HOME", "XDG_CONFIG_HOME", "OPENCODE_DB", "AILOG_CLAUDE_HOME", "AILOG_HOOK_DEBUG"} {
		_ = os.Unsetenv(k)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
