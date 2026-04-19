// Package cli internal/cli/hook.go — umbrella for the per-tool hook
// adapter subcommand tree, plus the shared primitives every adapter
// calls into. Per-tool adapters live in sibling files:
//
//	hook_claude.go    — Anthropic Claude Code adapter (real, in production)
//	hook_codex.go     — OpenAI Codex CLI adapter (skeleton)
//	hook_opencode.go  — opencode CLI adapter (skeleton)
//	hook_generic.go   — neutral JSON adapter for anything else
//
// The umbrella `ailog hook ...` command is hidden from `--help` because
// it's invoked by harness hook configs, not by humans.
package cli

import (
	"bufio"
	stdcontext "context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/khanakia/ai-logger/internal/config"
	aictx "github.com/khanakia/ai-logger/internal/context"
	"github.com/khanakia/ai-logger/internal/redact"
	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

// newHookCmd is the tool-agnostic namespace for JSON-payload hook
// adapters. Each subtree (claude-code / codex / opencode / generic)
// converts its native payload into the neutral
// store.InsertEntryInput + AttachResponse calls. Adding a new tool =
// adding a new sibling file with a `newXxxHookCmd()` and registering
// it here.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Internal: per-tool hook adapters (claude-code, codex, opencode, generic)",
		Hidden: true,
	}
	cmd.AddCommand(
		newClaudeCodeHookCmd(),
		newCodexHookCmd(),
		newOpenCodeHookCmd(),
		newGenericHookCmd(),
	)
	return cmd
}

// capturePrompt is the tool-agnostic primitive behind every prompt-side
// hook adapter. It chdirs into the payload-supplied cwd (so git
// auto-detection works), collects the rest of the environment, scrubs
// secrets out of the prompt, and writes one new entry to the store.
func capturePrompt(ctx stdcontext.Context, tool, sessionID, cwd, prompt, trace string) error {
	if cwd != "" {
		_ = os.Chdir(cwd)
	}
	env := aictx.Collect(ctx)
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()
	_, err = s.InsertEntry(ctx, store.InsertEntryInput{
		Tool:          tool,
		CWD:           firstNonEmptyS(cwd, env.CWD),
		Project:       env.Project,
		RepoOwner:     env.Git.Owner,
		RepoName:      env.Git.Name,
		RepoRemote:    env.Git.Remote,
		GitBranch:     env.Git.Branch,
		GitCommit:     env.Git.Commit,
		SessionID:     sessionID,
		Hostname:      env.Env.Hostname,
		User:          env.Env.User,
		Shell:         env.Env.Shell,
		Terminal:      env.Env.Terminal,
		TerminalTitle: env.Env.TerminalTitle,
		TTY:           env.Env.TTY,
		PID:           env.Env.PID,
		Prompt:        redact.Scrub(prompt),
		Raw:           trace,
	})
	return err
}

// attachResponse is the tool-agnostic primitive behind every stop-side
// hook adapter. It walks the session's entries newest-first and
// attaches the response (after scrubbing) to the first one that doesn't
// already have one. No-op if the session is unknown or all entries are
// already closed.
func attachResponse(ctx stdcontext.Context, sessionID, response, model string) error {
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()
	entries, err := s.SessionEntries(ctx, sessionID)
	if err != nil {
		return err
	}
	resp := redact.Scrub(response)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Response == "" {
			return s.AttachResponse(ctx, entries[i].ID, resp, model, 0)
		}
	}
	return nil
}

// lastAssistantTurn reads a Claude Code transcript JSONL file and
// returns the text + model of the most recent `type:"assistant"` line.
// Used as a race-free fallback when the Stop hook payload doesn't carry
// `last_assistant_message`. Empty strings on any error.
func lastAssistantTurn(path string) (text, model string) {
	if path == "" {
		return "", ""
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024) // 32MB max line — assistant turns can be long
	type msgLine struct {
		Type    string `json:"type"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Model   string          `json:"model"`
		} `json:"message"`
	}
	var lastText, lastModel string
	for scanner.Scan() {
		var m msgLine
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			continue
		}
		if m.Type != "assistant" {
			continue
		}
		// Content can be either a plain string or an array of typed blocks
		// (text / tool_use / tool_result). flattenContent handles both.
		t := flattenContent(m.Message.Content)
		if t != "" {
			lastText = t
			lastModel = m.Message.Model
		}
	}
	return lastText, lastModel
}

// hookDebug appends one line per hook invocation to ~/.ailog/hook.log.
// The log captures the raw stdin payload + an extra status string so we
// can debug "no entries appearing" without instrumenting the harness.
// Disable with `AILOG_HOOK_DEBUG=0`.
func hookDebug(event string, payload []byte, extra string) {
	if os.Getenv("AILOG_HOOK_DEBUG") == "0" {
		return
	}
	home, err := config.Home()
	if err != nil {
		return
	}
	_ = os.MkdirAll(home, 0o700)
	f, err := os.OpenFile(filepath.Join(home, "hook.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %s  %s  %s\n", time.Now().Format(time.RFC3339Nano), event, extra, string(payload))
}

// firstNonEmptyS returns the first non-empty string in vs, or "" if none.
// Used to layer fallback values (flag → env → autodetect).
func firstNonEmptyS(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
