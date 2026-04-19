package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// claudeCodePromptPayload is the subset of Claude Code's
// UserPromptSubmit hook JSON that we care about. Other fields
// (permission_mode, hook_event_name) are present in the payload but
// unused.
type claudeCodePromptPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Prompt         string `json:"prompt"`
}

// claudeCodeStopPayload is the subset of Claude Code's Stop hook JSON
// we use. `LastAssistantMessage` is shipped inline since Claude Code
// 1.0+, so the transcript-file fallback is rarely needed.
type claudeCodeStopPayload struct {
	SessionID            string `json:"session_id"`
	TranscriptPath       string `json:"transcript_path"`
	LastAssistantMessage string `json:"last_assistant_message"`
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
// Reads the JSON payload from stdin, extracts session/cwd/prompt,
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
			hookDebug("claude-code/prompt", raw, fmt.Sprintf("session=%s promptLen=%d", p.SessionID, len(p.Prompt)))
			if p.Prompt == "" {
				return nil
			}
			return capturePrompt(cmd.Context(), "claude-code", p.SessionID, p.CWD, p.Prompt, p.TranscriptPath)
		},
	}
}

// newClaudeCodeStopCmd handles Claude Code's Stop event. Prefers the
// inline `last_assistant_message` field for race-free response capture;
// falls back to polling the transcript JSONL (10 retries × 100ms) when
// the inline field is missing.
func newClaudeCodeStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Claude Code Stop — attaches the last assistant message",
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

			// Inline last_assistant_message is race-free — Claude Code
			// constructs it from the in-memory state, not from the
			// flushed-to-disk transcript. Prefer it when present.
			resp, model := p.LastAssistantMessage, ""
			if resp == "" {
				// Pre-1.0 Claude Code or future regression: poll the
				// transcript file. The Stop hook can fire before the last
				// assistant line is flushed to disk, so retry briefly.
				for i := 0; i < 10; i++ {
					resp, model = lastAssistantTurn(p.TranscriptPath)
					if resp != "" {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
			} else {
				// Even when the inline message is present, we still parse the
				// transcript to recover the model id (which the Stop payload
				// omits).
				_, model = lastAssistantTurn(p.TranscriptPath)
			}
			hookDebug("claude-code/stop", raw, fmt.Sprintf("session=%s respLen=%d model=%s", p.SessionID, len(resp), model))
			if resp == "" {
				return nil
			}
			return attachResponse(cmd.Context(), p.SessionID, resp, model)
		},
	}
}
