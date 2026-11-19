package cli

import (
	"bytes"
	stdcontext "context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/ubgo/agentio"

	"github.com/khanakia/ai-logger/internal/agents"
	"github.com/khanakia/ai-logger/internal/store"
)

// Hook subcommands. They are part of the command line ailog writes into
// an agent's settings ("<bin> hook claude-code prompt"), so they never
// change.
const (
	hookSubPrompt = "prompt"
	hookSubStop   = "stop"
)

// hookKindSubs maps the neutral hook kinds ailog installs to their
// subcommand.
func hookKindSubs() []struct {
	Kind agentio.HookKind
	Sub  string
} {
	return []struct {
		Kind agentio.HookKind
		Sub  string
	}{
		{agentio.HookPromptSubmit, hookSubPrompt},
		{agentio.HookTurnEnd, hookSubStop},
	}
}

// hookTools lists the agents with a `hook` subtree: those whose agentio
// agent can run command hooks (agents.Registry.WithHooks; a test keeps
// the two lists equal). Static so building the command tree reads no
// disk.
func hookTools() []string {
	return []string{agents.ToolClaude, agents.ToolCodex, agents.ToolGemini}
}

// The Stop hook can fire before the agent flushed the answer to its
// transcript; the transcript is read again until the answer shows.
const (
	stopRetries  = 10
	stopInterval = 100 * time.Millisecond
)

// hookCursorPrefix keys the Stop hook's per-transcript read cursor in
// import_state, apart from the importer's own row for the same file (the
// two read different things and must not share a watermark).
const hookCursorPrefix = "hook:"

// newAgentHookCmd builds `ailog hook <tool> prompt|stop`.
func newAgentHookCmd(tool string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   tool,
		Short: tool + " hook payload adapters (decoded through agentio)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   hookSubPrompt,
			Short: tool + " prompt-submit hook — stores the prompt",
			RunE: func(cmd *cobra.Command, _ []string) error {
				call, raw, ok, err := decodeHookCall(tool, hookSubPrompt)
				if err != nil || !ok {
					return err
				}
				hookDebug(tool+"/"+hookSubPrompt, raw, fmt.Sprintf("session=%s promptLen=%d permMode=%s",
					call.ConversationID, len(call.Prompt), call.PermissionMode))
				if call.Prompt == "" {
					return nil
				}
				return capturePrompt(cmd.Context(), promptCapture{
					Tool:           tool,
					SessionID:      call.ConversationID,
					CWD:            call.Dir,
					Prompt:         call.Prompt,
					Trace:          call.TranscriptPath,
					PermissionMode: call.PermissionMode,
				})
			},
		},
		&cobra.Command{
			Use:   hookSubStop,
			Short: tool + " turn-end hook — attaches the answer and its usage",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runStopHook(cmd.Context(), tool)
			},
		},
	)
	return cmd
}

// decodeHookCall reads a hook payload from stdin and decodes it with the
// agent's agentio decoder. ok=false (no error) for an empty payload.
func decodeHookCall(tool, sub string) (call agentio.HookCall, raw []byte, ok bool, err error) {
	raw, _ = io.ReadAll(os.Stdin)
	if len(bytes.TrimSpace(raw)) == 0 {
		hookDebug(tool+"/"+sub, raw, "empty-payload")
		return agentio.HookCall{}, raw, false, nil
	}
	ag, err := agentFor(tool)
	if err != nil {
		return agentio.HookCall{}, raw, false, err
	}
	h, has := ag.Hooks()
	if !has {
		return agentio.HookCall{}, raw, false, fmt.Errorf("%s has no command hooks", tool)
	}
	call, err = h.DecodeHook(bytes.NewReader(raw))
	if err != nil {
		hookDebug(tool+"/"+sub, raw, "decode-error: "+err.Error())
		return agentio.HookCall{}, raw, false, fmt.Errorf("decode payload: %w", err)
	}
	return call, raw, true, nil
}

// agentFor finds a registered agent by stored tool name.
func agentFor(tool string) (agents.Agent, error) {
	reg, err := agents.FromOS()
	if err != nil {
		return agents.Agent{}, err
	}
	ag, ok := reg.ByTool(tool)
	if !ok {
		return agents.Agent{}, fmt.Errorf("unknown agent %q", tool)
	}
	return ag, nil
}

// runStopHook attaches the turn's answer to the session's open prompt:
// the answer the payload carries inline, else the last answer of the
// transcript, with model, stop reason, usage and agent version from the
// transcript when it has them.
func runStopHook(ctx stdcontext.Context, tool string) error {
	call, raw, ok, err := decodeHookCall(tool, hookSubStop)
	if err != nil || !ok {
		return err
	}
	if call.ConversationID == "" {
		hookDebug(tool+"/"+hookSubStop, raw, "empty-session")
		return nil
	}
	ag, err := agentFor(tool)
	if err != nil {
		return err
	}
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	ans := lastAnswer(ctx, s, ag, call.TranscriptPath, stopRetries, stopInterval)
	resp := firstNonEmptyS(call.LastAssistantMessage, ans.Text)
	model := firstNonEmptyS(ans.Model, call.Model)
	in := store.AttachResponseInput{StopReason: ans.StopReason, PermissionMode: call.PermissionMode, ToolVersion: ans.AgentVersion}
	if tk := ans.Tokens; tk != nil {
		in.TokensIn = int(tk.Input)
		in.TokensOut = int(tk.Output + tk.Reasoning) // providers bill reasoning as output
		in.TokensCacheRead = int(tk.CachedInput)
		in.TokensCacheWrite = int(tk.CacheWrite)
	}
	hookDebug(tool+"/"+hookSubStop, raw, fmt.Sprintf(
		"session=%s respLen=%d model=%s stopReason=%s tokIn=%d tokOut=%d cacheRead=%d cacheCreate=%d ver=%s permMode=%s",
		call.ConversationID, len(resp), model, in.StopReason, in.TokensIn, in.TokensOut,
		in.TokensCacheRead, in.TokensCacheWrite, in.ToolVersion, in.PermissionMode))
	if resp == "" {
		return nil
	}
	return attachResponseIn(ctx, s, call.ConversationID, resp, model, in)
}

// lastAnswer reads the transcript at path from the cursor the previous
// Stop hook stored for it and returns the last answer (an assistant turn
// with text, interim progress messages skipped). It retries while no
// answer is there yet (the agent may still be flushing) and stores the
// new cursor once one is found, so each Stop reads only what the turn
// appended instead of the whole transcript. The zero Turn when path is
// empty, unreadable or holds no new answer.
func lastAnswer(ctx stdcontext.Context, s *store.Store, ag agents.Agent, path string, retries int, interval time.Duration) agentio.Turn {
	if path == "" {
		return agentio.Turn{}
	}
	key := hookCursorPrefix + path
	var from agentio.Offset
	if wm, ok, err := s.ImportFileWatermark(ctx, key); err == nil && ok {
		// A transcript that shrank was rewritten; its cursor is void.
		if info, err := os.Stat(path); err == nil && info.Size() >= wm.Size {
			from = agentio.Offset(wm.Offset)
		}
	}
	for attempt := 0; attempt < retries; attempt++ {
		var last agentio.Turn
		next, err := ag.TurnsAt(path, from, func(t agentio.Turn) error {
			if t.Role == agentio.RoleAssistant && !t.Interim && t.Text != "" {
				last = t
			}
			return nil
		})
		if err == nil && last.Text != "" {
			if info, err := os.Stat(path); err == nil {
				_ = s.ImportFileMark(ctx, key, info.ModTime(), info.Size(), int64(next))
			}
			return last
		}
		if interval > 0 && attempt+1 < retries {
			time.Sleep(interval)
		}
	}
	return agentio.Turn{}
}
