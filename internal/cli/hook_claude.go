package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

// claudeCodePromptPayload is the subset of Claude Code's
// UserPromptSubmit hook JSON we actually use.
//
// The full payload also carries `permission_mode` ("default" |
// "acceptEdits" | "bypassPermissions" | "plan") and `hook_event_name`
// (always "UserPromptSubmit"). We pull permission_mode through so it
// lands on the entry; hook_event_name is diagnostic-only.
type claudeCodePromptPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Prompt         string `json:"prompt"`
	PermissionMode string `json:"permission_mode"`
}

// claudeCodeStopPayload is the subset of Claude Code's Stop hook JSON
// we use. `LastAssistantMessage` ships inline (race-free); we fall back
// to parsing the transcript file when it isn't present.
//
// Fields we ignore on purpose: `cwd` (already captured at prompt time),
// `stop_hook_active` (only relevant for hook recursion), `hook_event_name`.
type claudeCodeStopPayload struct {
	SessionID            string `json:"session_id"`
	TranscriptPath       string `json:"transcript_path"`
	LastAssistantMessage string `json:"last_assistant_message"`
	PermissionMode       string `json:"permission_mode"`
}

// newClaudeCodeHookCmd builds the `ailog hook claude-code` subtree
// with `prompt` (UserPromptSubmit handler) and `stop` (Stop handler)
// children.
func newClaudeCodeHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude-code",
		Short: "Claude Code hook payload adapters",
	}
	cmd.AddCommand(newClaudeCodePromptCmd(), newClaudeCodeStopCmd())
	return cmd
}

// newClaudeCodePromptCmd handles Claude Code's UserPromptSubmit event.
// Reads the JSON payload from stdin, extracts session/cwd/prompt/permission_mode,
// delegates to capturePrompt.
func newClaudeCodePromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Claude Code UserPromptSubmit — stores the prompt",
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := io.ReadAll(os.Stdin)
			var p claudeCodePromptPayload
			if err := json.Unmarshal(raw, &p); err != nil && len(raw) > 0 {
				hookDebug("claude-code/prompt", raw, "decode-error: "+err.Error())
				return fmt.Errorf("decode payload: %w", err)
			}
			hookDebug("claude-code/prompt", raw, fmt.Sprintf("session=%s promptLen=%d permMode=%s",
				p.SessionID, len(p.Prompt), p.PermissionMode))
			if p.Prompt == "" {
				return nil
			}
			return capturePrompt(cmd.Context(), promptCapture{
				Tool:           "claude-code",
				SessionID:      p.SessionID,
				CWD:            p.CWD,
				Prompt:         p.Prompt,
				Trace:          p.TranscriptPath,
				PermissionMode: p.PermissionMode,
			})
		},
	}
}

// newClaudeCodeStopCmd handles Claude Code's Stop event. Prefers the
// inline `last_assistant_message` field for race-free response capture
// and falls back to polling the transcript JSONL when the inline field
// is missing.
//
// Either way we ALWAYS parse the transcript for the rich metadata that
// only lives there: model, stop_reason, and the full token usage
// breakdown (input / output / cache_read / cache_create) — Anthropic
// cache hit is one of the most useful per-turn signals we can capture.
func newClaudeCodeStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Claude Code Stop — attaches the last assistant message + usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := io.ReadAll(os.Stdin)
			var p claudeCodeStopPayload
			if err := json.Unmarshal(raw, &p); err != nil && len(raw) > 0 {
				hookDebug("claude-code/stop", raw, "decode-error: "+err.Error())
				return fmt.Errorf("decode payload: %w", err)
			}
			if p.SessionID == "" {
				hookDebug("claude-code/stop", raw, "empty-session")
				return nil
			}

			// Always do a transcript scan for the structured metadata —
			// model / stop_reason / usage live there, never in the Stop
			// payload. Tolerate the brief flush race with a few retries.
			meta := readTranscriptMeta(p.TranscriptPath, 10, 100*time.Millisecond)

			// Pick text: inline payload wins (race-free), transcript fallback otherwise.
			resp := p.LastAssistantMessage
			if resp == "" {
				resp = meta.Text
			}

			hookDebug("claude-code/stop", raw, fmt.Sprintf(
				"session=%s respLen=%d model=%s stopReason=%s tokIn=%d tokOut=%d cacheRead=%d cacheCreate=%d ver=%s permMode=%s",
				p.SessionID, len(resp), meta.Model, meta.StopReason,
				meta.InputTokens, meta.OutputTokens, meta.CacheReadTokens, meta.CacheCreateTokens,
				meta.ToolVersion, p.PermissionMode))

			if resp == "" {
				return nil
			}
			return attachResponse(cmd.Context(), p.SessionID, resp, meta.Model, store.AttachResponseInput{
				TokensIn:         meta.InputTokens,
				TokensOut:        meta.OutputTokens,
				TokensCacheRead:  meta.CacheReadTokens,
				TokensCacheWrite: meta.CacheCreateTokens,
				StopReason:       meta.StopReason,
				PermissionMode:   p.PermissionMode,
				ToolVersion:      meta.ToolVersion,
			})
		},
	}
}
