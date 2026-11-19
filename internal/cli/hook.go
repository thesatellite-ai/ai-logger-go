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

// promptCapture carries the prompt-side input from a hook adapter to
// capturePrompt. Adapters with no extras (codex / opencode / generic)
// leave the optional fields empty.
type promptCapture struct {
	Tool           string // required — "claude-code" / "codex" / …
	SessionID      string
	CWD            string
	Prompt         string
	Trace          string // free-form provenance; claude-code stores transcript_path
	PermissionMode string // claude-code only
}

// capturePrompt is the tool-agnostic primitive behind every prompt-side
// hook adapter. It chdirs into the payload-supplied cwd (so git
// auto-detection works), collects the rest of the environment, scrubs
// secrets out of the prompt, and writes one new entry to the store.
func capturePrompt(ctx stdcontext.Context, p promptCapture) error {
	if p.CWD != "" {
		_ = os.Chdir(p.CWD)
	}
	env := aictx.Collect(ctx)
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	_, err = s.InsertEntry(ctx, store.InsertEntryInput{
		Tool:           p.Tool,
		CWD:            firstNonEmptyS(p.CWD, env.CWD),
		Project:        env.Project,
		RepoOwner:      env.Git.Owner,
		RepoName:       env.Git.Name,
		RepoRemote:     env.Git.Remote,
		GitBranch:      env.Git.Branch,
		GitCommit:      env.Git.Commit,
		SessionID:      p.SessionID,
		Hostname:       env.Env.Hostname,
		User:           env.Env.User,
		Shell:          env.Env.Shell,
		Terminal:       env.Env.Terminal,
		TerminalTitle:  env.Env.TerminalTitle,
		TTY:            env.Env.TTY,
		PID:            env.Env.PID,
		Prompt:         redact.Scrub(p.Prompt),
		Raw:            p.Trace,
		PermissionMode: p.PermissionMode,
	})
	return err
}

// attachResponse is the tool-agnostic primitive behind every stop-side
// hook adapter. It walks the session's entries newest-first and
// attaches the response (after scrubbing) to the first one that doesn't
// already have one. No-op if the session is unknown or all entries are
// already closed.
//
// `extras` carries any additional per-turn metadata the adapter could
// extract (token usage, stop_reason, permission_mode, tool_version,
// …). Adapters with no extras pass a zero-value struct.
func attachResponse(ctx stdcontext.Context, sessionID, response, model string, extras store.AttachResponseInput) error {
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	entries, err := s.SessionEntries(ctx, sessionID)
	if err != nil {
		return err
	}
	resp := redact.Scrub(response)
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Response != "" {
			continue
		}
		extras.EntryID = entries[i].ID
		extras.Response = resp
		if extras.Model == "" {
			extras.Model = model
		}
		return s.AttachResponse(ctx, extras)
	}
	return nil
}

// transcriptMeta is the structured metadata extracted from the most
// recent assistant message line of a Claude Code transcript.
//
// All fields are best-effort — zero values mean the field wasn't
// present in the source (which is fine for non-Anthropic tools that
// don't emit `usage`). See assistant samples in ~/.claude/projects/*.jsonl
// for the full shape.
type transcriptMeta struct {
	Text              string // flattened text from message.content[]
	Model             string // message.model (e.g. "claude-opus-4-7")
	StopReason        string // message.stop_reason (end_turn | tool_use | max_tokens | stop_sequence)
	InputTokens       int    // message.usage.input_tokens
	OutputTokens      int    // message.usage.output_tokens
	CacheReadTokens   int    // message.usage.cache_read_input_tokens (Anthropic prompt cache HIT)
	CacheCreateTokens int    // message.usage.cache_creation_input_tokens (cache WRITE)
	ToolVersion       string // top-level "version" — Claude Code version string
}

// readTranscriptMeta scans the JSONL for the latest assistant line and
// returns a populated transcriptMeta. retries / interval handle the
// flush race (Stop hook can fire before the last line lands on disk).
//
// Pass retries=1 interval=0 for a one-shot read.
func readTranscriptMeta(path string, retries int, interval time.Duration) transcriptMeta {
	if path == "" {
		return transcriptMeta{}
	}
	for attempt := 0; attempt < retries; attempt++ {
		m := scanTranscriptOnce(path)
		if m.Text != "" {
			return m
		}
		if interval > 0 && attempt+1 < retries {
			time.Sleep(interval)
		}
	}
	return transcriptMeta{}
}

// scanTranscriptOnce performs a single full-file scan and returns the
// latest assistant message's metadata.
func scanTranscriptOnce(path string) transcriptMeta {
	f, err := os.Open(path)
	if err != nil {
		return transcriptMeta{}
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024) // 32MB max line — assistant turns can be long

	type usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	}
	type message struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		Model      string          `json:"model"`
		StopReason string          `json:"stop_reason"`
		Usage      usage           `json:"usage"`
	}
	type msgLine struct {
		Type    string  `json:"type"`
		Version string  `json:"version"`
		Message message `json:"message"`
	}

	var last transcriptMeta
	for scanner.Scan() {
		var m msgLine
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			continue
		}
		if m.Type != "assistant" {
			continue
		}
		// Content can be a string OR an array of typed blocks
		// (text / thinking / tool_use). flattenContent extracts text.
		t := flattenContent(m.Message.Content)
		if t == "" {
			// Don't overwrite the last good text if this assistant line
			// only had a tool_use block — keep scanning forward.
			continue
		}
		last = transcriptMeta{
			Text:              t,
			Model:             m.Message.Model,
			StopReason:        m.Message.StopReason,
			InputTokens:       m.Message.Usage.InputTokens,
			OutputTokens:      m.Message.Usage.OutputTokens,
			CacheReadTokens:   m.Message.Usage.CacheReadInputTokens,
			CacheCreateTokens: m.Message.Usage.CacheCreationInputTokens,
			ToolVersion:       m.Version,
		}
	}
	return last
}

// Hook debug log settings. The log is OFF unless AILOG_HOOK_DEBUG=1:
// hook payloads carry full prompt text, so ailog must not keep a copy
// of them by default (an always-on log once grew to 179 MB of raw
// prompts). When enabled, payloads are redacted before writing and the
// file rotates so it cannot grow without bound.
const (
	hookDebugEnv     = "AILOG_HOOK_DEBUG"
	hookDebugOn      = "1"
	hookLogName      = "hook.log"
	hookLogRotated   = hookLogName + ".1" // the single kept rotation
	hookLogMaxBytes  = 10 << 20           // rotate once hook.log reaches 10 MiB
	hookLogFilePerm  = 0o600
	hookLogDirPerm   = 0o700
	hookLogTimestamp = time.RFC3339Nano
)

// hookLogMax is the rotation threshold; a variable so tests can use a
// small value instead of writing 10 MiB.
var hookLogMax int64 = hookLogMaxBytes

// hookDebugEnabled reports whether AILOG_HOOK_DEBUG=1 is set.
func hookDebugEnabled() bool { return os.Getenv(hookDebugEnv) == hookDebugOn }

// hookDebug appends one line per hook invocation to ~/.ailog/hook.log
// when AILOG_HOOK_DEBUG=1, to debug "no entries appearing" without
// instrumenting the harness. The payload is passed through
// redact.Scrub first (the same scrubber the stored prompts use), and
// the file rotates to hook.log.1 at hookLogMax, keeping one old file.
func hookDebug(event string, payload []byte, extra string) {
	if !hookDebugEnabled() {
		return
	}
	home, err := config.Home()
	if err != nil {
		return
	}
	_ = os.MkdirAll(home, hookLogDirPerm)
	path := filepath.Join(home, hookLogName)
	line := fmt.Sprintf("%s  %s  %s  %s\n", time.Now().Format(hookLogTimestamp), event,
		redact.Scrub(extra), redact.Scrub(string(payload)))
	rotateHookLog(path, int64(len(line)))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, hookLogFilePerm)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(line)
}

// rotateHookLog renames path to path.1 (replacing any older rotation)
// when appending incoming bytes would take it past hookLogMax.
func rotateHookLog(path string, incoming int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 || info.Size()+incoming <= hookLogMax {
		return
	}
	_ = os.Rename(path, filepath.Join(filepath.Dir(path), hookLogRotated))
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

// flattenContent extracts text from Claude's `message.content` field,
// which can be either a plain string or an array of typed blocks. Used
// by the live Stop hook's transcript scanner; the importer has its own
// copy because the two paths must stay independent.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var out string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				if out != "" {
					out += "\n"
				}
				out += b.Text
			}
		}
		return out
	}
	return ""
}
