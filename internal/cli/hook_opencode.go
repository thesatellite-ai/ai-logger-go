package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/khanakia/ai-logger/internal/store"
	"github.com/spf13/cobra"
)

// openCodePayload is a tentative shape for opencode CLI hook events,
// mirroring the codex skeleton. Adjust once opencode publishes a real
// hook payload schema.
type openCodePayload struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Prompt    string `json:"prompt"`
	Response  string `json:"response"`
	Model     string `json:"model"`
}

// newOpenCodeHookCmd builds the `ailog hook opencode {prompt,stop}` subtree.
func newOpenCodeHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "opencode",
		Short: "opencode CLI hook adapters",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "prompt",
			Short: "opencode — stores the prompt",
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, _ := io.ReadAll(os.Stdin)
				var p openCodePayload
				if err := json.Unmarshal(raw, &p); err != nil && len(raw) > 0 {
					return err
				}
				hookDebug("opencode/prompt", raw, fmt.Sprintf("session=%s promptLen=%d", p.SessionID, len(p.Prompt)))
				if p.Prompt == "" {
					return nil
				}
				return capturePrompt(cmd.Context(), promptCapture{
					Tool: "opencode", SessionID: p.SessionID, CWD: p.CWD, Prompt: p.Prompt,
				})
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "opencode — attaches a response",
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, _ := io.ReadAll(os.Stdin)
				var p openCodePayload
				if err := json.Unmarshal(raw, &p); err != nil && len(raw) > 0 {
					return err
				}
				hookDebug("opencode/stop", raw, fmt.Sprintf("session=%s respLen=%d", p.SessionID, len(p.Response)))
				if p.SessionID == "" || p.Response == "" {
					return nil
				}
				return attachResponse(cmd.Context(), p.SessionID, p.Response, p.Model, store.AttachResponseInput{})
			},
		},
	)
	return cmd
}
