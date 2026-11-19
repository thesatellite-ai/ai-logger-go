// Package cli internal/cli/hook.go — umbrella for the hook adapter
// subcommand tree, plus the shared primitives every adapter calls into:
//
//	hook_agent.go    — `ailog hook <agent> prompt|stop` for every agent with
//	                   command hooks (claude-code, codex, gemini); payloads
//	                   are decoded and transcripts read through agentio
//	hook_generic.go  — neutral JSON adapter for anything else
//
// The umbrella `ailog hook ...` command is hidden from `--help` because
// it's invoked by harness hook configs, not by humans. The command lines
// (`<bin> hook claude-code prompt`, …) are written into users' settings
// files, so a subcommand name never changes.
package cli

import (
	stdcontext "context"
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
// adapters: one subtree per agent with command hooks, plus generic.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Internal: per-agent hook adapters (claude-code, codex, gemini, generic)",
		Hidden: true,
	}
	for _, tool := range hookTools() {
		cmd.AddCommand(newAgentHookCmd(tool))
	}
	cmd.AddCommand(newGenericHookCmd())
	return cmd
}

// promptCapture carries the prompt-side input from a hook adapter to
// capturePrompt. Adapters with no extras (generic) leave the optional
// fields empty.
type promptCapture struct {
	Tool           string // required — "claude-code" / "codex" / …
	SessionID      string
	CWD            string
	Prompt         string
	Trace          string // free-form provenance: the transcript path a hook payload names
	PermissionMode string // the agent's permission mode, when its payload sends one
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
	return attachResponseIn(ctx, s, sessionID, response, model, extras)
}

// attachResponseIn is attachResponse on an open store.
func attachResponseIn(ctx stdcontext.Context, s *store.Store, sessionID, response, model string, extras store.AttachResponseInput) error {
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
