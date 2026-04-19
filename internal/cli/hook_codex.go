package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

// codexPayload is a tentative shape for OpenAI Codex CLI hook events.
// The CLI hasn't shipped a stable hook payload schema as of writing;
// when it does, adjust the field tags and the adapter logic below.
type codexPayload struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Prompt    string `json:"prompt"`
	Response  string `json:"response"`
	Model     string `json:"model"`
}

// newCodexHookCmd builds the `ailog hook codex {prompt,stop}` subtree.
// The adapter is wired but the JSON shape is a guess — verify against
// real Codex hook output before relying on it.
func newCodexHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codex",
		Short: "OpenAI Codex CLI hook adapters",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "prompt",
			Short: "Codex — stores the prompt (payload shape: {session_id, cwd, prompt})",
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, _ := io.ReadAll(os.Stdin)
				var p codexPayload
				if err := json.Unmarshal(raw, &p); err != nil && len(raw) > 0 {
					hookDebug("codex/prompt", raw, "decode-error: "+err.Error())
					return err
				}
				hookDebug("codex/prompt", raw, fmt.Sprintf("session=%s promptLen=%d", p.SessionID, len(p.Prompt)))
				if p.Prompt == "" {
					return nil
				}
				return capturePrompt(cmd.Context(), promptCapture{
					Tool: "codex", SessionID: p.SessionID, CWD: p.CWD, Prompt: p.Prompt,
				})
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Codex — attaches a response (payload shape: {session_id, response, model})",
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, _ := io.ReadAll(os.Stdin)
				var p codexPayload
				if err := json.Unmarshal(raw, &p); err != nil && len(raw) > 0 {
					return err
				}
				hookDebug("codex/stop", raw, fmt.Sprintf("session=%s respLen=%d", p.SessionID, len(p.Response)))
				if p.SessionID == "" || p.Response == "" {
					return nil
				}
				return attachResponse(cmd.Context(), p.SessionID, p.Response, p.Model, store.AttachResponseInput{})
			},
		},
	)
	return cmd
}
